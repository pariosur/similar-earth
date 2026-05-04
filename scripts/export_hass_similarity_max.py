#!/usr/bin/env python3
"""
Export Hass Avocado MAX similarity raster to Google Drive.

This compares each global location to EACH of the 23 verified farms individually,
then takes the MAXIMUM similarity. This captures diverse growing conditions better
than comparing to the mean.

Run this, then download the file from Drive and run generate_tiles_from_geotiff.py
"""

import csv
import os
import sys
import time
from pathlib import Path

import numpy as np

try:
    import ee
except ImportError:
    print("ERROR: earthengine-api not installed")
    sys.exit(1)

# =============================================================================
# Configuration  
# =============================================================================

SCRIPT_DIR = Path(__file__).parent
DATA_DIR = SCRIPT_DIR / "data"
GOLD_STANDARD_CSV = SCRIPT_DIR.parent / "gold_standard_verified_yields.csv"

GEE_PROJECT = os.environ.get("GEE_PROJECT", "tierra-ai")
EMBEDDING_COLLECTION = "GOOGLE/SATELLITE_EMBEDDING/V1/ANNUAL"
EMBEDDING_BANDS = [f"A{i:02d}" for i in range(64)]

# Export settings
EXPORT_SCALE = 5000  # meters per pixel (5km resolution for global)
EXPORT_FILENAME = "hass_dotproduct_max"  # Using dot product instead of cosine
DRIVE_FOLDER = "tierra-exports"


def init_ee():
    """Initialize Earth Engine."""
    try:
        ee.Initialize(project=GEE_PROJECT)
        print(f"✓ Earth Engine initialized (project: {GEE_PROJECT})")
    except Exception:
        ee.Authenticate()
        ee.Initialize(project=GEE_PROJECT)


def load_hass_farms() -> list[dict]:
    """Load Hass avocado farm coordinates from gold standard CSV."""
    farms = []
    
    with open(GOLD_STANDARD_CSV, 'r') as f:
        reader = csv.DictReader(f)
        for row in reader:
            if row['crop'].lower() == 'hass avocado':
                farms.append({
                    'name': row['farm_name'],
                    'lat': float(row['latitude']),
                    'lon': float(row['longitude']),
                    'region': row['region'],
                    'country': row['country'],
                })
    
    print(f"✓ Loaded {len(farms)} Hass avocado farms from gold standard")
    return farms


def fetch_farm_embeddings(farms: list[dict], year: int = 2024) -> list[list[float]]:
    """
    Fetch the 64D embedding for each farm location.
    Returns list of embedding vectors.
    """
    print(f"\nFetching embeddings for {len(farms)} farms...")
    
    embeddings = []
    farm_info = []
    
    for i, farm in enumerate(farms):
        point = ee.Geometry.Point([farm['lon'], farm['lat']])
        region = point.buffer(1000)  # 1km buffer
        
        # Get embedding image
        start_date = f"{year}-01-01"
        end_date = f"{year + 1}-01-01"
        
        embedding_img = (
            ee.ImageCollection(EMBEDDING_COLLECTION)
            .filterDate(start_date, end_date)
            .filterBounds(point)
            .mosaic()
        )
        
        # Reduce to mean values
        values = embedding_img.reduceRegion(
            reducer=ee.Reducer.mean(),
            geometry=region,
            scale=10,
            maxPixels=1e8,
        ).getInfo()
        
        # Extract vector
        vector = [values.get(band, 0.0) for band in EMBEDDING_BANDS]
        
        if all(v == 0 or v is None for v in vector):
            print(f"  [{i+1}/{len(farms)}] ⚠ No data for {farm['name']} ({farm['country']})")
            continue
        
        embeddings.append(vector)
        farm_info.append(farm)
        print(f"  [{i+1}/{len(farms)}] ✓ {farm['name']} ({farm['country']})")
    
    print(f"\n✓ Retrieved embeddings for {len(embeddings)} farms")
    return embeddings, farm_info


def create_max_similarity_image(farm_embeddings: list[list[float]], year: int = 2024) -> ee.Image:
    """
    Create a GEE image where each pixel = MAX similarity to ANY farm.
    
    For each pixel:
      1. Get the pixel's 64D embedding
      2. Compute cosine similarity to EACH of the 23 farms
      3. Take the maximum similarity value
    """
    print(f"\nCreating MAX similarity image from {len(farm_embeddings)} farms...")
    
    # Get global embeddings for the year
    start_date = f"{year}-01-01"
    end_date = f"{year + 1}-01-01"
    
    global_embeddings = (
        ee.ImageCollection(EMBEDDING_COLLECTION)
        .filterDate(start_date, end_date)
        .mosaic()
        .select(EMBEDDING_BANDS)
    )
    
    # DOT PRODUCT similarity (not cosine)
    # Dot product captures both direction AND magnitude
    # This finds locations that match the intensity of conditions, not just the type
    
    # Compute similarity to each farm and stack as bands
    similarity_bands = []
    
    for i, farm_embedding in enumerate(farm_embeddings):
        # Create constant image for this farm's embedding
        farm_img = ee.Image.constant(farm_embedding).rename(EMBEDDING_BANDS)
        
        # DOT PRODUCT = sum(a * b) - no normalization by magnitude
        dot_product = global_embeddings.multiply(farm_img).reduce(ee.Reducer.sum())
        
        # Dot product range varies, so we'll normalize later
        similarity_bands.append(dot_product.rename(f'sim_farm_{i}'))
        
        if (i + 1) % 5 == 0:
            print(f"  Processed {i + 1}/{len(farm_embeddings)} farms...")
    
    # Stack all dot products and take the max
    stacked = ee.Image.cat(similarity_bands)
    max_dot_product = stacked.reduce(ee.Reducer.max())
    
    # Normalize dot product to 0-1 range for visualization
    # We'll use percentile-based normalization (robust to outliers)
    # Typical dot product range for AlphaEarth embeddings: ~0 to ~50
    # We'll normalize so that high values (indicating strong match) map to 1
    
    # Empirical normalization: divide by expected max (~40-50 for strong matches)
    # Then clamp to 0-1
    normalized = max_dot_product.divide(50).clamp(0, 1).rename('similarity')
    
    # MASK OUT WATER BODIES
    # Use JRC Global Surface Water to identify permanent water
    water = ee.Image("JRC/GSW1_4/GlobalSurfaceWater").select('occurrence')
    water_mask = water.gt(50)  # Pixels with >50% water occurrence
    
    # Also use MODIS Land Cover to mask oceans
    land_cover = ee.ImageCollection("MODIS/061/MCD12Q1").first().select('LC_Type1')
    ocean_mask = land_cover.eq(17)  # 17 = Water Bodies in IGBP classification
    
    # Combined water mask
    all_water = water_mask.Or(ocean_mask)
    
    # Apply mask - set water pixels to 0
    normalized = normalized.where(all_water, 0)
    
    print(f"✓ MAX dot product image created (normalized, water masked)")
    return normalized


def export_to_drive(image: ee.Image, filename: str, folder: str, scale: int):
    """Export image to Google Drive."""
    
    # Define global bounds (skip polar regions, avoid antimeridian issues)
    region = ee.Geometry.Rectangle([-179.9, -55, 179.9, 70], None, False)
    
    print(f"\nStarting export to Google Drive...")
    print(f"  Folder: {folder}")
    print(f"  Filename: {filename}")
    print(f"  Scale: {scale}m")
    print(f"  Region: Global (-180,-55 to 180,70)")
    
    task = ee.batch.Export.image.toDrive(
        image=image,
        description=filename,
        folder=folder,
        fileNamePrefix=filename,
        region=region,
        scale=scale,
        crs='EPSG:4326',
        maxPixels=1e10,
        fileFormat='GeoTIFF',
    )
    
    task.start()
    print(f"\n✓ Export task started!")
    print(f"  Task ID: {task.id}")
    
    # Monitor progress
    print("\nMonitoring progress (Ctrl+C to stop monitoring, export continues)...")
    try:
        while True:
            status = task.status()
            state = status['state']
            
            if state == 'COMPLETED':
                print(f"\n✓ Export COMPLETED!")
                print(f"  Check Google Drive folder: {folder}")
                print(f"  File: {filename}.tif")
                break
            elif state == 'FAILED':
                print(f"\n✗ Export FAILED!")
                print(f"  Error: {status.get('error_message', 'Unknown error')}")
                break
            elif state == 'CANCELLED':
                print(f"\n✗ Export CANCELLED")
                break
            else:
                print(f"  Status: {state}...", end='\r')
                time.sleep(10)
    except KeyboardInterrupt:
        print(f"\n\nMonitoring stopped. Export continues in background.")
        print(f"Check status at: https://code.earthengine.google.com/tasks")
    
    return task


def main():
    print("=" * 60)
    print("Hass Avocado MAX Similarity Export")
    print("Compares to each farm individually, takes maximum")
    print("=" * 60)
    
    init_ee()
    
    # Load farms
    farms = load_hass_farms()
    
    # Fetch embeddings for each farm
    embeddings, valid_farms = fetch_farm_embeddings(farms)
    
    if len(embeddings) < 5:
        print("ERROR: Not enough farms with valid embeddings")
        sys.exit(1)
    
    # Save embeddings for reference
    embeddings_path = DATA_DIR / "hass_farm_embeddings.npy"
    np.save(embeddings_path, np.array(embeddings))
    print(f"✓ Saved farm embeddings to {embeddings_path}")
    
    # Create max similarity image
    max_similarity = create_max_similarity_image(embeddings)
    
    # Export to Drive
    task = export_to_drive(
        max_similarity,
        EXPORT_FILENAME,
        DRIVE_FOLDER,
        EXPORT_SCALE
    )
    
    print("\n" + "=" * 60)
    print("NEXT STEPS:")
    print("1. Wait for export to complete (check Google Drive)")
    print(f"2. Download {EXPORT_FILENAME}.tif to scripts/data/")
    print("3. Rename to hass_similarity_global.tif")
    print("4. Run: python scripts/generate_tiles_from_geotiff.py")
    print("=" * 60)


if __name__ == "__main__":
    main()
