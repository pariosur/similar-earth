#!/usr/bin/env python3
"""
Validate grid.bin embeddings against known Hass avocado farm locations.

Tests:
1. Load grid.bin and look up embeddings for each verified farm
2. Compute pairwise dot products between all avocado farm embeddings
3. Compare intra-crop similarity to random land pixel baseline
4. Leave-one-out: for each farm, check if other farms rank highly
   in the global similarity distribution

Usage:
    python scripts/validate_embeddings.py [grid.bin path]
"""

import csv
import math
import struct
import sys
from pathlib import Path

import numpy as np

# =============================================================================
# Configuration
# =============================================================================

SCRIPT_DIR = Path(__file__).parent
DEFAULT_GRID = SCRIPT_DIR.parent / "data" / "grid.bin"
GOLD_STANDARD_CSV = SCRIPT_DIR.parent / "gold_standard_verified_yields.csv.bak"

NUM_RANDOM_SAMPLES = 5000  # Random land pixels for baseline
MAGIC = b"TRGRID01"
NUM_BANDS = 64


def load_grid(path: Path) -> dict:
    """
    Load the grid.bin binary file.

    Returns dict with keys:
        width, height, bounds (west, south, east, north),
        scales (64,), offsets (64,),
        data (height, width, 64) int8,
        land_mask (height, width) bool
    """
    print(f"Loading grid: {path}")
    file_size = path.stat().st_size
    print(f"  File size: {file_size / (1024**2):.1f} MB")

    with open(path, "rb") as f:
        # Header
        magic = f.read(8)
        assert magic == MAGIC, f"Bad magic: {magic!r}"

        version = struct.unpack("<I", f.read(4))[0]
        bands = struct.unpack("<I", f.read(4))[0]
        width = struct.unpack("<I", f.read(4))[0]
        height = struct.unpack("<I", f.read(4))[0]
        west = struct.unpack("<d", f.read(8))[0]
        south = struct.unpack("<d", f.read(8))[0]
        east = struct.unpack("<d", f.read(8))[0]
        north = struct.unpack("<d", f.read(8))[0]

        assert bands == NUM_BANDS, f"Expected {NUM_BANDS} bands, got {bands}"

        scales = np.frombuffer(f.read(256), dtype=np.float32).copy()
        offsets = np.frombuffer(f.read(256), dtype=np.float32).copy()

        assert f.tell() == 568, f"Header size: {f.tell()}"

        # Data
        data_bytes = width * height * bands
        raw = np.frombuffer(f.read(data_bytes), dtype=np.int8).copy()
        data = raw.reshape(height, width, bands)

        # Land mask
        total_pixels = width * height
        mask_bytes = math.ceil(total_pixels / 8)
        mask_raw = np.frombuffer(f.read(mask_bytes), dtype=np.uint8)
        land_flat = np.unpackbits(mask_raw, bitorder="big")[:total_pixels]
        land_mask = land_flat.reshape(height, width).astype(bool)

    print(f"  Grid: {width} x {height}, {bands} bands")
    print(f"  Bounds: W={west:.4f} S={south:.4f} E={east:.4f} N={north:.4f}")
    print(f"  Land pixels: {np.sum(land_mask):,} / {total_pixels:,}")

    return {
        "width": width,
        "height": height,
        "bounds": (west, south, east, north),
        "scales": scales,
        "offsets": offsets,
        "data": data,
        "land_mask": land_mask,
    }


def dequantize(int8_vals: np.ndarray, scales: np.ndarray, offsets: np.ndarray) -> np.ndarray:
    """Convert int8 values back to float32 using per-band scale/offset."""
    return int8_vals.astype(np.float32) * scales + offsets


def lonlat_to_pixel(lon: float, lat: float, grid: dict) -> tuple:
    """Convert lon/lat to pixel col/row in the grid."""
    west, south, east, north = grid["bounds"]
    width, height = grid["width"], grid["height"]

    # Pixel size
    px = (east - west) / width
    py = (north - south) / height

    col = int((lon - west) / px)
    row = int((north - lat) / py)  # Row 0 is top (north)

    col = max(0, min(col, width - 1))
    row = max(0, min(row, height - 1))

    return col, row


def load_farms() -> list[dict]:
    """Load Hass avocado farms from gold standard CSV."""
    farms = []
    with open(GOLD_STANDARD_CSV, "r") as f:
        reader = csv.DictReader(f)
        for row in reader:
            if row["crop"].lower() == "hass avocado":
                farms.append({
                    "name": row["farm_name"],
                    "lat": float(row["latitude"]),
                    "lon": float(row["longitude"]),
                    "region": row["region"],
                    "country": row["country"],
                })
    return farms


def get_farm_embeddings(farms: list[dict], grid: dict) -> tuple:
    """
    Look up the embedding vector for each farm in the grid.
    Returns (embeddings array, valid_farms list) for farms that fall on land.
    """
    embeddings = []
    valid_farms = []

    for farm in farms:
        col, row = lonlat_to_pixel(farm["lon"], farm["lat"], grid)
        if not grid["land_mask"][row, col]:
            print(f"  SKIP (water): {farm['name']} ({farm['country']})")
            continue

        int8_vec = grid["data"][row, col, :]
        float_vec = dequantize(int8_vec, grid["scales"], grid["offsets"])
        embeddings.append(float_vec)
        valid_farms.append(farm)

    return np.array(embeddings), valid_farms


def sample_random_land(grid: dict, n: int, rng: np.random.Generator) -> np.ndarray:
    """Sample n random land pixel embeddings from the grid."""
    land_rows, land_cols = np.where(grid["land_mask"])
    total_land = len(land_rows)

    if n > total_land:
        n = total_land

    idx = rng.choice(total_land, size=n, replace=False)
    int8_vecs = grid["data"][land_rows[idx], land_cols[idx], :]
    return dequantize(int8_vecs, grid["scales"], grid["offsets"])


def compute_dot_products(a: np.ndarray, b: np.ndarray) -> np.ndarray:
    """Compute all pairwise dot products between rows of a and b."""
    return a @ b.T


def main():
    # Determine grid path
    if len(sys.argv) > 1:
        grid_path = Path(sys.argv[1])
    else:
        grid_path = DEFAULT_GRID

    if not grid_path.exists():
        print(f"ERROR: grid.bin not found: {grid_path}")
        print("Run build_grid_bin.py first.")
        sys.exit(1)

    if not GOLD_STANDARD_CSV.exists():
        print(f"ERROR: Gold standard CSV not found: {GOLD_STANDARD_CSV}")
        sys.exit(1)

    print("=" * 60)
    print("Validate AlphaEarth Embeddings (grid.bin)")
    print("=" * 60)

    # Load grid
    grid = load_grid(grid_path)

    # Load farms
    farms = load_farms()
    print(f"\nLoaded {len(farms)} Hass avocado farms from gold standard")

    # Look up farm embeddings
    print("\nLooking up farm embeddings in grid...")
    farm_embeddings, valid_farms = get_farm_embeddings(farms, grid)
    n_farms = len(valid_farms)
    print(f"  Valid farms on land: {n_farms}/{len(farms)}")

    if n_farms < 3:
        print("ERROR: Too few valid farms to compute meaningful statistics")
        sys.exit(1)

    # =========================================================================
    # 1. Intra-crop pairwise similarity
    # =========================================================================
    print("\n" + "-" * 60)
    print("1. INTRA-CROP PAIRWISE SIMILARITY (dot product)")
    print("-" * 60)

    farm_dots = compute_dot_products(farm_embeddings, farm_embeddings)
    # Extract upper triangle (exclude diagonal = self-similarity)
    triu_idx = np.triu_indices(n_farms, k=1)
    intra_sims = farm_dots[triu_idx]

    print(f"  Pairs:  {len(intra_sims)}")
    print(f"  Mean:   {np.mean(intra_sims):.4f}")
    print(f"  Std:    {np.std(intra_sims):.4f}")
    print(f"  Min:    {np.min(intra_sims):.4f}")
    print(f"  Max:    {np.max(intra_sims):.4f}")
    print(f"  Median: {np.median(intra_sims):.4f}")

    # =========================================================================
    # 2. Random baseline similarity
    # =========================================================================
    print("\n" + "-" * 60)
    print("2. RANDOM BASELINE (farm vs random land pixels)")
    print("-" * 60)

    rng = np.random.default_rng(42)
    random_embeddings = sample_random_land(grid, NUM_RANDOM_SAMPLES, rng)
    print(f"  Sampled {len(random_embeddings)} random land pixels")

    # Farm-to-random dot products
    farm_random_dots = compute_dot_products(farm_embeddings, random_embeddings)
    baseline_sims = farm_random_dots.ravel()

    print(f"  Pairs:  {len(baseline_sims)}")
    print(f"  Mean:   {np.mean(baseline_sims):.4f}")
    print(f"  Std:    {np.std(baseline_sims):.4f}")
    print(f"  Min:    {np.min(baseline_sims):.4f}")
    print(f"  Max:    {np.max(baseline_sims):.4f}")
    print(f"  Median: {np.median(baseline_sims):.4f}")

    # =========================================================================
    # 3. Signal-to-noise ratio
    # =========================================================================
    print("\n" + "-" * 60)
    print("3. SIGNAL VS NOISE")
    print("-" * 60)

    intra_mean = np.mean(intra_sims)
    baseline_mean = np.mean(baseline_sims)
    baseline_std = np.std(baseline_sims)

    ratio = intra_mean / baseline_mean if baseline_mean != 0 else float("inf")
    separation = (intra_mean - baseline_mean) / baseline_std if baseline_std > 0 else float("inf")

    print(f"  Intra-crop mean:   {intra_mean:.4f}")
    print(f"  Random mean:       {baseline_mean:.4f}")
    print(f"  Ratio:             {ratio:.2f}x")
    print(f"  Separation (z):    {separation:.2f} std devs")

    if ratio > 1.5:
        print("  --> GOOD: Avocado farms are notably more similar to each other than random")
    elif ratio > 1.1:
        print("  --> MODERATE: Some signal, but noisy")
    else:
        print("  --> WEAK: Embeddings may not capture crop-specific features well")

    # =========================================================================
    # 4. Leave-one-out ranking test
    # =========================================================================
    print("\n" + "-" * 60)
    print("4. LEAVE-ONE-OUT RANKING TEST")
    print("-" * 60)
    print("  For each farm, how do other farms rank vs random pixels?\n")

    # Combine farm and random embeddings for ranking
    all_embeddings = np.vstack([farm_embeddings, random_embeddings])
    n_total = len(all_embeddings)

    percentiles = []

    for i in range(n_farms):
        query = farm_embeddings[i : i + 1]  # (1, 64)

        # Dot product with everything (excluding self)
        dots = compute_dot_products(query, all_embeddings).ravel()
        dots[i] = -np.inf  # Exclude self

        # Rank all other farms
        sorted_idx = np.argsort(-dots)  # Descending
        ranks = np.argsort(sorted_idx)  # Position of each index in sorted order

        other_farm_ranks = []
        for j in range(n_farms):
            if j != i:
                other_farm_ranks.append(ranks[j])

        mean_rank = np.mean(other_farm_ranks)
        best_rank = np.min(other_farm_ranks)
        pctile = 100.0 * (1.0 - mean_rank / (n_total - 1))
        percentiles.append(pctile)

        farm = valid_farms[i]
        print(f"  {farm['name'][:40]:<40s} "
              f"mean_rank={mean_rank:>6.0f}  best={best_rank:>5d}  "
              f"top {pctile:.1f}%")

    print(f"\n  Mean percentile: {np.mean(percentiles):.1f}%")
    print(f"  Median percentile: {np.median(percentiles):.1f}%")

    if np.mean(percentiles) > 90:
        print("  --> EXCELLENT: Farms consistently in top 10% of global similarity")
    elif np.mean(percentiles) > 75:
        print("  --> GOOD: Farms generally rank high")
    elif np.mean(percentiles) > 50:
        print("  --> MODERATE: Some discriminative power")
    else:
        print("  --> POOR: Embeddings do not clearly distinguish this crop type")

    # =========================================================================
    # Summary
    # =========================================================================
    print("\n" + "=" * 60)
    print("SUMMARY")
    print("=" * 60)
    print(f"  Farms validated:      {n_farms}")
    print(f"  Intra-crop sim:       {intra_mean:.4f} +/- {np.std(intra_sims):.4f}")
    print(f"  Random baseline:      {baseline_mean:.4f} +/- {baseline_std:.4f}")
    print(f"  Similarity ratio:     {ratio:.2f}x")
    print(f"  Z-separation:         {separation:.2f}")
    print(f"  Mean LOO percentile:  {np.mean(percentiles):.1f}%")


if __name__ == "__main__":
    main()
