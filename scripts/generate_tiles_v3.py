#!/usr/bin/env python3
"""
Generate tiles by sampling directly from lat/lon source to Web Mercator tiles.
This avoids intermediate reprojection artifacts.
"""

import sys
from pathlib import Path
from math import pi, log, tan, cos, atan, sinh, exp

import numpy as np

try:
    import rasterio
    from PIL import Image
except ImportError:
    print("Run: pip install rasterio pillow numpy")
    sys.exit(1)

# =============================================================================
# Configuration
# =============================================================================

SCRIPT_DIR = Path(__file__).parent
INPUT_FILE = SCRIPT_DIR / "data" / "hass_similarity_global.tif"
OUTPUT_DIR = SCRIPT_DIR.parent / "frontend" / "public" / "tiles" / "hass-avocado"

# Monochrome magenta palette
# STRICT thresholds - only show top 10% of land to reduce noise
# Real farms score 0.017-0.019, noise like Berlin scores 0.009
COLOR_PALETTE = [
    (0.016, 134, 25, 143),   # Dark Magenta - top 3% (excellent match)
    (0.015, 192, 38, 211),   # Bright Magenta - top 5% (very good)
    (0.013, 232, 121, 249),  # Light Pink - top 10% (good match)
]

ZOOM_RANGE = (2, 6)
TILE_SIZE = 256


def mercator_to_latlon(mx: float, my: float) -> tuple[float, float]:
    """Convert Web Mercator to lat/lon."""
    lon = mx / 20037508.342789244 * 180
    lat = atan(exp(my / 20037508.342789244 * pi)) * 360 / pi - 90
    return lat, lon


def tile_to_mercator_bounds(tx: int, ty: int, zoom: int) -> tuple[float, float, float, float]:
    """Get Web Mercator bounds for a tile (minx, miny, maxx, maxy)."""
    n = 2 ** zoom
    WORLD = 20037508.342789244 * 2
    tile_size = WORLD / n
    
    minx = tx * tile_size - WORLD / 2
    maxx = (tx + 1) * tile_size - WORLD / 2
    maxy = WORLD / 2 - ty * tile_size
    miny = WORLD / 2 - (ty + 1) * tile_size
    
    return minx, miny, maxx, maxy


def generate_tiles():
    print("=" * 60)
    print("Hass Avocado Tile Generator v3")
    print("Direct lat/lon to Web Mercator sampling")
    print("=" * 60)
    
    if not INPUT_FILE.exists():
        print(f"ERROR: Input not found: {INPUT_FILE}")
        sys.exit(1)
    
    # Load source data
    print(f"\nReading {INPUT_FILE}...")
    with rasterio.open(INPUT_FILE) as src:
        similarity = src.read(1)
        bounds = src.bounds
        h, w = similarity.shape
        
        # Source extent in lat/lon
        src_lon_min, src_lat_min = bounds.left, bounds.bottom
        src_lon_max, src_lat_max = bounds.right, bounds.top
        
        print(f"  Size: {w} x {h}")
        print(f"  Lat: {src_lat_min:.2f} to {src_lat_max:.2f}")
        print(f"  Lon: {src_lon_min:.2f} to {src_lon_max:.2f}")
    
    # Colorize
    print("\nColorizing...")
    rgba = np.zeros((h, w, 4), dtype=np.uint8)
    for threshold, r, g, b in sorted(COLOR_PALETTE, key=lambda x: x[0]):
        mask = similarity >= threshold
        rgba[mask, 0] = r
        rgba[mask, 1] = g
        rgba[mask, 2] = b
        rgba[mask, 3] = 200
    
    min_thresh = min(t for t, _, _, _ in COLOR_PALETTE)
    rgba[similarity < min_thresh, 3] = 0
    print(f"  Visible pixels: {np.count_nonzero(rgba[:,:,3] > 0):,}")
    
    # Clean output
    import shutil
    if OUTPUT_DIR.exists():
        shutil.rmtree(OUTPUT_DIR)
    OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
    
    # Generate tiles
    print("\nGenerating tiles...")
    total_tiles = 0
    
    for zoom in range(ZOOM_RANGE[0], ZOOM_RANGE[1] + 1):
        n = 2 ** zoom
        zoom_tiles = 0
        
        for tx in range(n):
            for ty in range(n):
                # Get tile bounds in Web Mercator
                minx, miny, maxx, maxy = tile_to_mercator_bounds(tx, ty, zoom)
                
                # Convert corners to lat/lon
                lat_min, lon_min = mercator_to_latlon(minx, miny)
                lat_max, lon_max = mercator_to_latlon(maxx, maxy)
                
                # Check if tile overlaps source data
                if lon_max < src_lon_min or lon_min > src_lon_max:
                    continue
                if lat_max < src_lat_min or lat_min > src_lat_max:
                    continue
                
                # Create tile by sampling from source
                tile_rgba = np.zeros((TILE_SIZE, TILE_SIZE, 4), dtype=np.uint8)
                
                for py in range(TILE_SIZE):
                    for px in range(TILE_SIZE):
                        # Position in tile (0-1)
                        tx_frac = (px + 0.5) / TILE_SIZE
                        ty_frac = (py + 0.5) / TILE_SIZE
                        
                        # Web Mercator position
                        mx = minx + tx_frac * (maxx - minx)
                        my = maxy - ty_frac * (maxy - miny)  # Y is flipped
                        
                        # Convert to lat/lon
                        lat, lon = mercator_to_latlon(mx, my)
                        
                        # Check bounds
                        if lat < src_lat_min or lat > src_lat_max:
                            continue
                        if lon < src_lon_min or lon > src_lon_max:
                            continue
                        
                        # Sample from source (nearest neighbor)
                        src_x = int((lon - src_lon_min) / (src_lon_max - src_lon_min) * w)
                        src_y = int((src_lat_max - lat) / (src_lat_max - src_lat_min) * h)
                        
                        # Clamp
                        src_x = max(0, min(w - 1, src_x))
                        src_y = max(0, min(h - 1, src_y))
                        
                        tile_rgba[py, px] = rgba[src_y, src_x]
                
                # Skip empty tiles
                if np.max(tile_rgba[:,:,3]) == 0:
                    continue
                
                # Save tile
                tile_dir = OUTPUT_DIR / str(zoom) / str(tx)
                tile_dir.mkdir(parents=True, exist_ok=True)
                Image.fromarray(tile_rgba, mode='RGBA').save(tile_dir / f"{ty}.png")
                zoom_tiles += 1
        
        print(f"  Zoom {zoom}: {zoom_tiles} tiles")
        total_tiles += zoom_tiles
    
    print(f"\n✓ Generated {total_tiles} tiles")


if __name__ == "__main__":
    generate_tiles()
