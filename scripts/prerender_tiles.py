#!/usr/bin/env python3
"""
Pre-render PNG tiles from score files for all crops.

Reads data/scores_<crop>.bin files and outputs tiles/<crop>/<z>/<x>/<y>.png
These can be served as static files (local dev) or uploaded to a CDN (production).

Usage:
    python scripts/prerender_tiles.py [--crop coffee] [--min-zoom 2] [--max-zoom 8]
"""

import struct
import math
import os
import sys
import time
import numpy as np
from pathlib import Path
from PIL import Image

TILE_SIZE = 256


# ============================================================================
# Color ramp (must match internal/tiles/colorramp.go exactly)
# ============================================================================

def lerp8(a, b, t):
    v = a + (b - a) * t
    return int(max(0, min(255, v)))


def build_color_ramp():
    """Build the 256-entry RGBA color ramp matching the Go DefaultRamp()."""
    ramp = np.zeros((256, 4), dtype=np.uint8)

    for i in range(256):
        if i <= 140:
            r, g, b, a = 0, 0, 0, 0
        elif i <= 166:
            f = (i - 141) / (166 - 141)
            r = lerp8(200, 240, f)
            g = lerp8(180, 200, f)
            b = lerp8(50, 30, f)
            a = lerp8(60, 140, f)
        elif i <= 204:
            f = (i - 167) / (204 - 167)
            r = lerp8(240, 245, f)
            g = lerp8(200, 120, f)
            b = lerp8(30, 20, f)
            a = lerp8(140, 210, f)
        elif i <= 240:
            f = (i - 205) / (240 - 205)
            r = lerp8(245, 220, f)
            g = lerp8(120, 40, f)
            b = lerp8(20, 20, f)
            a = lerp8(210, 240, f)
        else:
            f = (i - 241) / max(1, (255 - 241))
            r = lerp8(220, 180, f)
            g = lerp8(40, 15, f)
            b = lerp8(20, 30, f)
            a = lerp8(240, 255, f)

        ramp[i] = [r, g, b, a]

    return ramp


RAMP = build_color_ramp()


# ============================================================================
# Grid header (for coordinate mapping)
# ============================================================================

def load_grid_header(path):
    """Load just the grid header (no data) for coordinate mapping."""
    with open(path, "rb") as f:
        magic = f.read(8)
        assert magic == b"TRGRID01"
        f.read(4)  # version
        bands = struct.unpack("<I", f.read(4))[0]
        width = struct.unpack("<I", f.read(4))[0]
        height = struct.unpack("<I", f.read(4))[0]
        west, south, east, north = struct.unpack("<4d", f.read(32))

    return {
        "width": width, "height": height,
        "west": west, "south": south, "east": east, "north": north,
        "cell_w": (east - west) / width,
        "cell_h": (north - south) / height,
    }


# ============================================================================
# Score file loading
# ============================================================================

def load_scores(path):
    """Load a scores file, returning (width, height, scores_array)."""
    with open(path, "rb") as f:
        width, height, pin_count, _ = struct.unpack("<IIII", f.read(16))
        total = width * height
        scores = np.frombuffer(f.read(total * 4), dtype=np.float32).reshape(height, width)
    return width, height, scores


# ============================================================================
# Tile math (matches Go TileBounds exactly)
# ============================================================================

def tile_bounds(z, x, y):
    """Convert tile z/x/y to geographic bounds (west, south, east, north)."""
    n = 2.0 ** z
    west = x / n * 360.0 - 180.0
    east = (x + 1) / n * 360.0 - 180.0
    north = tile_lat_deg(y, z)
    south = tile_lat_deg(y + 1, z)
    return west, south, east, north


def tile_lat_deg(y, z):
    n = math.pi - 2.0 * math.pi * y / (2.0 ** z)
    return 180.0 / math.pi * math.atan(math.sinh(n))


# ============================================================================
# Tile rendering
# ============================================================================

def render_tile(scores, grid, z, x, y):
    """Render a 256x256 RGBA tile. Returns PIL Image or None if empty."""
    west, south, east, north = tile_bounds(z, x, y)

    # Check if tile overlaps with grid bounds at all
    if east < grid["west"] or west > grid["east"]:
        return None
    if north < grid["south"] or south > grid["north"]:
        return None

    lng_step = (east - west) / TILE_SIZE
    lat_step = (north - south) / TILE_SIZE

    # Build coordinate arrays for all 256x256 pixels
    px_range = np.arange(TILE_SIZE)
    py_range = np.arange(TILE_SIZE)

    lngs = west + (px_range + 0.5) * lng_step
    lats = north - (py_range + 0.5) * lat_step

    # Convert to grid row/col
    cols = ((lngs - grid["west"]) / grid["cell_w"]).astype(np.int32)
    rows = ((grid["north"] - lats) / grid["cell_h"]).astype(np.int32)

    # Clamp to valid range
    cols = np.clip(cols, 0, grid["width"] - 1)
    rows = np.clip(rows, 0, grid["height"] - 1)

    # Mask out-of-bounds
    col_valid = (lngs >= grid["west"]) & (lngs <= grid["east"])
    row_valid = (lats >= grid["south"]) & (lats <= grid["north"])

    # Sample scores for all pixels
    tile_scores = np.zeros((TILE_SIZE, TILE_SIZE), dtype=np.float32)
    for py_idx in range(TILE_SIZE):
        if not row_valid[py_idx]:
            continue
        r = rows[py_idx]
        for px_idx in range(TILE_SIZE):
            if not col_valid[px_idx]:
                continue
            c = cols[px_idx]
            tile_scores[py_idx, px_idx] = scores[r, c]

    # Check if tile is all transparent (no scores above threshold)
    # Ramp index 140 = score 0.55 is the visibility threshold
    max_score = tile_scores.max()
    if max_score < 0.55:
        return None  # Skip empty tiles

    # Map scores to color ramp indices
    indices = np.clip((tile_scores * 255).astype(np.int32), 0, 255)

    # Look up colors
    rgba = RAMP[indices]  # (256, 256, 4)

    return Image.fromarray(rgba, "RGBA")


def render_tile_fast(scores, grid, z, x, y):
    """Optimized tile rendering using vectorized numpy operations."""
    west, south, east, north = tile_bounds(z, x, y)

    if east < grid["west"] or west > grid["east"]:
        return None
    if north < grid["south"] or south > grid["north"]:
        return None

    lng_step = (east - west) / TILE_SIZE
    lat_step = (north - south) / TILE_SIZE

    lngs = west + (np.arange(TILE_SIZE) + 0.5) * lng_step
    lats = north - (np.arange(TILE_SIZE) + 0.5) * lat_step

    cols = np.clip(((lngs - grid["west"]) / grid["cell_w"]).astype(np.int32), 0, grid["width"] - 1)
    rows = np.clip(((grid["north"] - lats) / grid["cell_h"]).astype(np.int32), 0, grid["height"] - 1)

    # Vectorized: sample entire tile at once
    tile_scores = scores[rows[:, None], cols[None, :]]  # (256, 256)

    # Mask out-of-bounds to 0
    col_mask = (lngs >= grid["west"]) & (lngs <= grid["east"])
    row_mask = (lats >= grid["south"]) & (lats <= grid["north"])
    mask = row_mask[:, None] & col_mask[None, :]
    tile_scores = np.where(mask, tile_scores, 0.0)

    if tile_scores.max() < 0.55:
        return None

    indices = np.clip((tile_scores * 255).astype(np.int32), 0, 255)
    rgba = RAMP[indices]

    return Image.fromarray(rgba, "RGBA")


# ============================================================================
# Main
# ============================================================================

def main():
    import argparse
    parser = argparse.ArgumentParser()
    parser.add_argument("--grid", default="data/grid.bin")
    parser.add_argument("--scores-dir", default="data")
    parser.add_argument("--output-dir", default="tiles")
    parser.add_argument("--crop", help="Only render this crop")
    parser.add_argument("--min-zoom", type=int, default=2)
    parser.add_argument("--max-zoom", type=int, default=8)
    args = parser.parse_args()

    print("Loading grid header...")
    grid = load_grid_header(args.grid)
    print(f"Grid: {grid['width']}x{grid['height']}, "
          f"bbox: [{grid['west']:.1f}, {grid['south']:.1f}, {grid['east']:.1f}, {grid['north']:.1f}]")

    # Find score files
    scores_dir = Path(args.scores_dir)
    if args.crop:
        score_files = [scores_dir / f"scores_{args.crop}.bin"]
    else:
        score_files = sorted(scores_dir.glob("scores_*.bin"))

    if not score_files:
        print("No score files found!")
        sys.exit(1)

    t0 = time.time()
    total_tiles = 0
    total_skipped = 0

    for score_file in score_files:
        crop_id = score_file.stem.replace("scores_", "")
        print(f"\n{'='*50}")
        print(f"Rendering: {crop_id}")
        print(f"{'='*50}")

        width, height, scores = load_scores(str(score_file))
        print(f"  Scores: {width}x{height}, max={scores.max():.3f}")

        crop_tiles = 0
        crop_skipped = 0

        for z in range(args.min_zoom, args.max_zoom + 1):
            n_tiles = 2 ** z
            z_tiles = 0
            z_start = time.time()

            for x in range(n_tiles):
                for y in range(n_tiles):
                    img = render_tile_fast(scores, grid, z, x, y)

                    if img is None:
                        crop_skipped += 1
                        continue

                    # Save tile
                    tile_path = Path(args.output_dir) / crop_id / str(z) / str(x) / f"{y}.png"
                    tile_path.parent.mkdir(parents=True, exist_ok=True)
                    img.save(str(tile_path), "PNG", optimize=True)

                    crop_tiles += 1
                    z_tiles += 1

            z_elapsed = time.time() - z_start
            print(f"  z{z}: {z_tiles} tiles ({n_tiles*n_tiles - z_tiles} empty) in {z_elapsed:.1f}s")

        total_tiles += crop_tiles
        total_skipped += crop_skipped
        print(f"  Total: {crop_tiles} tiles, {crop_skipped} empty/skipped")

    elapsed = time.time() - t0

    # Calculate total size
    output_path = Path(args.output_dir)
    total_size = sum(f.stat().st_size for f in output_path.rglob("*.png"))
    size_mb = total_size / 1024 / 1024

    print(f"\n{'='*50}")
    print(f"Done! {total_tiles} tiles rendered, {total_skipped} empty")
    print(f"Total size: {size_mb:.1f} MB")
    print(f"Time: {elapsed:.0f}s")
    print(f"Output: {args.output_dir}/")
    print(f"{'='*50}")


if __name__ == "__main__":
    main()
