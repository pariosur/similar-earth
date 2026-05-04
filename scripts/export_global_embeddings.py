#!/usr/bin/env python3
"""
Export AlphaEarth raw 64-band embeddings at 2km resolution globally.

This exports the RAW embedding vectors (bands A00-A63) from Google's
SATELLITE_EMBEDDING/V1/ANNUAL collection, NOT pre-computed similarity scores.
Each pixel in the output GeoTIFF contains a 64-dimensional float32 vector
representing the satellite-derived environmental signature of that location.

Water/ocean pixels are masked using MODIS Land Cover (class 17) and
JRC Global Surface Water (occurrence > 50%), with masked pixels set to 0.

Output:
    Google Drive: tierra-exports/alphaearth_embeddings_2km_2024.tif
    - 64-band GeoTIFF, float32, LZW compressed
    - ~2km resolution (scale=2000m), EPSG:4326
    - Global land coverage: -179.9,-55 to 179.9,70

Next step:
    Download the .tif from Google Drive, place it in scripts/data/, then run:
        python scripts/build_grid_bin.py
    to quantize the embeddings into the compact grid.bin format used by the app.
"""

import os
import sys
import time

try:
    import ee
except ImportError:
    print("ERROR: earthengine-api not installed. Run: pip install earthengine-api")
    sys.exit(1)

# =============================================================================
# Configuration
# =============================================================================

GEE_PROJECT = os.environ.get("GEE_PROJECT", "tierra-ai")
EMBEDDING_COLLECTION = "GOOGLE/SATELLITE_EMBEDDING/V1/ANNUAL"
EMBEDDING_BANDS = [f"A{i:02d}" for i in range(64)]

YEAR = 2025
EXPORT_SCALE = 2000  # meters per pixel (2km resolution)
EXPORT_FILENAME = "alphaearth_embeddings_2km_2025"
DRIVE_FOLDER = "tierra-exports"
MAX_PIXELS = 1e11  # Large export — 64 bands at 2km global


def init_ee():
    """Initialize Earth Engine."""
    try:
        ee.Initialize(project=GEE_PROJECT)
        print(f"Earth Engine initialized (project: {GEE_PROJECT})")
    except Exception:
        ee.Authenticate()
        ee.Initialize(project=GEE_PROJECT)


def create_water_mask() -> ee.Image:
    """
    Create a combined water mask from MODIS Land Cover and JRC Surface Water.
    Returns an image where 1 = water, 0 = land.
    """
    # MODIS Land Cover: class 17 = Water Bodies (IGBP classification)
    land_cover = ee.ImageCollection("MODIS/061/MCD12Q1").first().select("LC_Type1")
    modis_water = land_cover.eq(17)

    # JRC Global Surface Water: pixels with >50% water occurrence
    jrc_water = ee.Image("JRC/GSW1_4/GlobalSurfaceWater").select("occurrence")
    jrc_water_mask = jrc_water.gt(50)

    # Combined: water if either source says water
    return modis_water.Or(jrc_water_mask)


def create_embedding_image() -> ee.Image:
    """
    Create the global 64-band embedding image for the target year.
    Water pixels are set to 0 (nodata) across all bands.
    """
    start_date = f"{YEAR}-01-01"
    end_date = f"{YEAR + 1}-01-01"

    print(f"Loading embeddings for {YEAR}...")
    embeddings = (
        ee.ImageCollection(EMBEDDING_COLLECTION)
        .filterDate(start_date, end_date)
        .mosaic()
        .select(EMBEDDING_BANDS)
    )

    # Build water mask
    print("Building water mask (MODIS LC + JRC Surface Water)...")
    water_mask = create_water_mask()

    # Set water pixels to 0 across all 64 bands
    # unmask(0) ensures pixels outside the embedding footprint are also 0
    masked = embeddings.unmask(0).where(water_mask, 0)

    print(f"Embedding image ready: {len(EMBEDDING_BANDS)} bands, water masked to 0")
    return masked.toFloat()


def export_to_drive(image: ee.Image):
    """Export the 64-band embedding image to Google Drive."""

    region = ee.Geometry.Rectangle([-179.9, -55, 179.9, 70], None, False)

    print(f"\nStarting export to Google Drive...")
    print(f"  Folder:    {DRIVE_FOLDER}")
    print(f"  Filename:  {EXPORT_FILENAME}")
    print(f"  Scale:     {EXPORT_SCALE}m ({EXPORT_SCALE / 1000}km)")
    print(f"  Bands:     {len(EMBEDDING_BANDS)} (A00-A63)")
    print(f"  CRS:       EPSG:4326")
    print(f"  Region:    Global land (-179.9,-55 to 179.9,70)")
    print(f"  MaxPixels: {MAX_PIXELS:.0e}")

    task = ee.batch.Export.image.toDrive(
        image=image,
        description=EXPORT_FILENAME,
        folder=DRIVE_FOLDER,
        fileNamePrefix=EXPORT_FILENAME,
        region=region,
        scale=EXPORT_SCALE,
        crs="EPSG:4326",
        maxPixels=MAX_PIXELS,
        fileFormat="GeoTIFF",
        formatOptions={"cloudOptimized": False, "noData": 0},
    )

    task.start()
    print(f"\nExport task started!")
    print(f"  Task ID: {task.id}")

    # Monitor progress
    print("\nMonitoring progress (Ctrl+C to stop monitoring, export continues)...")
    start_time = time.time()
    try:
        while True:
            status = task.status()
            state = status["state"]
            elapsed = time.time() - start_time
            elapsed_str = time.strftime("%H:%M:%S", time.gmtime(elapsed))

            if state == "COMPLETED":
                print(f"\nExport COMPLETED in {elapsed_str}")
                print(f"  Check Google Drive folder: {DRIVE_FOLDER}")
                print(f"  File: {EXPORT_FILENAME}.tif")
                break
            elif state == "FAILED":
                print(f"\nExport FAILED after {elapsed_str}")
                print(f"  Error: {status.get('error_message', 'Unknown error')}")
                sys.exit(1)
            elif state == "CANCELLED":
                print(f"\nExport CANCELLED after {elapsed_str}")
                sys.exit(1)
            else:
                print(f"  [{elapsed_str}] Status: {state}...", end="\r")
                time.sleep(15)
    except KeyboardInterrupt:
        print(f"\n\nMonitoring stopped after {elapsed_str}. Export continues in background.")
        print(f"Check status at: https://code.earthengine.google.com/tasks")

    return task


def main():
    print("=" * 60)
    print("AlphaEarth Global Embeddings Export")
    print(f"64 raw bands, {EXPORT_SCALE / 1000}km resolution, year {YEAR}")
    print("=" * 60)

    init_ee()

    image = create_embedding_image()

    export_to_drive(image)

    print("\n" + "=" * 60)
    print("NEXT STEPS:")
    print(f"1. Wait for export to complete (check Google Drive)")
    print(f"2. Download {EXPORT_FILENAME}.tif to scripts/data/")
    print(f"3. Run: python scripts/build_grid_bin.py")
    print("=" * 60)


if __name__ == "__main__":
    main()
