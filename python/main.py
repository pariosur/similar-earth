"""
TierraAI GEE Microservice
Handles Google Earth Engine operations for satellite imagery
Includes Geospatial-RAG with AlphaEarth embeddings
"""
import os
import tempfile
import asyncio
from contextlib import asynccontextmanager
from dataclasses import dataclass
from datetime import datetime, timedelta
from pathlib import Path
from typing import Optional
import logging

import ee
import numpy as np
from fastapi import FastAPI, HTTPException, BackgroundTasks
from pydantic import BaseModel
from PIL import Image
import psycopg2
from psycopg2.extras import RealDictCursor
from pgvector.psycopg2 import register_vector

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# Initialize Earth Engine
PROJECT = os.getenv("GEE_PROJECT", "tierra-ai")

try:
    ee.Initialize(project=PROJECT)
except Exception:
    # For local development with personal auth
    ee.Authenticate()
    ee.Initialize(project=PROJECT)

# Database URL
DATABASE_URL = os.getenv("DATABASE_URL", "postgres://tierraai:tierraai@localhost:5432/tierraai")

# Global database connection
db_conn = None

def get_db():
    """Get database connection with pgvector support."""
    global db_conn
    if db_conn is None or db_conn.closed:
        db_conn = psycopg2.connect(DATABASE_URL)
        register_vector(db_conn)
    else:
        # Rollback any aborted transaction to reset the connection state
        try:
            db_conn.rollback()
        except Exception:
            # If rollback fails, try to reconnect
            try:
                db_conn.close()
            except Exception:
                pass
            db_conn = psycopg2.connect(DATABASE_URL)
            register_vector(db_conn)
    return db_conn


def init_reference_farms_table():
    """Initialize the reference_farms table with pgvector extension."""
    conn = get_db()
    cur = conn.cursor()
    
    try:
        # Enable pgvector extension
        cur.execute("CREATE EXTENSION IF NOT EXISTS vector;")
        
        # Create reference_farms table
        cur.execute("""
            CREATE TABLE IF NOT EXISTS reference_farms (
                id SERIAL PRIMARY KEY,
                name TEXT NOT NULL,
                crop TEXT NOT NULL,
                latitude DOUBLE PRECISION NOT NULL,
                longitude DOUBLE PRECISION NOT NULL,
                embedding vector(64),
                
                -- Documented performance (from published sources)
                yield_min DOUBLE PRECISION,
                yield_max DOUBLE PRECISION,
                yield_typical DOUBLE PRECISION,
                
                -- Context
                region TEXT,
                country TEXT,
                elevation_m DOUBLE PRECISION,
                annual_rainfall_mm DOUBLE PRECISION,
                avg_temp_c DOUBLE PRECISION,
                
                -- Source documentation
                data_source TEXT,
                notes TEXT,
                
                -- Metadata
                seasons_per_year INTEGER DEFAULT 1,
                embedding_year INTEGER DEFAULT 2024,
                created_at TIMESTAMPTZ DEFAULT NOW(),
                
                UNIQUE(crop, latitude, longitude)
            );
            
            -- Create index for fast similarity search
            CREATE INDEX IF NOT EXISTS idx_reference_farms_embedding 
            ON reference_farms USING ivfflat (embedding vector_cosine_ops)
            WITH (lists = 20);
            
            CREATE INDEX IF NOT EXISTS idx_reference_farms_crop ON reference_farms(crop);
            
            -- Add seasons_per_year column if it doesn't exist (for existing tables)
            ALTER TABLE reference_farms ADD COLUMN IF NOT EXISTS seasons_per_year INTEGER DEFAULT 1;
        """)
        
        conn.commit()
        logger.info("Reference farms table initialized successfully")
    except Exception as e:
        conn.rollback()
        logger.error(f"Failed to initialize reference farms table: {e}")
        raise
    finally:
        cur.close()


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Application lifespan handler - runs on startup and shutdown."""
    # Startup
    logger.info("Initializing Geospatial-RAG database...")
    try:
        init_reference_farms_table()
        await seed_reference_farms()
        logger.info("Geospatial-RAG ready!")
    except Exception as e:
        logger.warning(f"Could not initialize RAG database (may not have Postgres): {e}")
    
    yield
    
    # Shutdown
    global db_conn
    if db_conn and not db_conn.closed:
        db_conn.close()


app = FastAPI(
    title="Similar Earth GEE Service",
    description="Google Earth Engine microservice with Geospatial-RAG",
    version="0.2.0",
    lifespan=lifespan,
)


# =============================================================================
# Data Classes for Internal Use
# =============================================================================

@dataclass
class WeatherData:
    """Weather data from Open-Meteo API."""
    temp_mean: float
    temp_min: float
    temp_max: float
    precipitation_total_mm: float
    precipitation_days: int
    humidity_mean: float
    sunshine_hours_daily_avg: float
    cloud_cover_mean_percent: float


@dataclass
class SoilData:
    """Soil data from SoilGrids API."""
    clay_percent: float
    sand_percent: float
    silt_percent: float
    organic_carbon_g_kg: float
    ph: float
    nitrogen_g_kg: float
    cec_cmol_kg: float = 0.0
    bulk_density_kg_m3: float = 0.0
    soil_type: str = "Loam"
    drainage_class: str = "Moderate"


# =============================================================================
# Models
# =============================================================================

class FetchImagesRequest(BaseModel):
    latitude: float
    longitude: float
    start_date: Optional[str] = None  # YYYY-MM-DD, defaults to 1 year ago
    end_date: Optional[str] = None    # YYYY-MM-DD, defaults to today
    buffer_meters: int = 5000         # Area around point
    interval_days: int = 5            # Days between images


class FetchImagesResponse(BaseModel):
    images: list[dict]  # List of {date, cloud_pct, url}
    count: int
    location: dict


class TerrainRequest(BaseModel):
    latitude: float
    longitude: float


class TerrainResponse(BaseModel):
    elevation: float
    slope: float
    aspect: float


class LandcoverRequest(BaseModel):
    latitude: float
    longitude: float
    buffer_meters: int = 1000


class LandcoverResponse(BaseModel):
    dominant_class: str
    class_percentages: dict[str, float]


class EmbeddingStabilityRequest(BaseModel):
    latitude: float
    longitude: float
    buffer_meters: int = 1000


class EmbeddingStabilityResponse(BaseModel):
    stability_score: float  # 0-1, higher = more stable
    interpretation: str     # "highly_stable", "stable", "moderate_change", "significant_change"
    year_comparisons: dict[str, dict]  # {"2022_to_2023": {"similarity": 0.97, "change": "minimal"}}
    years_analyzed: list[str]


class EmbeddingZonesRequest(BaseModel):
    latitude: float
    longitude: float
    buffer_meters: int = 1000
    num_zones: int = 5
    year: int = 2024


class EmbeddingZonesResponse(BaseModel):
    zones: list[dict]  # [{"zone_id": 1, "percentage": 45.2, "interpretation": "active_vegetation"}]
    total_pixels: int
    year: int


# =============================================================================
# Geospatial-RAG Models
# =============================================================================

class EmbeddingFetchRequest(BaseModel):
    latitude: float
    longitude: float
    year: int = 2024
    buffer_meters: int = 1000


class EmbeddingFetchResponse(BaseModel):
    embedding: list[float]
    location: dict
    year: int


class SimilarFarmsRequest(BaseModel):
    latitude: float
    longitude: float
    crop: str
    limit: int = 5
    year: int = 2024


class SimilarFarmMatch(BaseModel):
    name: str
    region: str
    country: str
    similarity: float
    yield_min: float
    yield_max: float
    yield_typical: float
    elevation_m: Optional[float]
    data_source: str
    seasons_per_year: int = 1
    interpretation: str


class SimilarFarmsResponse(BaseModel):
    query_location: dict
    crop: str
    similar_farms: list[SimilarFarmMatch]
    yield_prediction: dict
    confidence: str
    message: str


# =============================================================================
# Biophysical Suitability Models
# =============================================================================

class TWIRequest(BaseModel):
    """Topographic Wetness Index request - identifies waterlogging risk."""
    latitude: float
    longitude: float
    buffer_meters: int = 1000


class TWIResponse(BaseModel):
    """TWI response with pooling/waterlogging risk assessment."""
    twi_mean: float
    twi_min: float
    twi_max: float
    pooling_risk: str  # "LOW", "MODERATE", "HIGH"
    risk_interpretation: str
    high_risk_percentage: float  # % of area with high TWI (waterlogging prone)


class WaterBalanceRequest(BaseModel):
    """Water balance request - rainfall minus evapotranspiration."""
    latitude: float
    longitude: float
    year: int = 2023  # Use recent complete year
    buffer_meters: int = 5000  # Larger buffer for climate data


class WaterBalanceResponse(BaseModel):
    """Water balance assessment for irrigation planning."""
    annual_rainfall_mm: float
    annual_et_mm: float  # Evapotranspiration
    water_balance_mm: float  # Rainfall - ET (positive = surplus)
    irrigation_needed: bool
    assessment: str
    monthly_pattern: Optional[dict] = None  # Monthly breakdown if available


class SoilMoistureRequest(BaseModel):
    """Soil moisture from Sentinel-1 SAR - cloud-proof measurement."""
    latitude: float
    longitude: float
    buffer_meters: int = 1000
    days_back: int = 30  # Average over recent period


class SoilMoistureResponse(BaseModel):
    """Current soil moisture status from radar."""
    vv_backscatter: float  # Sentinel-1 VV polarization (dB)
    vh_backscatter: float  # Sentinel-1 VH polarization (dB)
    moisture_index: float  # Derived moisture indicator (0-1)
    moisture_status: str  # "DRY", "OPTIMAL", "WET", "SATURATED"
    interpretation: str
    observation_date: str


class HeatStressRequest(BaseModel):
    """Heat stress calculation based on aspect and latitude."""
    latitude: float
    longitude: float
    aspect: Optional[float] = None  # If not provided, will be fetched


class HeatStressResponse(BaseModel):
    """Solar heat stress assessment for crop suitability."""
    aspect_degrees: float
    aspect_direction: str  # "N", "NE", "E", "SE", "S", "SW", "W", "NW"
    latitude: float
    heat_stress_index: float  # 0-1, higher = more PM heat stress
    risk_level: str  # "LOW", "MODERATE", "HIGH", "SEVERE"
    interpretation: str
    recommendation: str


class BiophysicalSuiteRequest(BaseModel):
    """Request all biophysical metrics at once."""
    latitude: float
    longitude: float
    buffer_meters: int = 1000


class BiophysicalSuiteResponse(BaseModel):
    """Complete biophysical suitability assessment."""
    elevation_m: float  # Elevation in meters (for crop requirement matching)
    slope_degrees: float  # Slope in degrees
    twi: TWIResponse
    water_balance: WaterBalanceResponse
    soil_moisture: SoilMoistureResponse
    heat_stress: HeatStressResponse
    overall_suitability: str  # "EXCELLENT", "GOOD", "MODERATE", "POOR"
    key_concerns: list[str]


# =============================================================================
# Consolidated Land Data Models (Flat, Simple Structure)
# =============================================================================

class LandDataRequest(BaseModel):
    """Request for consolidated land data."""
    latitude: float
    longitude: float
    buffer_meters: int = 1000


class LandDataResponse(BaseModel):
    """
    Flat, simple land data structure.
    All metrics at root level for easy Gemini reasoning.
    """
    # Climate
    temp_mean_c: float
    temp_min_c: float
    temp_max_c: float
    annual_rainfall_mm: float
    water_balance_mm: float  # rainfall - evapotranspiration
    sunshine_hours_daily: float
    humidity_percent: float
    
    # Terrain
    elevation_m: float
    slope_degrees: float
    aspect: str  # N, NE, E, SE, S, SW, W, NW
    
    # Soil
    soil_ph: float
    soil_type: str
    soil_drainage: str
    soil_moisture_status: str  # DRY, OPTIMAL, WET, SATURATED
    
    # Risk Assessments
    pooling_risk: str  # LOW, MODERATE, HIGH, SEVERE
    heat_stress_risk: str  # LOW, MODERATE, HIGH, SEVERE
    irrigation_needed: bool
    
    # Summary
    suitability: str  # EXCELLENT, GOOD, MODERATE, POOR
    concerns: list[str]


# =============================================================================
# Reference Farms Seed Data (Documented from Published Sources)
# =============================================================================

# These are real locations with documented agricultural production.
# Yields are from FAO, USDA, local ministry reports, and research papers.
# Embeddings will be fetched from AlphaEarth at startup.
# Focused on 5 premium investment crops: avocado, coffee, cacao, grapes, macadamia

REFERENCE_FARMS_SEED = [
    # =========================================================================
    # HASS AVOCADO - Documented high-yield regions
    # =========================================================================
    {
        "name": "Uruapan Valley",
        "crop": "avocado",
        "lat": 19.42, "lon": -102.06,
        "region": "Michoacán", "country": "Mexico",
        "yield_min": 10.0, "yield_max": 14.0, "yield_typical": 12.0,
        "elevation_m": 1600, "rainfall_mm": 1500, "temp_c": 18,
        "source": "SAGARPA Mexico Avocado Profile 2023"
    },
    {
        "name": "Peribán Highlands",
        "crop": "avocado",
        "lat": 19.52, "lon": -102.42,
        "region": "Michoacán", "country": "Mexico",
        "yield_min": 9.0, "yield_max": 13.0, "yield_typical": 11.0,
        "elevation_m": 1800, "rainfall_mm": 1400, "temp_c": 17,
        "source": "SAGARPA Mexico 2023"
    },
    {
        "name": "Urrao Valley",
        "crop": "avocado",
        "lat": 6.32, "lon": -76.13,
        "region": "Antioquia", "country": "Colombia",
        "yield_min": 8.0, "yield_max": 12.0, "yield_typical": 10.0,
        "elevation_m": 1850, "rainfall_mm": 2200, "temp_c": 18,
        "source": "ICA Colombia Avocado Report 2023"
    },
    {
        "name": "Rionegro Plateau",
        "crop": "avocado",
        "lat": 6.15, "lon": -75.38,
        "region": "Antioquia", "country": "Colombia",
        "yield_min": 7.0, "yield_max": 11.0, "yield_typical": 9.0,
        "elevation_m": 2100, "rainfall_mm": 2000, "temp_c": 17,
        "source": "ICA Colombia 2023"
    },
    {
        "name": "La Ligua Valley",
        "crop": "avocado",
        "lat": -32.45, "lon": -71.23,
        "region": "Valparaíso", "country": "Chile",
        "yield_min": 8.0, "yield_max": 11.0, "yield_typical": 9.5,
        "elevation_m": 400, "rainfall_mm": 350, "temp_c": 15,
        "source": "ODEPA Chile Avocado Statistics 2023"
    },
    {
        "name": "Limpopo Lowveld",
        "crop": "avocado",
        "lat": -23.90, "lon": 30.45,
        "region": "Limpopo", "country": "South Africa",
        "yield_min": 7.0, "yield_max": 10.0, "yield_typical": 8.5,
        "elevation_m": 600, "rainfall_mm": 700, "temp_c": 22,
        "source": "SAAGA South Africa Avocado Growers 2023"
    },
    
    # =========================================================================
    # COFFEE - Premium Arabica regions
    # =========================================================================
    {
        "name": "Huila Highlands",
        "crop": "coffee",
        "lat": 2.05, "lon": -75.75,
        "region": "Huila", "country": "Colombia",
        "yield_min": 1.2, "yield_max": 2.0, "yield_typical": 1.6,
        "elevation_m": 1700, "rainfall_mm": 1800, "temp_c": 19,
        "source": "FNC Colombia Coffee Census 2023"
    },
    {
        "name": "Quindío Coffee Triangle",
        "crop": "coffee",
        "lat": 4.53, "lon": -75.68,
        "region": "Quindío", "country": "Colombia",
        "yield_min": 1.0, "yield_max": 1.8, "yield_typical": 1.4,
        "elevation_m": 1500, "rainfall_mm": 2200, "temp_c": 20,
        "source": "FNC Colombia 2023"
    },
    {
        "name": "Sidama Zone",
        "crop": "coffee",
        "lat": 6.75, "lon": 38.45,
        "region": "Sidama", "country": "Ethiopia",
        "yield_min": 0.6, "yield_max": 1.2, "yield_typical": 0.8,
        "elevation_m": 1900, "rainfall_mm": 1200, "temp_c": 18,
        "source": "ECX Ethiopia Coffee Report 2023"
    },
    {
        "name": "Yirgacheffe Highlands",
        "crop": "coffee",
        "lat": 6.16, "lon": 38.21,
        "region": "Gedeo", "country": "Ethiopia",
        "yield_min": 0.5, "yield_max": 1.0, "yield_typical": 0.7,
        "elevation_m": 2000, "rainfall_mm": 1400, "temp_c": 17,
        "source": "ECX Ethiopia 2023"
    },
    {
        "name": "Minas Gerais Cerrado",
        "crop": "coffee",
        "lat": -21.13, "lon": -45.45,
        "region": "Minas Gerais", "country": "Brazil",
        "yield_min": 1.5, "yield_max": 2.5, "yield_typical": 2.0,
        "elevation_m": 1100, "rainfall_mm": 1500, "temp_c": 21,
        "source": "CONAB Brazil Coffee Report 2023"
    },
    {
        "name": "Dak Lak Plateau",
        "crop": "coffee",
        "lat": 12.67, "lon": 108.05,
        "region": "Central Highlands", "country": "Vietnam",
        "yield_min": 2.5, "yield_max": 3.5, "yield_typical": 3.0,
        "elevation_m": 500, "rainfall_mm": 2000, "temp_c": 24,
        "source": "VICOFA Vietnam Coffee 2023"
    },
    
    # =========================================================================
    # CACAO - Fine flavor regions
    # =========================================================================
    {
        "name": "Tumaco Coast",
        "crop": "cacao",
        "lat": 1.80, "lon": -78.76,
        "region": "Nariño", "country": "Colombia",
        "yield_min": 0.4, "yield_max": 0.8, "yield_typical": 0.6,
        "elevation_m": 50, "rainfall_mm": 3000, "temp_c": 26,
        "source": "Fedecacao Colombia 2023"
    },
    {
        "name": "Santander Mountains",
        "crop": "cacao",
        "lat": 6.64, "lon": -73.65,
        "region": "Santander", "country": "Colombia",
        "yield_min": 0.5, "yield_max": 1.0, "yield_typical": 0.7,
        "elevation_m": 800, "rainfall_mm": 2000, "temp_c": 24,
        "source": "Fedecacao Colombia 2023"
    },
    {
        "name": "Bahia Sul",
        "crop": "cacao",
        "lat": -14.80, "lon": -39.28,
        "region": "Bahia", "country": "Brazil",
        "yield_min": 0.3, "yield_max": 0.6, "yield_typical": 0.45,
        "elevation_m": 200, "rainfall_mm": 1800, "temp_c": 25,
        "source": "CEPLAC Brazil Cacao 2023"
    },
    
    # =========================================================================
    # WINE GRAPES - Premium viticulture regions
    # =========================================================================
    {
        "name": "Napa Valley",
        "crop": "grapes",
        "lat": 38.50, "lon": -122.46,
        "region": "Napa", "country": "USA",
        "yield_min": 4.0, "yield_max": 8.0, "yield_typical": 6.0,
        "elevation_m": 150, "rainfall_mm": 600, "temp_c": 16,
        "source": "Napa Valley Vintners 2023"
    },
    {
        "name": "Maipo Valley",
        "crop": "grapes",
        "lat": -33.73, "lon": -70.68,
        "region": "Metropolitan", "country": "Chile",
        "yield_min": 6.0, "yield_max": 10.0, "yield_typical": 8.0,
        "elevation_m": 500, "rainfall_mm": 350, "temp_c": 15,
        "source": "Wines of Chile 2023"
    },
    {
        "name": "Mendoza Andes",
        "crop": "grapes",
        "lat": -33.00, "lon": -68.85,
        "region": "Mendoza", "country": "Argentina",
        "yield_min": 8.0, "yield_max": 12.0, "yield_typical": 10.0,
        "elevation_m": 900, "rainfall_mm": 200, "temp_c": 16,
        "source": "Wines of Argentina 2023"
    },
    {
        "name": "Stellenbosch Hills",
        "crop": "grapes",
        "lat": -33.93, "lon": 18.86,
        "region": "Western Cape", "country": "South Africa",
        "yield_min": 5.0, "yield_max": 9.0, "yield_typical": 7.0,
        "elevation_m": 150, "rainfall_mm": 650, "temp_c": 17,
        "source": "WOSA South Africa Wine Industry 2023"
    },
    {
        "name": "Bordeaux Médoc",
        "crop": "grapes",
        "lat": 45.13, "lon": -0.78,
        "region": "Nouvelle-Aquitaine", "country": "France",
        "yield_min": 4.0, "yield_max": 7.0, "yield_typical": 5.5,
        "elevation_m": 20, "rainfall_mm": 900, "temp_c": 13,
        "source": "CIVB Bordeaux Wine Council 2023"
    },
    {
        "name": "Villa de Leyva",
        "crop": "grapes",
        "lat": 5.64, "lon": -73.52,
        "region": "Boyacá", "country": "Colombia",
        "yield_min": 3.0, "yield_max": 6.0, "yield_typical": 4.5,
        "elevation_m": 2150, "rainfall_mm": 800, "temp_c": 17,
        "source": "Colombian Wine Producers Assoc 2023"
    },
    
    # =========================================================================
    # MACADAMIA - Premium nut regions
    # =========================================================================
    {
        "name": "Kona Coast",
        "crop": "macadamia",
        "lat": 19.64, "lon": -155.99,
        "region": "Hawaii", "country": "USA",
        "yield_min": 2.5, "yield_max": 4.5, "yield_typical": 3.5,
        "elevation_m": 450, "rainfall_mm": 1800, "temp_c": 23,
        "source": "Hawaii Macadamia Nut Association 2023"
    },
    {
        "name": "Bundaberg Region",
        "crop": "macadamia",
        "lat": -24.87, "lon": 152.35,
        "region": "Queensland", "country": "Australia",
        "yield_min": 3.0, "yield_max": 5.0, "yield_typical": 4.0,
        "elevation_m": 50, "rainfall_mm": 1100, "temp_c": 21,
        "source": "Australian Macadamia Society 2023"
    },
    {
        "name": "Lismore Hills",
        "crop": "macadamia",
        "lat": -28.81, "lon": 153.28,
        "region": "New South Wales", "country": "Australia",
        "yield_min": 2.8, "yield_max": 4.5, "yield_typical": 3.6,
        "elevation_m": 200, "rainfall_mm": 1400, "temp_c": 20,
        "source": "Australian Macadamia Society 2023"
    },
    {
        "name": "Mpumalanga Lowveld",
        "crop": "macadamia",
        "lat": -25.47, "lon": 30.98,
        "region": "Mpumalanga", "country": "South Africa",
        "yield_min": 3.0, "yield_max": 5.5, "yield_typical": 4.2,
        "elevation_m": 400, "rainfall_mm": 800, "temp_c": 22,
        "source": "SAMAC South Africa Macadamia 2023"
    },
    {
        "name": "Limpopo Estates",
        "crop": "macadamia",
        "lat": -23.70, "lon": 30.20,
        "region": "Limpopo", "country": "South Africa",
        "yield_min": 2.5, "yield_max": 4.5, "yield_typical": 3.5,
        "elevation_m": 600, "rainfall_mm": 700, "temp_c": 23,
        "source": "SAMAC South Africa 2023"
    },
    {
        "name": "Chinchiná Valley",
        "crop": "macadamia",
        "lat": 4.98, "lon": -75.60,
        "region": "Caldas", "country": "Colombia",
        "yield_min": 2.0, "yield_max": 4.0, "yield_typical": 3.0,
        "elevation_m": 1300, "rainfall_mm": 2000, "temp_c": 21,
        "source": "Asohofrucol Colombia 2023"
    },
]


async def seed_reference_farms():
    """Seed reference farms with real AlphaEarth embeddings."""
    conn = get_db()
    cur = conn.cursor(cursor_factory=RealDictCursor)
    
    try:
        # Check if we already have farms
        cur.execute("SELECT COUNT(*) as count FROM reference_farms")
        count = cur.fetchone()["count"]
        
        if count >= len(REFERENCE_FARMS_SEED):
            logger.info(f"Reference farms already seeded ({count} farms)")
            return
        
        logger.info(f"Seeding {len(REFERENCE_FARMS_SEED)} reference farms with AlphaEarth embeddings...")
        
        seeded = 0
        for farm in REFERENCE_FARMS_SEED:
            try:
                # Check if this farm already exists
                cur.execute(
                    "SELECT id FROM reference_farms WHERE crop = %s AND latitude = %s AND longitude = %s",
                    (farm["crop"], farm["lat"], farm["lon"])
                )
                if cur.fetchone():
                    continue
                
                # Fetch AlphaEarth embedding for this location
                embedding = get_embedding_vector(farm["lat"], farm["lon"], 2024, 1000)
                
                if embedding is None:
                    logger.warning(f"Could not fetch embedding for {farm['name']}, skipping")
                    continue
                
                # Insert the farm
                cur.execute("""
                    INSERT INTO reference_farms 
                    (name, crop, latitude, longitude, embedding, 
                     yield_min, yield_max, yield_typical,
                     region, country, elevation_m, annual_rainfall_mm, avg_temp_c,
                     data_source)
                    VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)
                    ON CONFLICT (crop, latitude, longitude) DO NOTHING
                """, (
                    farm["name"], farm["crop"], farm["lat"], farm["lon"], embedding,
                    farm["yield_min"], farm["yield_max"], farm["yield_typical"],
                    farm["region"], farm["country"], farm["elevation_m"],
                    farm["rainfall_mm"], farm["temp_c"], farm["source"]
                ))
                
                seeded += 1
                logger.info(f"Seeded: {farm['name']} ({farm['crop']})")
                
            except Exception as e:
                logger.warning(f"Failed to seed {farm['name']}: {e}")
                continue
        
        conn.commit()
        logger.info(f"Successfully seeded {seeded} reference farms")
        
    except Exception as e:
        conn.rollback()
        logger.error(f"Failed to seed reference farms: {e}")
    finally:
        cur.close()


# =============================================================================
# Health
# =============================================================================

@app.get("/health")
def health():
    """Health check endpoint"""
    return {"status": "ok", "service": "gee-service", "project": PROJECT}


# =============================================================================
# Satellite Images
# =============================================================================

def calculate_local_cloud_percentage(image: ee.Image, region: ee.Geometry) -> float:
    """
    Calculate cloud percentage specifically within the region of interest.
    Uses the SCL (Scene Classification Layer) band from Sentinel-2.
    
    SCL classes 8, 9, 10 are cloud-related:
    - 8: Cloud medium probability
    - 9: Cloud high probability  
    - 10: Thin cirrus
    
    This is much more accurate than CLOUDY_PIXEL_PERCENTAGE which is for the full tile.
    """
    try:
        scl = image.select("SCL")
        
        # Create cloud mask: SCL classes 8, 9, 10 are clouds/cirrus
        cloud_mask = scl.eq(8).Or(scl.eq(9)).Or(scl.eq(10))
        
        # Calculate percentage of cloudy pixels in the region
        cloud_stats = cloud_mask.reduceRegion(
            reducer=ee.Reducer.mean(),
            geometry=region,
            scale=20,  # SCL resolution is 20m
            maxPixels=1e6
        )
        
        cloud_fraction = cloud_stats.get("SCL").getInfo()
        if cloud_fraction is None:
            return 100.0  # Assume cloudy if we can't calculate
        
        return round(cloud_fraction * 100, 1)
    except Exception as e:
        logger.warning(f"Failed to calculate local cloud %: {e}")
        return 100.0  # Assume cloudy on error


@app.post("/fetch-images", response_model=FetchImagesResponse)
def fetch_images(req: FetchImagesRequest):
    """
    Fetch Sentinel-2 satellite images for a location over time.
    Returns URLs to download each image.
    
    Cloud percentage is calculated specifically for the region of interest
    using the SCL band, not the tile-wide metadata.
    """
    # Default dates: 1 year ago to today
    if req.end_date:
        end = datetime.strptime(req.end_date, "%Y-%m-%d")
    else:
        end = datetime.now()
    
    if req.start_date:
        start = datetime.strptime(req.start_date, "%Y-%m-%d")
    else:
        start = end - timedelta(days=365)
    
    # Create point and buffer
    point = ee.Geometry.Point([req.longitude, req.latitude])
    region = point.buffer(req.buffer_meters).bounds()
    
    # Get Sentinel-2 imagery - pre-filter by tile cloud % to reduce API calls
    collection = (
        ee.ImageCollection("COPERNICUS/S2_SR_HARMONIZED")
        .filterBounds(point)
        .filterDate(start.strftime("%Y-%m-%d"), end.strftime("%Y-%m-%d"))
        .filter(ee.Filter.lt("CLOUDY_PIXEL_PERCENTAGE", 70))  # Pre-filter very cloudy tiles
        .filter(ee.Filter.eq("SPACECRAFT_NAME", "Sentinel-2A"))  # Single satellite
        .sort("system:time_start")
    )
    
    # Sample every N days
    images_info = []
    current_date = start
    
    while current_date <= end:
        date_str = current_date.strftime("%Y-%m-%d")
        next_date = current_date + timedelta(days=req.interval_days)
        next_str = next_date.strftime("%Y-%m-%d")
        
        # Get images in this window
        window = collection.filterDate(date_str, next_str)
        count = window.size().getInfo()
        
        if count > 0:
            # Get the image with least tile-wide clouds as starting point
            image = window.sort("CLOUDY_PIXEL_PERCENTAGE").first()
            
            # Calculate ACTUAL cloud percentage for our specific region
            local_cloud_pct = calculate_local_cloud_percentage(image, region)
            
            # Get metadata
            props = image.getInfo()["properties"]
            timestamp = props.get("system:time_start", 0)
            img_date = datetime.fromtimestamp(timestamp / 1000).strftime("%Y-%m-%d")
            
            # Log for debugging
            tile_cloud = props.get("CLOUDY_PIXEL_PERCENTAGE", 0)
            logger.info(f"Image {img_date}: tile_cloud={tile_cloud:.1f}%, local_cloud={local_cloud_pct:.1f}%")
            
            # Create visualization
            rgb = image.select(["B4", "B3", "B2"])
            vis_params = {
                "min": 0,
                "max": 3000,
                "dimensions": 1000,
                "region": region,
                "format": "png",
            }
            
            # Get download URL
            url = rgb.getThumbURL(vis_params)
            
            images_info.append({
                "date": img_date,
                "cloud_pct": local_cloud_pct,  # Use LOCAL cloud %, not tile metadata
                "url": url,
            })
        
        current_date = next_date
    
    return FetchImagesResponse(
        images=images_info,
        count=len(images_info),
        location={
            "latitude": req.latitude,
            "longitude": req.longitude,
            "buffer_meters": req.buffer_meters,
        },
    )


# =============================================================================
# Terrain
# =============================================================================

@app.post("/fetch-terrain", response_model=TerrainResponse)
def fetch_terrain(req: TerrainRequest):
    """
    Fetch terrain data (elevation, slope, aspect) for a location.
    Uses SRTM Digital Elevation Model.
    """
    point = ee.Geometry.Point([req.longitude, req.latitude])
    
    # Get SRTM elevation
    srtm = ee.Image("USGS/SRTMGL1_003")
    elevation = srtm.sample(point, 30).first().get("elevation").getInfo()
    
    # Calculate slope and aspect
    terrain = ee.Terrain.products(srtm)
    sample = terrain.sample(point, 30).first()
    
    slope = sample.get("slope").getInfo()
    aspect = sample.get("aspect").getInfo()
    
    return TerrainResponse(
        elevation=round(elevation, 1),
        slope=round(slope, 2),
        aspect=round(aspect, 1),
    )


# =============================================================================
# Land Cover
# =============================================================================

ESA_WORLDCOVER_CLASSES = {
    10: "Tree cover",
    20: "Shrubland",
    30: "Grassland",
    40: "Cropland",
    50: "Built-up",
    60: "Bare / sparse vegetation",
    70: "Snow and ice",
    80: "Permanent water bodies",
    90: "Herbaceous wetland",
    95: "Mangroves",
    100: "Moss and lichen",
}


@app.post("/fetch-landcover", response_model=LandcoverResponse)
def fetch_landcover(req: LandcoverRequest):
    """
    Fetch land cover classification for a location.
    Uses ESA WorldCover 2021.
    """
    point = ee.Geometry.Point([req.longitude, req.latitude])
    region = point.buffer(req.buffer_meters)
    
    # Get ESA WorldCover
    worldcover = ee.Image("ESA/WorldCover/v200/2021")
    
    # Get dominant class at point
    dominant = worldcover.sample(point, 10).first().get("Map").getInfo()
    dominant_class = ESA_WORLDCOVER_CLASSES.get(dominant, "Unknown")
    
    # Calculate class percentages in buffer
    histogram = worldcover.reduceRegion(
        reducer=ee.Reducer.frequencyHistogram(),
        geometry=region,
        scale=10,
        maxPixels=1e8,
    ).get("Map").getInfo()
    
    # Convert to percentages
    total = sum(histogram.values())
    percentages = {}
    for class_code, count in histogram.items():
        class_name = ESA_WORLDCOVER_CLASSES.get(int(class_code), f"Class {class_code}")
        percentages[class_name] = round((count / total) * 100, 1)
    
    return LandcoverResponse(
        dominant_class=dominant_class,
        class_percentages=percentages,
    )


# =============================================================================
# Biophysical Suitability Analysis
# =============================================================================

def _calculate_twi(lat: float, lon: float, buffer_meters: int = 1000) -> TWIResponse:
    """
    Calculate Topographic Wetness Index (TWI) for waterlogging risk assessment.
    
    TWI = ln(upstream_area / tan(slope))
    High TWI = water accumulation zones = root rot risk
    """
    point = ee.Geometry.Point([lon, lat])
    region = point.buffer(buffer_meters)
    
    # Get SRTM DEM
    srtm = ee.Image("USGS/SRTMGL1_003")
    
    # Calculate slope in radians
    slope_rad = ee.Terrain.slope(srtm).multiply(ee.Number(3.14159).divide(180))
    
    # Calculate flow accumulation (proxy for upstream contributing area)
    # Using MERIT Hydro dataset which has flow accumulation pre-computed
    try:
        merit_hydro = ee.Image("MERIT/Hydro/v1_0_1")
        flow_acc = merit_hydro.select("upa")  # Upstream drainage area
        
        # Calculate TWI: ln(a / tan(b)) where a = flow accumulation, b = slope
        # Add small value to avoid log(0) and division by zero
        tan_slope = slope_rad.tan().add(0.001)
        twi = flow_acc.add(1).log().subtract(tan_slope.log())
        
    except Exception:
        # Fallback: Use simpler slope-based wetness proxy
        # Flatter areas = higher wetness potential
        tan_slope = slope_rad.tan().add(0.001)
        twi = tan_slope.multiply(-1).add(1)  # Inverse of slope as proxy
    
    # Get statistics over the region
    stats = twi.reduceRegion(
        reducer=ee.Reducer.mean().combine(
            ee.Reducer.min(), sharedInputs=True
        ).combine(
            ee.Reducer.max(), sharedInputs=True
        ).combine(
            ee.Reducer.percentile([90]), sharedInputs=True
        ),
        geometry=region,
        scale=30,
        maxPixels=1e8,
    ).getInfo()
    
    # Extract values (handle different possible key names)
    twi_mean = stats.get("upa_mean") or stats.get("slope_mean") or 5.0
    twi_min = stats.get("upa_min") or stats.get("slope_min") or 2.0
    twi_max = stats.get("upa_max") or stats.get("slope_max") or 10.0
    
    # Determine risk level based on mean TWI
    # TWI > 8 is typically considered high wetness
    if twi_mean < 5:
        pooling_risk = "LOW"
        risk_interpretation = "Well-drained terrain with minimal waterlogging risk. Suitable for most crops."
        high_risk_pct = 5.0
    elif twi_mean < 7:
        pooling_risk = "MODERATE"
        risk_interpretation = "Some low-lying areas may accumulate water during heavy rains. Consider drainage improvements."
        high_risk_pct = 15.0
    elif twi_mean < 9:
        pooling_risk = "HIGH"
        risk_interpretation = "Significant waterlogging risk. Many areas prone to water pooling. Drainage infrastructure required."
        high_risk_pct = 35.0
    else:
        pooling_risk = "SEVERE"
        risk_interpretation = "Very high waterlogging risk. Not suitable for root-sensitive crops without major drainage work."
        high_risk_pct = 60.0
    
    return TWIResponse(
        twi_mean=round(float(twi_mean), 2),
        twi_min=round(float(twi_min), 2),
        twi_max=round(float(twi_max), 2),
        pooling_risk=pooling_risk,
        risk_interpretation=risk_interpretation,
        high_risk_percentage=high_risk_pct,
    )


def _calculate_water_balance(
    lat: float, lon: float, year: int = 2023, buffer_meters: int = 5000
) -> WaterBalanceResponse:
    """
    Calculate annual water balance: Rainfall - Evapotranspiration.
    
    Positive balance = self-sustaining (no irrigation needed)
    Negative balance = irrigation required
    
    Uses CHIRPS for rainfall and MODIS for ET.
    """
    point = ee.Geometry.Point([lon, lat])
    region = point.buffer(buffer_meters)
    
    start_date = f"{year}-01-01"
    end_date = f"{year}-12-31"
    
    # Fetch CHIRPS rainfall (mm/day) and sum for the year
    try:
        chirps = (
            ee.ImageCollection("UCSB-CHG/CHIRPS/DAILY")
            .filterDate(start_date, end_date)
            .filterBounds(point)
        )
        
        annual_rainfall = chirps.select("precipitation").sum()
        rainfall_stats = annual_rainfall.reduceRegion(
            reducer=ee.Reducer.mean(),
            geometry=region,
            scale=5000,
            maxPixels=1e8,
        ).getInfo()
        
        annual_rainfall_mm = rainfall_stats.get("precipitation", 1200)
    except Exception as e:
        logger.warning(f"CHIRPS fetch failed: {e}, using fallback")
        annual_rainfall_mm = 1200  # Default fallback
    
    # Fetch MODIS ET (evapotranspiration in kg/m2/8day = mm/8day)
    try:
        modis_et = (
            ee.ImageCollection("MODIS/061/MOD16A2")
            .filterDate(start_date, end_date)
            .filterBounds(point)
        )
        
        # ET is in kg/m²/8day, multiply by 0.1 to get mm, sum for year
        annual_et = modis_et.select("ET").sum().multiply(0.1)
        et_stats = annual_et.reduceRegion(
            reducer=ee.Reducer.mean(),
            geometry=region,
            scale=500,
            maxPixels=1e8,
        ).getInfo()
        
        annual_et_mm = et_stats.get("ET", 800)
    except Exception as e:
        logger.warning(f"MODIS ET fetch failed: {e}, using fallback")
        annual_et_mm = 800  # Default fallback
    
    # Calculate water balance
    water_balance = float(annual_rainfall_mm) - float(annual_et_mm)
    
    # Determine irrigation needs
    if water_balance > 300:
        irrigation_needed = False
        assessment = f"Excellent water surplus (+{water_balance:.0f}mm). Land is self-sustaining with no irrigation required."
    elif water_balance > 0:
        irrigation_needed = False
        assessment = f"Positive water balance (+{water_balance:.0f}mm). Marginally self-sustaining, supplemental irrigation during dry spells recommended."
    elif water_balance > -200:
        irrigation_needed = True
        assessment = f"Slight water deficit ({water_balance:.0f}mm). Supplemental irrigation required during dry season."
    elif water_balance > -500:
        irrigation_needed = True
        assessment = f"Moderate water deficit ({water_balance:.0f}mm). Regular irrigation infrastructure required."
    else:
        irrigation_needed = True
        assessment = f"Severe water deficit ({water_balance:.0f}mm). Extensive irrigation essential. Consider water rights and infrastructure costs."
    
    return WaterBalanceResponse(
        annual_rainfall_mm=round(float(annual_rainfall_mm), 1),
        annual_et_mm=round(float(annual_et_mm), 1),
        water_balance_mm=round(water_balance, 1),
        irrigation_needed=irrigation_needed,
        assessment=assessment,
        monthly_pattern=None,  # Could add monthly breakdown in future
    )


def _calculate_soil_moisture(
    lat: float, lon: float, buffer_meters: int = 1000, days_back: int = 30
) -> SoilMoistureResponse:
    """
    Fetch current soil moisture using Sentinel-1 SAR backscatter.
    
    SAR provides cloud-proof soil moisture estimation.
    VV and VH polarization backscatter correlate with soil water content.
    """
    point = ee.Geometry.Point([lon, lat])
    region = point.buffer(buffer_meters)
    
    # Get recent Sentinel-1 data
    end_date = datetime.now()
    start_date = end_date - timedelta(days=days_back)
    
    try:
        s1 = (
            ee.ImageCollection("COPERNICUS/S1_GRD")
            .filterDate(start_date.strftime("%Y-%m-%d"), end_date.strftime("%Y-%m-%d"))
            .filterBounds(point)
            .filter(ee.Filter.eq("instrumentMode", "IW"))
            .filter(ee.Filter.listContains("transmitterReceiverPolarisation", "VV"))
            .filter(ee.Filter.listContains("transmitterReceiverPolarisation", "VH"))
            .select(["VV", "VH"])
        )
        
        count = s1.size().getInfo()
        if count == 0:
            raise ValueError("No Sentinel-1 data available for this period")
        
        # Get mean backscatter over the period
        s1_mean = s1.mean()
        
        stats = s1_mean.reduceRegion(
            reducer=ee.Reducer.mean(),
            geometry=region,
            scale=10,
            maxPixels=1e8,
        ).getInfo()
        
        vv_db = stats.get("VV", -12.0)
        vh_db = stats.get("VH", -18.0)
        
        # Get the most recent observation date
        latest = s1.sort("system:time_start", False).first()
        obs_date = datetime.fromtimestamp(
            latest.get("system:time_start").getInfo() / 1000
        ).strftime("%Y-%m-%d")
        
    except Exception as e:
        logger.warning(f"Sentinel-1 fetch failed: {e}, using fallback values")
        vv_db = -12.0
        vh_db = -18.0
        obs_date = datetime.now().strftime("%Y-%m-%d")
    
    # Convert backscatter to moisture index
    # Higher backscatter (less negative) = wetter soil
    # Typical range: VV from -20 (dry) to -5 (wet)
    # Normalize to 0-1 scale
    moisture_index = (float(vv_db) + 20) / 15  # Maps -20 to 0, -5 to 1
    moisture_index = max(0, min(1, moisture_index))  # Clamp to 0-1
    
    # Classify moisture status
    if moisture_index < 0.2:
        moisture_status = "DRY"
        interpretation = "Soil is very dry. Immediate irrigation may be needed for sensitive crops."
    elif moisture_index < 0.4:
        moisture_status = "MODERATELY_DRY"
        interpretation = "Below-average soil moisture. Monitor closely and prepare for irrigation."
    elif moisture_index < 0.6:
        moisture_status = "OPTIMAL"
        interpretation = "Soil moisture is in the optimal range for most crops."
    elif moisture_index < 0.8:
        moisture_status = "WET"
        interpretation = "Above-average moisture. Good for establishment but watch for fungal issues."
    else:
        moisture_status = "SATURATED"
        interpretation = "Soil is near saturation. Risk of waterlogging and root damage."
    
    return SoilMoistureResponse(
        vv_backscatter=round(float(vv_db), 2),
        vh_backscatter=round(float(vh_db), 2),
        moisture_index=round(moisture_index, 3),
        moisture_status=moisture_status,
        interpretation=interpretation,
        observation_date=obs_date,
    )


def _calculate_heat_stress(
    lat: float, lon: float, aspect: Optional[float] = None
) -> HeatStressResponse:
    """
    Calculate solar heat stress based on slope aspect and latitude.
    
    In the tropics/subtropics:
    - West-facing slopes (225-315°) receive intense afternoon sun = high heat stress
    - East-facing slopes (45-135°) receive morning sun = lower stress
    - South-facing (in Northern Hemisphere) = more sun exposure
    """
    point = ee.Geometry.Point([lon, lat])
    
    # Fetch aspect if not provided
    if aspect is None:
        srtm = ee.Image("USGS/SRTMGL1_003")
        terrain = ee.Terrain.products(srtm)
        aspect_value = terrain.select("aspect").sample(point, 30).first().get("aspect").getInfo()
    else:
        aspect_value = aspect
    
    aspect_deg = float(aspect_value)
    
    # Determine aspect direction
    if aspect_deg < 22.5 or aspect_deg >= 337.5:
        direction = "N"
    elif aspect_deg < 67.5:
        direction = "NE"
    elif aspect_deg < 112.5:
        direction = "E"
    elif aspect_deg < 157.5:
        direction = "SE"
    elif aspect_deg < 202.5:
        direction = "S"
    elif aspect_deg < 247.5:
        direction = "SW"
    elif aspect_deg < 292.5:
        direction = "W"
    else:
        direction = "NW"
    
    # Calculate heat stress index
    # Base stress from aspect (west-facing = highest)
    if 225 <= aspect_deg < 315:  # West-facing
        aspect_stress = 1.0
    elif 202.5 <= aspect_deg < 337.5:  # SW to NW
        aspect_stress = 0.7
    elif 157.5 <= aspect_deg < 202.5:  # South-facing
        aspect_stress = 0.5 if lat > 0 else 0.3  # More stress in N hemisphere
    elif 45 <= aspect_deg < 135:  # East-facing
        aspect_stress = 0.2
    else:  # North-facing
        aspect_stress = 0.3 if lat > 0 else 0.5
    
    # Latitude modifier (tropics get more intense sun)
    if abs(lat) < 15:  # Deep tropics
        lat_modifier = 1.2
    elif abs(lat) < 25:  # Subtropics
        lat_modifier = 1.0
    else:  # Temperate
        lat_modifier = 0.8
    
    # Combined heat stress index (0-1)
    heat_stress_index = min(1.0, aspect_stress * lat_modifier)
    
    # Determine risk level
    if heat_stress_index < 0.3:
        risk_level = "LOW"
        interpretation = "Minimal afternoon heat stress. Favorable orientation for heat-sensitive crops."
        recommendation = "No specific shade requirements."
    elif heat_stress_index < 0.5:
        risk_level = "MODERATE"
        interpretation = "Moderate afternoon sun exposure. Most crops will tolerate this."
        recommendation = "Consider shade trees for sensitive crops like cacao or young coffee."
    elif heat_stress_index < 0.7:
        risk_level = "HIGH"
        interpretation = "Significant afternoon heat stress. Challenging for shade-loving crops."
        recommendation = "Shade infrastructure or intercropping with taller species strongly recommended."
    else:
        risk_level = "SEVERE"
        interpretation = "Extreme afternoon sun exposure. High risk of heat damage to crops."
        recommendation = "Extensive shade canopy required. Consider alternative crops or aspect."
    
    return HeatStressResponse(
        aspect_degrees=round(aspect_deg, 1),
        aspect_direction=direction,
        latitude=lat,
        heat_stress_index=round(heat_stress_index, 3),
        risk_level=risk_level,
        interpretation=interpretation,
        recommendation=recommendation,
    )


@app.post("/fetch-biophysical", response_model=BiophysicalSuiteResponse)
def fetch_biophysical(req: BiophysicalSuiteRequest):
    """
    Fetch all biophysical metrics in a single call.
    
    Combines: Elevation, Slope, TWI, Water Balance, Soil Moisture, and Heat Stress
    into a comprehensive suitability assessment.
    
    This is the primary biophysical analysis endpoint.
    """
    lat = req.latitude
    lon = req.longitude
    buffer = req.buffer_meters
    
    # Fetch terrain data (elevation, slope)
    point = ee.Geometry.Point([lon, lat])
    srtm = ee.Image("USGS/SRTMGL1_003")
    elevation = srtm.sample(point, 30).first().get("elevation").getInfo()
    terrain = ee.Terrain.products(srtm)
    terrain_sample = terrain.sample(point, 30).first()
    slope = terrain_sample.get("slope").getInfo()
    
    # Fetch all metrics using internal functions
    twi = _calculate_twi(lat, lon, buffer)
    water_balance = _calculate_water_balance(lat, lon, year=2023, buffer_meters=5000)
    soil_moisture = _calculate_soil_moisture(lat, lon, buffer, days_back=30)
    heat_stress = _calculate_heat_stress(lat, lon)
    
    # Aggregate concerns
    key_concerns = []
    score = 4  # Start with max score
    
    if twi.pooling_risk in ["HIGH", "SEVERE"]:
        key_concerns.append(f"High waterlogging risk (TWI: {twi.pooling_risk})")
        score -= 1
    
    if water_balance.irrigation_needed and water_balance.water_balance_mm < -200:
        key_concerns.append(f"Significant water deficit ({water_balance.water_balance_mm:.0f}mm)")
        score -= 1
    
    if soil_moisture.moisture_status in ["DRY", "SATURATED"]:
        key_concerns.append(f"Current soil moisture: {soil_moisture.moisture_status}")
        score -= 0.5
    
    if heat_stress.risk_level in ["HIGH", "SEVERE"]:
        key_concerns.append(f"High heat stress ({heat_stress.aspect_direction}-facing slope)")
        score -= 0.5
    
    # Determine overall suitability
    if score >= 3.5:
        overall = "EXCELLENT"
    elif score >= 2.5:
        overall = "GOOD"
    elif score >= 1.5:
        overall = "MODERATE"
    else:
        overall = "POOR"
    
    if not key_concerns:
        key_concerns.append("No significant biophysical concerns identified")
    
    return BiophysicalSuiteResponse(
        elevation_m=round(float(elevation), 1),
        slope_degrees=round(float(slope), 2),
        twi=twi,
        water_balance=water_balance,
        soil_moisture=soil_moisture,
        heat_stress=heat_stress,
        overall_suitability=overall,
        key_concerns=key_concerns,
    )


# =============================================================================
# Weather Data (Open-Meteo)
# =============================================================================

def _fetch_weather(lat: float, lon: float) -> WeatherData:
    """Fetch historical weather data from Open-Meteo API."""
    import requests
    from datetime import datetime, timedelta
    
    end_date = datetime.now()
    start_date = end_date - timedelta(days=365)
    
    url = (
        f"https://archive-api.open-meteo.com/v1/archive?"
        f"latitude={lat}&longitude={lon}"
        f"&start_date={start_date.strftime('%Y-%m-%d')}"
        f"&end_date={end_date.strftime('%Y-%m-%d')}"
        f"&daily=temperature_2m_mean,temperature_2m_min,temperature_2m_max,"
        f"precipitation_sum,relative_humidity_2m_mean,sunshine_duration"
        f"&hourly=cloud_cover&timezone=auto"
    )
    
    try:
        resp = requests.get(url, timeout=30)
        resp.raise_for_status()
        data = resp.json()
        
        daily = data.get("daily", {})
        hourly = data.get("hourly", {})
        
        temps = [t for t in daily.get("temperature_2m_mean", []) if t is not None]
        temp_mins = [t for t in daily.get("temperature_2m_min", []) if t is not None]
        temp_maxs = [t for t in daily.get("temperature_2m_max", []) if t is not None]
        precips = [p for p in daily.get("precipitation_sum", []) if p is not None]
        humidities = [h for h in daily.get("relative_humidity_2m_mean", []) if h is not None]
        sunshine = [s for s in daily.get("sunshine_duration", []) if s is not None]  # seconds
        cloud = [c for c in hourly.get("cloud_cover", []) if c is not None]
        
        return WeatherData(
            temp_mean=round(sum(temps) / len(temps), 1) if temps else 20.0,
            temp_min=round(min(temp_mins), 1) if temp_mins else 15.0,
            temp_max=round(max(temp_maxs), 1) if temp_maxs else 30.0,
            precipitation_total_mm=round(sum(precips), 1) if precips else 1000.0,
            precipitation_days=len([p for p in precips if p > 1.0]),
            humidity_mean=round(sum(humidities) / len(humidities), 1) if humidities else 70.0,
            sunshine_hours_daily_avg=round(sum(sunshine) / len(sunshine) / 3600, 1) if sunshine else 6.0,
            cloud_cover_mean_percent=round(sum(cloud) / len(cloud), 1) if cloud else 50.0,
        )
    except Exception as e:
        logger.warning(f"Weather fetch failed: {e}, using defaults")
        return WeatherData(
            temp_mean=20.0, temp_min=15.0, temp_max=30.0,
            precipitation_total_mm=1000.0, precipitation_days=100,
            humidity_mean=70.0, sunshine_hours_daily_avg=6.0,
            cloud_cover_mean_percent=50.0,
        )


# =============================================================================
# Soil Data (SoilGrids)
# =============================================================================

def _fetch_soil(lat: float, lon: float) -> SoilData:
    """Fetch soil composition from SoilGrids API."""
    import requests
    
    # SoilGrids REST API
    base_url = "https://rest.isric.org/soilgrids/v2.0/properties/query"
    params = {
        "lon": lon,
        "lat": lat,
        "property": ["phh2o", "soc", "nitrogen", "sand", "clay", "silt"],
        "depth": "0-5cm",
        "value": "mean"
    }
    
    try:
        resp = requests.get(base_url, params=params, timeout=30)
        resp.raise_for_status()
        data = resp.json()
        
        # Extract values from response
        props = {}
        for layer in data.get("properties", {}).get("layers", []):
            name = layer.get("name", "")
            depths = layer.get("depths", [])
            if depths:
                value = depths[0].get("values", {}).get("mean")
                props[name] = value
        
        ph = (props.get("phh2o", 65) or 65) / 10  # Convert from 10x to actual pH
        soc = (props.get("soc", 20) or 20) / 10  # dg/kg to g/kg
        nitrogen = (props.get("nitrogen", 2) or 2) / 100  # cg/kg to g/kg
        sand = (props.get("sand", 40) or 40) / 10  # g/kg to %
        clay = (props.get("clay", 30) or 30) / 10
        site = (props.get("silt", 30) or 30) / 10
        
        # Determine soil type based on texture
        if sand > 70:
            soil_type = "Sandy"
            drainage = "Excessive"
        elif clay > 40:
            soil_type = "Clay"
            drainage = "Poor"
        elif soc > 3:
            soil_type = "Organic-rich Loam"
            drainage = "Good"
        else:
            soil_type = "Loam"
            drainage = "Moderate"
        
        return SoilData(
            ph=round(ph, 1),
            organic_carbon_g_kg=round(soc, 1),
            nitrogen_g_kg=round(nitrogen, 2),
            sand_percent=round(sand, 1),
            clay_percent=round(clay, 1),
            silt_percent=round(site, 1),
            soil_type=soil_type,
            drainage_class=drainage,
        )
    except Exception as e:
        logger.warning(f"SoilGrids fetch failed: {e}, using defaults")
        return SoilData(
            ph=6.5, organic_carbon_g_kg=20.0, nitrogen_g_kg=2.0,
            sand_percent=40.0, clay_percent=30.0, silt_percent=30.0,
            soil_type="Loam", drainage_class="Moderate",
        )


# =============================================================================
# Consolidated Land Data Endpoint
# =============================================================================

@app.post("/fetch-land-data", response_model=LandDataResponse)
def fetch_land_data(req: LandDataRequest):
    """
    Fetch ALL land data in a single call - FLAT, SIMPLE structure.
    
    Returns all metrics at root level for easy Gemini reasoning.
    """
    lat = req.latitude
    lon = req.longitude
    buffer = req.buffer_meters
    
    # Fetch weather from Open-Meteo
    weather = _fetch_weather(lat, lon)
    
    # Fetch soil from SoilGrids
    soil = _fetch_soil(lat, lon)
    
    # Fetch terrain from GEE
    point = ee.Geometry.Point([lon, lat])
    srtm = ee.Image("USGS/SRTMGL1_003")
    elevation = srtm.sample(point, 30).first().get("elevation").getInfo()
    terrain = ee.Terrain.products(srtm)
    terrain_sample = terrain.sample(point, 30).first()
    slope = terrain_sample.get("slope").getInfo()
    aspect_deg = terrain_sample.get("aspect").getInfo()
    
    # Convert aspect to direction
    if aspect_deg < 22.5 or aspect_deg >= 337.5:
        aspect = "N"
    elif aspect_deg < 67.5:
        aspect = "NE"
    elif aspect_deg < 112.5:
        aspect = "E"
    elif aspect_deg < 157.5:
        aspect = "SE"
    elif aspect_deg < 202.5:
        aspect = "S"
    elif aspect_deg < 247.5:
        aspect = "SW"
    elif aspect_deg < 292.5:
        aspect = "W"
    else:
        aspect = "NW"
    
    # Fetch biophysical metrics
    twi = _calculate_twi(lat, lon, buffer)
    water_balance = _calculate_water_balance(lat, lon, year=2023, buffer_meters=5000)
    soil_moisture = _calculate_soil_moisture(lat, lon, buffer, days_back=30)
    heat_stress = _calculate_heat_stress(lat, lon)
    
    # Aggregate concerns
    concerns = []
    score = 4  # Start with max
    
    if twi.pooling_risk in ["HIGH", "SEVERE"]:
        concerns.append(f"Waterlogging risk: {twi.pooling_risk}")
        score -= 1
    
    if water_balance.irrigation_needed and water_balance.water_balance_mm < -200:
        concerns.append(f"Water deficit: {water_balance.water_balance_mm:.0f}mm")
        score -= 1
    
    if soil_moisture.moisture_status in ["DRY", "SATURATED"]:
        concerns.append(f"Soil moisture: {soil_moisture.moisture_status}")
        score -= 0.5
    
    if heat_stress.risk_level in ["HIGH", "SEVERE"]:
        concerns.append(f"Heat stress: {heat_stress.risk_level}")
        score -= 0.5
    
    if weather.precipitation_total_mm < 800:
        concerns.append(f"Low rainfall: {weather.precipitation_total_mm:.0f}mm/year")
        score -= 0.5
    
    if weather.temp_max > 35:
        concerns.append(f"High temps: {weather.temp_max:.0f}°C max")
        score -= 0.5
    
    if soil.ph < 5.0 or soil.ph > 7.5:
        concerns.append(f"Soil pH: {soil.ph}")
        score -= 0.5
    
    if soil.drainage_class == "Poor":
        concerns.append("Poor drainage")
        score -= 0.5
    
    # Determine suitability
    if score >= 3.5:
        suitability = "EXCELLENT"
    elif score >= 2.5:
        suitability = "GOOD"
    elif score >= 1.5:
        suitability = "MODERATE"
    else:
        suitability = "POOR"
    
    if not concerns:
        concerns.append("No significant concerns")
    
    return LandDataResponse(
        # Climate
        temp_mean_c=weather.temp_mean,
        temp_min_c=weather.temp_min,
        temp_max_c=weather.temp_max,
        annual_rainfall_mm=weather.precipitation_total_mm,
        water_balance_mm=water_balance.water_balance_mm,
        sunshine_hours_daily=weather.sunshine_hours_daily_avg,
        humidity_percent=weather.humidity_mean,
        # Terrain
        elevation_m=round(float(elevation), 1),
        slope_degrees=round(float(slope), 2),
        aspect=aspect,
        # Soil
        soil_ph=soil.ph,
        soil_type=soil.soil_type,
        soil_drainage=soil.drainage_class,
        soil_moisture_status=soil_moisture.moisture_status,
        # Risks
        pooling_risk=twi.pooling_risk,
        heat_stress_risk=heat_stress.risk_level,
        irrigation_needed=water_balance.irrigation_needed,
        # Summary
        suitability=suitability,
        concerns=concerns,
    )


# =============================================================================
# Satellite Embeddings (AlphaEarth Foundations)
# =============================================================================

EMBEDDING_COLLECTION = "GOOGLE/SATELLITE_EMBEDDING/V1/ANNUAL"
EMBEDDING_BANDS = [f"A{i:02d}" for i in range(64)]  # A00, A01, ..., A63


def get_embedding_vector(lat: float, lon: float, year: int, buffer_meters: int = 1000) -> Optional[list[float]]:
    """
    Fetch the mean 64D embedding vector for a location and year.
    Returns None if no data available.
    """
    point = ee.Geometry.Point([lon, lat])
    region = point.buffer(buffer_meters)
    
    # Filter to the specific year
    start_date = f"{year}-01-01"
    end_date = f"{year + 1}-01-01"
    
    embeddings = (
        ee.ImageCollection(EMBEDDING_COLLECTION)
        .filterDate(start_date, end_date)
        .filterBounds(point)
    )
    
    count = embeddings.size().getInfo()
    if count == 0:
        return None
    
    # Mosaic tiles and get mean embedding for region
    image = embeddings.mosaic()
    
    values = image.reduceRegion(
        reducer=ee.Reducer.mean(),
        geometry=region,
        scale=10,
        maxPixels=1e8,
    ).getInfo()
    
    # Extract as vector
    vector = [values.get(band, 0.0) for band in EMBEDDING_BANDS]
    
    # Check if we got valid data
    if all(v == 0 or v is None for v in vector):
        return None
    
    return vector


def cosine_similarity(v1: list[float], v2: list[float]) -> float:
    """Compute cosine similarity between two vectors."""
    a = np.array(v1)
    b = np.array(v2)
    
    # Embeddings are already unit-length, so dot product = cosine similarity
    return float(np.dot(a, b))


def interpret_stability(similarity: float) -> str:
    """Interpret similarity score as change magnitude."""
    if similarity >= 0.95:
        return "minimal"
    elif similarity >= 0.90:
        return "slight"
    elif similarity >= 0.80:
        return "moderate"
    else:
        return "significant"


def interpret_overall_stability(score: float) -> str:
    """Interpret overall stability score."""
    if score >= 0.95:
        return "highly_stable"
    elif score >= 0.90:
        return "stable"
    elif score >= 0.80:
        return "moderate_change"
    else:
        return "significant_change"


@app.post("/embeddings/stability", response_model=EmbeddingStabilityResponse)
def get_embedding_stability(req: EmbeddingStabilityRequest):
    """
    Analyze land stability using satellite embeddings across multiple years.
    Computes year-over-year similarity using dot product of 64D embedding vectors.
    
    Higher stability score = land has been consistent (good for agriculture).
    Lower score = significant changes detected (could be development, clearing, etc).
    """
    # Fetch embeddings for available years (2022-2024)
    years = [2022, 2023, 2024]
    embeddings = {}
    
    for year in years:
        try:
            vector = get_embedding_vector(
                req.latitude, req.longitude, year, req.buffer_meters
            )
            if vector:
                embeddings[str(year)] = vector
        except Exception as e:
            print(f"Warning: Could not fetch embeddings for {year}: {e}")
    
    if len(embeddings) < 2:
        raise HTTPException(
            status_code=404,
            detail=f"Insufficient embedding data. Found {len(embeddings)} years, need at least 2."
        )
    
    # Compute year-over-year similarities
    years_found = sorted(embeddings.keys())
    comparisons = {}
    similarities = []
    
    for i in range(len(years_found) - 1):
        year1, year2 = years_found[i], years_found[i + 1]
        sim = cosine_similarity(embeddings[year1], embeddings[year2])
        similarities.append(sim)
        
        comparisons[f"{year1}_to_{year2}"] = {
            "similarity": round(sim, 4),
            "change": interpret_stability(sim),
        }
    
    # Overall stability is the average similarity
    overall_stability = np.mean(similarities)
    
    return EmbeddingStabilityResponse(
        stability_score=round(float(overall_stability), 4),
        interpretation=interpret_overall_stability(overall_stability),
        year_comparisons=comparisons,
        years_analyzed=years_found,
    )


@app.post("/embeddings/zones", response_model=EmbeddingZonesResponse)
def get_embedding_zones(req: EmbeddingZonesRequest):
    """
    Segment land into zones using unsupervised clustering on satellite embeddings.
    Uses K-means clustering in 64D embedding space to identify distinct land areas.
    
    Each zone represents pixels with similar temporal and spectral characteristics.
    """
    point = ee.Geometry.Point([req.longitude, req.latitude])
    region = point.buffer(req.buffer_meters)
    
    # Get embeddings for the specified year
    start_date = f"{req.year}-01-01"
    end_date = f"{req.year + 1}-01-01"
    
    embeddings_collection = (
        ee.ImageCollection(EMBEDDING_COLLECTION)
        .filterDate(start_date, end_date)
        .filterBounds(point)
    )
    
    count = embeddings_collection.size().getInfo()
    if count == 0:
        raise HTTPException(
            status_code=404,
            detail=f"No embedding data available for year {req.year}"
        )
    
    embeddings_image = embeddings_collection.mosaic()
    
    # Sample pixels for training the clusterer
    training = embeddings_image.sample(
        region=region,
        scale=10,
        numPixels=1000,
        seed=42,
    )
    
    training_count = training.size().getInfo()
    if training_count < req.num_zones:
        raise HTTPException(
            status_code=400,
            detail=f"Not enough pixels ({training_count}) for {req.num_zones} zones"
        )
    
    # Train K-means clusterer
    clusterer = ee.Clusterer.wekaKMeans(nClusters=req.num_zones).train(training)
    
    # Cluster the image
    clustered = embeddings_image.cluster(clusterer)
    
    # Get zone histogram (count pixels in each cluster)
    histogram = clustered.reduceRegion(
        reducer=ee.Reducer.frequencyHistogram(),
        geometry=region,
        scale=10,
        maxPixels=1e8,
    ).get("cluster").getInfo()
    
    # Calculate percentages and interpret zones
    total_pixels = int(sum(histogram.values()))
    zones = []
    
    # Sort by percentage (largest first)
    sorted_clusters = sorted(
        histogram.items(),
        key=lambda x: x[1],
        reverse=True
    )
    
    # Zone interpretations based on typical patterns
    zone_labels = [
        "primary_vegetation",
        "secondary_vegetation",
        "transitional_area",
        "sparse_cover",
        "distinct_feature",
    ]
    
    for i, (cluster_id, count) in enumerate(sorted_clusters):
        pct = (count / total_pixels) * 100
        zones.append({
            "zone_id": int(cluster_id),
            "percentage": round(pct, 2),
            "pixel_count": count,
            "interpretation": zone_labels[i] if i < len(zone_labels) else f"zone_{i+1}",
        })
    
    return EmbeddingZonesResponse(
        zones=zones,
        total_pixels=total_pixels,
        year=req.year,
    )


# =============================================================================
# Geospatial-RAG Endpoints (AlphaEarth + pgvector)
# =============================================================================

@app.post("/embeddings/fetch", response_model=EmbeddingFetchResponse)
def fetch_embedding(req: EmbeddingFetchRequest):
    """
    Fetch the raw 64D AlphaEarth embedding for any location.
    This is the building block for Geospatial-RAG similarity search.
    
    The embedding captures multi-sensor characteristics:
    - Optical reflectance patterns
    - Radar backscatter (SAR)
    - Temporal patterns throughout the year
    - Vegetation indices and moisture
    """
    embedding = get_embedding_vector(
        req.latitude, req.longitude, req.year, req.buffer_meters
    )
    
    if embedding is None:
        raise HTTPException(
            status_code=404,
            detail=f"No AlphaEarth embedding data available for this location in {req.year}"
        )
    
    return EmbeddingFetchResponse(
        embedding=embedding,
        location={
            "latitude": req.latitude,
            "longitude": req.longitude,
            "buffer_meters": req.buffer_meters,
        },
        year=req.year,
    )


@app.post("/embeddings/similar-farms", response_model=SimilarFarmsResponse)
def find_similar_farms(req: SimilarFarmsRequest):
    """
    🌍 GEOSPATIAL-RAG: Find documented successful farms with similar satellite signatures.
    
    This endpoint:
    1. Fetches the AlphaEarth 64D embedding for the query location
    2. Performs cosine similarity search in the reference farms database
    3. Returns farms with proven yields that have similar multi-sensor characteristics
    
    The similarity score represents how closely the query plot's satellite signature
    matches documented successful farms. High similarity = high confidence in yield prediction.
    """
    # Step 1: Fetch embedding for the query location
    query_embedding = get_embedding_vector(
        req.latitude, req.longitude, req.year, 1000
    )
    
    if query_embedding is None:
        raise HTTPException(
            status_code=404,
            detail=f"Could not fetch AlphaEarth embedding for this location in {req.year}"
        )
    
    # Step 2: Vector similarity search in pgvector
    try:
        conn = get_db()
        cur = conn.cursor(cursor_factory=RealDictCursor)
        
        # Query for similar farms using cosine similarity
        cur.execute("""
            SELECT 
                name, region, country,
                1 - (embedding <=> %s::vector) as similarity,
                yield_min, yield_max, yield_typical,
                elevation_m, annual_rainfall_mm, avg_temp_c,
                data_source, COALESCE(seasons_per_year, 1) as seasons_per_year
            FROM reference_farms
            WHERE crop = %s
            AND embedding IS NOT NULL
            ORDER BY embedding <=> %s::vector
            LIMIT %s
        """, (query_embedding, req.crop.lower(), query_embedding, req.limit))
        
        results = cur.fetchall()
        cur.close()
        
    except Exception as e:
        raise HTTPException(
            status_code=500,
            detail=f"Database error during similarity search: {str(e)}"
        )
        
    if not results:
        raise HTTPException(
            status_code=404,
            detail=f"No reference farms found for crop '{req.crop}'. Available crops: avocado, coffee, banana, cacao, mango"
        )
    
    # Step 3: Format results with interpretations
    similar_farms = []
    for r in results:
        similarity = float(r["similarity"])
        
        # Interpret similarity score
        if similarity >= 0.90:
            interpretation = "EXCELLENT match - very similar satellite signature to this successful farm"
        elif similarity >= 0.80:
            interpretation = "GOOD match - similar growing conditions detected"
        elif similarity >= 0.70:
            interpretation = "MODERATE match - some similarities in land characteristics"
        else:
            interpretation = "LOW match - different land characteristics"
        
        similar_farms.append(SimilarFarmMatch(
            name=r["name"],
            region=r["region"],
            country=r["country"],
            similarity=round(similarity, 3),
            yield_min=r["yield_min"],
            yield_max=r["yield_max"],
            yield_typical=r["yield_typical"],
            elevation_m=r["elevation_m"],
            data_source=r["data_source"],
            seasons_per_year=r["seasons_per_year"],
            interpretation=interpretation,
        ))
    
    # Step 4: Calculate weighted yield prediction
    total_weight = sum(f.similarity for f in similar_farms)
    if total_weight > 0:
        weighted_yield = sum(f.yield_typical * f.similarity for f in similar_farms) / total_weight
        yield_min = min(f.yield_min for f in similar_farms)
        yield_max = max(f.yield_max for f in similar_farms)
    else:
        weighted_yield = similar_farms[0].yield_typical if similar_farms else 0
        yield_min = similar_farms[0].yield_min if similar_farms else 0
        yield_max = similar_farms[0].yield_max if similar_farms else 0
    
    # Determine confidence based on best match similarity
    best_similarity = similar_farms[0].similarity if similar_farms else 0
    if best_similarity >= 0.90:
        confidence = "HIGH"
        confidence_msg = "Satellite signature closely matches proven successful farms"
    elif best_similarity >= 0.80:
        confidence = "MEDIUM"
        confidence_msg = "Good similarity to reference farms, reasonable yield expectation"
    elif best_similarity >= 0.70:
        confidence = "LOW"
        confidence_msg = "Moderate similarity - yields may vary from prediction"
    else:
        confidence = "VERY LOW"
        confidence_msg = "Low similarity to reference farms - novel conditions"
    
    # Build message
    top_match = similar_farms[0] if similar_farms else None
    if top_match:
        message = (
            f"Your plot's satellite signature is {top_match.similarity*100:.0f}% similar to "
            f"{top_match.name} ({top_match.region}, {top_match.country}), which produces "
            f"{top_match.yield_typical} t/ha. Based on {len(similar_farms)} reference farms, "
            f"expected yield: {weighted_yield:.1f} t/ha ({yield_min:.1f}-{yield_max:.1f} range)."
        )
    else:
        message = "No similar farms found for comparison."
    
    return SimilarFarmsResponse(
        query_location={
                "latitude": req.latitude,
                "longitude": req.longitude,
            "year": req.year,
        },
        crop=req.crop,
        similar_farms=similar_farms,
        yield_prediction={
            "min": round(yield_min, 1),
            "max": round(yield_max, 1),
            "expected": round(weighted_yield, 1),
            "unit": "tons/hectare",
            },
        confidence=confidence,
        message=message,
    )


# =============================================================================
# Legacy Endpoints (DEPRECATED - Use /embeddings/similar-farms instead)
# These endpoints use climate-based matching, not AlphaEarth embeddings.
# Kept for backwards compatibility but will be removed in future versions.
# =============================================================================

# NOTE: Old CROP_REFERENCE_REGIONS and MapSPAM endpoints removed.
# The new Geospatial-RAG approach uses:
#   - /embeddings/similar-farms for embedding-based similarity search
#   - Reference farms with real AlphaEarth 64D embeddings
#   - pgvector for efficient cosine similarity search


# =============================================================================
# COG Tile Endpoint — 10m refinement via Earth Engine computePixels
# =============================================================================

EMBEDDING_BANDS = [f"A{i:02d}" for i in range(64)]
TILE_SIZE = 256

# For 10m COG tiles, EE returns float32 embeddings (range ~-0.4 to 0.4).
# Dot product of dequantized ref (magnitude ~1.0) vs EE float32 (magnitude ~0.87).
# Self-similarity is ~0.75, strong matches ~0.80-0.85, weak ~0.3-0.5.
# Normalizing by 0.9 maps the best matches to ~0.93 and weak to ~0.33-0.55.
COG_EMPIRICAL_MAX = 0.9

# Load grid.bin scale/offset for dequantizing int8 reference embeddings to float32.
_GRID_SCALES = None
_GRID_OFFSETS = None

def _load_grid_scales():
    """Load per-band scale and offset from grid.bin header for dequantization."""
    global _GRID_SCALES, _GRID_OFFSETS
    if _GRID_SCALES is not None:
        return
    import struct
    grid_path = os.environ.get("GRID_PATH", os.path.join(os.path.dirname(__file__), "..", "data", "grid.bin"))
    try:
        with open(grid_path, "rb") as f:
            f.read(8 + 4 + 4 + 4 + 4 + 32)  # magic, version, bands, width, height, bbox
            _GRID_SCALES = np.frombuffer(f.read(256), dtype=np.float32).copy()
            _GRID_OFFSETS = np.frombuffer(f.read(256), dtype=np.float32).copy()
        logger.info(f"Loaded grid scales/offsets from {grid_path}")
    except Exception as e:
        logger.warning(f"Could not load grid scales from {grid_path}: {e}")
        # Fallback: identity transform (int8 values used as-is)
        _GRID_SCALES = np.ones(64, dtype=np.float32)
        _GRID_OFFSETS = np.zeros(64, dtype=np.float32)


class COGTileRequest(BaseModel):
    z: int
    x: int
    y: int
    year: int = 2025
    ref_embeddings: list[list[int]]  # N arrays of 64 int8 values


def tile_bounds(z: int, x: int, y: int) -> tuple[float, float, float, float]:
    """Convert web mercator tile z/x/y to geographic bounds (west, south, east, north)."""
    import math
    n = 2.0 ** z
    west = x / n * 360.0 - 180.0
    east = (x + 1) / n * 360.0 - 180.0
    north = math.degrees(math.atan(math.sinh(math.pi * (1 - 2 * y / n))))
    south = math.degrees(math.atan(math.sinh(math.pi * (1 - 2 * (y + 1) / n))))
    return west, south, east, north


@app.post("/fetch-cog-tile")
async def fetch_cog_tile(request: COGTileRequest):
    """
    Fetch 10m AlphaEarth embeddings for a map tile and compute similarity.
    Returns 256x256 float32 scores as raw binary, or 204 if no data.
    """
    from fastapi.responses import Response

    west, south, east, north = tile_bounds(request.z, request.x, request.y)

    try:
        # Build the embedding image for the requested year
        embedding_img = (
            ee.ImageCollection("GOOGLE/SATELLITE_EMBEDDING/V1/ANNUAL")
            .filterDate(f"{request.year}-01-01", f"{request.year + 1}-01-01")
            .filterBounds(ee.Geometry.Rectangle([west, south, east, north]))
            .mosaic()
            .select(EMBEDDING_BANDS)
        )

        # Use computePixels to get exactly 256x256 pixels
        pixel_grid = {
            "dimensions": {"width": TILE_SIZE, "height": TILE_SIZE},
            "affineTransform": {
                "scaleX": (east - west) / TILE_SIZE,
                "shearX": 0,
                "translateX": west,
                "scaleY": -(north - south) / TILE_SIZE,
                "shearY": 0,
                "translateY": north,
            },
            "crsCode": "EPSG:4326",
        }

        result = ee.data.computePixels({
            "expression": embedding_img,
            "fileFormat": "NUMPY_NDARRAY",
            "grid": pixel_grid,
        })

        # result is a structured numpy array with fields A00..A63
        # Convert to (256, 256, 64) int array
        pixels = np.stack([result[band] for band in EMBEDDING_BANDS], axis=-1)

        # Check if tile is all nodata (ocean)
        if np.all(pixels == 0):
            return Response(status_code=204)

        # Dequantize int8 reference embeddings to float32 (matching EE scale).
        _load_grid_scales()
        refs_int8 = np.array(request.ref_embeddings, dtype=np.float32)  # (N, 64)
        refs_float = refs_int8 * _GRID_SCALES + _GRID_OFFSETS  # (N, 64) in EE float32 space

        # Compute MAX dot-product similarity (both sides now float32).
        pixels_flat = pixels.reshape(-1, 64).astype(np.float32)  # (65536, 64)

        dots = pixels_flat @ refs_float.T    # (65536, N)
        max_dots = dots.max(axis=1)          # (65536,)

        scores = np.clip(max_dots / COG_EMPIRICAL_MAX, 0.0, 1.0).astype(np.float32)

        # Debug: log score distribution for tuning
        land_scores = scores[scores > 0]
        if len(land_scores) > 0:
            logger.info(
                f"COG tile z={request.z} x={request.x} y={request.y}: "
                f"dots min={max_dots.min():.4f} max={max_dots.max():.4f} "
                f"scores min={land_scores.min():.3f} p50={np.median(land_scores):.3f} "
                f"p90={np.percentile(land_scores, 90):.3f} max={land_scores.max():.3f} "
                f"land_pct={len(land_scores)/len(scores)*100:.0f}%"
            )

        return Response(
            content=scores.tobytes(),
            media_type="application/octet-stream",
        )

    except Exception as e:
        logger.warning(f"COG tile fetch failed z={request.z} x={request.x} y={request.y}: {e}")
        return Response(status_code=204)


# =============================================================================
# Main
# =============================================================================

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8001)

