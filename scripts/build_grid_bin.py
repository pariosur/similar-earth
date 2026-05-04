#!/usr/bin/env python3
"""
Convert the 64-band AlphaEarth GeoTIFF to a compact binary grid file.

Reads the raw float32 embeddings exported by export_global_embeddings.py,
quantizes them to int8 using per-band min/max scaling, and writes a binary
file (grid.bin) that the web app can load efficiently.

Binary format (TRGRID01):
    Header (568 bytes):
        magic:   8 bytes  "TRGRID01" ASCII
        version: 4 bytes  uint32 = 1
        bands:   4 bytes  uint32 = 64
        width:   4 bytes  uint32
        height:  4 bytes  uint32
        west:    8 bytes  float64
        south:   8 bytes  float64
        east:    8 bytes  float64
        north:   8 bytes  float64
        scale:   256 bytes float32[64] (per-band scale for dequantization)
        offset:  256 bytes float32[64] (per-band offset for dequantization)
    Data (width * height * 64 bytes):
        int8 vectors, row-major (top-to-bottom, left-to-right),
        band-interleaved-by-pixel
    Land mask (ceil(width * height / 8) bytes):
        bit-packed, 1 = land, 0 = water/nodata

Dequantization: float_value = int8_value * scale[band] + offset[band]

Usage:
    python scripts/build_grid_bin.py [input.tif]

    If no input path given, defaults to scripts/data/alphaearth_embeddings_2km_2024.tif
"""

import math
import struct
import sys
from pathlib import Path

import numpy as np

try:
    import rasterio
except ImportError:
    print("ERROR: rasterio not installed. Run: pip install rasterio")
    sys.exit(1)

# =============================================================================
# Configuration
# =============================================================================

SCRIPT_DIR = Path(__file__).parent
DEFAULT_INPUT = SCRIPT_DIR / "data" / "alphaearth_embeddings_2km_2024.tif"
OUTPUT_PATH = SCRIPT_DIR.parent / "data" / "grid.bin"

MAGIC = b"TRGRID01"
VERSION = 1
NUM_BANDS = 64
NODATA_INT8 = -128  # Reserved for nodata/water
INT8_MIN = -127
INT8_MAX = 127


def read_geotiff(path: Path) -> tuple:
    """
    Read the 64-band GeoTIFF and return pixel data + georeferencing.

    Returns:
        data: np.ndarray shape (height, width, 64) float32
        bounds: (west, south, east, north)
    """
    print(f"Reading GeoTIFF: {path}")
    print(f"  File size: {path.stat().st_size / (1024**3):.2f} GB")

    with rasterio.open(path) as ds:
        if ds.count != NUM_BANDS:
            print(f"ERROR: Expected {NUM_BANDS} bands, got {ds.count}")
            sys.exit(1)

        width = ds.width
        height = ds.height
        bounds = ds.bounds  # BoundingBox(left, bottom, right, top)

        print(f"  Dimensions: {width} x {height}")
        print(f"  Bounds: W={bounds.left:.4f} S={bounds.bottom:.4f} "
              f"E={bounds.right:.4f} N={bounds.top:.4f}")
        print(f"  CRS: {ds.crs}")

        # Read all bands: shape (64, height, width) -> transpose to (height, width, 64)
        print("  Reading pixel data (this may take a while)...")
        data = ds.read()  # (bands, height, width)
        data = np.transpose(data, (1, 2, 0)).astype(np.float32)  # (height, width, bands)

    print(f"  Data shape: {data.shape}, dtype: {data.dtype}")
    return data, (bounds.left, bounds.bottom, bounds.right, bounds.top)


def compute_land_mask(data: np.ndarray) -> np.ndarray:
    """
    Determine land vs water/nodata pixels.
    A pixel is land if any band has a nonzero value.

    Returns: bool array shape (height, width), True = land
    """
    # Water was masked to 0 across all bands during export
    land = np.any(data != 0, axis=2)
    return land


def quantize_embeddings(data: np.ndarray, land_mask: np.ndarray) -> tuple:
    """
    Quantize float32 embeddings to int8 using per-band min/max scaling.

    For each band:
        offset = min_value (across land pixels)
        scale  = (max_value - min_value) / 254  (maps to -127..127 range)
        int8   = round((float_value - offset) / scale) + INT8_MIN
               = round((float_value - offset) / scale) - 127

    Actually, to map [min, max] -> [-127, 127]:
        int8 = round((value - min) / (max - min) * 254 - 127)
        Dequant: value = (int8 + 127) / 254 * (max - min) + min
                       = int8 * ((max-min)/254) + (min + 127*(max-min)/254)
                       = int8 * scale + offset

    Where:
        scale  = (max - min) / 254
        offset = min + 127 * scale  (i.e., the value when int8 = 0)

    Returns:
        quantized: int8 array (height, width, 64)
        scales: float32 array (64,)
        offsets: float32 array (64,)
    """
    height, width, bands = data.shape
    quantized = np.full((height, width, bands), NODATA_INT8, dtype=np.int8)
    scales = np.zeros(bands, dtype=np.float32)
    offsets = np.zeros(bands, dtype=np.float32)

    land_pixels = data[land_mask]  # shape (N_land, 64)

    print(f"\nQuantizing {bands} bands (int8, per-band min/max scaling)...")
    for b in range(bands):
        band_vals = land_pixels[:, b]
        bmin = float(np.min(band_vals))
        bmax = float(np.max(band_vals))

        if bmax == bmin:
            # Constant band — scale=1, offset=bmin, all values map to 0
            scales[b] = 1.0
            offsets[b] = bmin
            quantized[land_mask, b] = 0
        else:
            scale = (bmax - bmin) / 254.0
            offset = bmin + 127.0 * scale

            scales[b] = scale
            offsets[b] = offset

            # Quantize: int8 = round((value - offset) / scale), clamped to [-127, 127]
            q = np.round((data[:, :, b][land_mask] - offset) / scale).astype(np.int32)
            q = np.clip(q, INT8_MIN, INT8_MAX).astype(np.int8)
            quantized[land_mask, b] = q

        if (b + 1) % 16 == 0:
            print(f"  Band A{b:02d}: min={bmin:.4f} max={bmax:.4f} scale={scales[b]:.6f}")

    return quantized, scales, offsets


def pack_land_mask(land_mask: np.ndarray) -> bytes:
    """
    Bit-pack the land mask: 1 = land, 0 = water/nodata.
    Row-major order, MSB first within each byte.
    """
    flat = land_mask.ravel().astype(np.uint8)
    # Pad to multiple of 8
    remainder = len(flat) % 8
    if remainder:
        flat = np.concatenate([flat, np.zeros(8 - remainder, dtype=np.uint8)])

    # Pack 8 bits per byte
    packed = np.packbits(flat, bitorder="big")
    return packed.tobytes()


def write_grid_bin(
    output_path: Path,
    quantized: np.ndarray,
    scales: np.ndarray,
    offsets: np.ndarray,
    bounds: tuple,
    land_mask: np.ndarray,
):
    """Write the binary grid file."""
    height, width, bands = quantized.shape
    west, south, east, north = bounds

    output_path.parent.mkdir(parents=True, exist_ok=True)

    print(f"\nWriting grid.bin to {output_path}")

    with open(output_path, "wb") as f:
        # Header: magic (8) + version (4) + bands (4) + width (4) + height (4)
        #       + west (8) + south (8) + east (8) + north (8)
        #       + scale[64] (256) + offset[64] (256)
        # Total = 8 + 4 + 4 + 4 + 4 + 8 + 8 + 8 + 8 + 256 + 256 = 568 bytes
        f.write(MAGIC)
        f.write(struct.pack("<I", VERSION))
        f.write(struct.pack("<I", bands))
        f.write(struct.pack("<I", width))
        f.write(struct.pack("<I", height))
        f.write(struct.pack("<d", west))
        f.write(struct.pack("<d", south))
        f.write(struct.pack("<d", east))
        f.write(struct.pack("<d", north))
        f.write(scales.tobytes())
        f.write(offsets.tobytes())

        header_size = f.tell()
        assert header_size == 568, f"Header size mismatch: {header_size} != 568"

        # Data section: row-major, band-interleaved-by-pixel
        # quantized is already (height, width, bands) which is the right layout
        f.write(quantized.tobytes())

        # Land mask section: bit-packed
        mask_bytes = pack_land_mask(land_mask)
        f.write(mask_bytes)

    file_size = output_path.stat().st_size
    print(f"  Header:    {header_size} bytes")
    print(f"  Data:      {height * width * bands} bytes")
    print(f"  Land mask: {len(mask_bytes)} bytes")
    print(f"  Total:     {file_size:,} bytes ({file_size / (1024**2):.1f} MB)")


def main():
    # Determine input path
    if len(sys.argv) > 1:
        input_path = Path(sys.argv[1])
    else:
        input_path = DEFAULT_INPUT

    if not input_path.exists():
        print(f"ERROR: Input file not found: {input_path}")
        print(f"\nDownload the GeoTIFF from Google Drive (tierra-exports folder)")
        print(f"and place it at: {DEFAULT_INPUT}")
        sys.exit(1)

    print("=" * 60)
    print("Build grid.bin from AlphaEarth embeddings GeoTIFF")
    print("=" * 60)

    # Read GeoTIFF
    data, bounds = read_geotiff(input_path)
    height, width, bands = data.shape

    # Compute land mask
    land_mask = compute_land_mask(data)
    total_pixels = height * width
    land_pixels = int(np.sum(land_mask))
    water_pixels = total_pixels - land_pixels

    print(f"\nPixel statistics:")
    print(f"  Total pixels:  {total_pixels:>12,}")
    print(f"  Land pixels:   {land_pixels:>12,} ({100 * land_pixels / total_pixels:.1f}%)")
    print(f"  Water pixels:  {water_pixels:>12,} ({100 * water_pixels / total_pixels:.1f}%)")

    # Quantize
    quantized, scales, offsets = quantize_embeddings(data, land_mask)

    # Verify round-trip accuracy on a sample
    sample_idx = np.where(land_mask.ravel())[0][:1000]
    if len(sample_idx) > 0:
        flat_q = quantized.reshape(-1, bands)[sample_idx].astype(np.float32)
        flat_orig = data.reshape(-1, bands)[sample_idx]
        reconstructed = flat_q * scales[np.newaxis, :] + offsets[np.newaxis, :]
        mse = np.mean((reconstructed - flat_orig) ** 2)
        max_err = np.max(np.abs(reconstructed - flat_orig))
        print(f"\nQuantization accuracy (sample of {len(sample_idx)} land pixels):")
        print(f"  MSE:       {mse:.6f}")
        print(f"  Max error: {max_err:.6f}")

    # Write binary file
    write_grid_bin(OUTPUT_PATH, quantized, scales, offsets, bounds, land_mask)

    print("\n" + "=" * 60)
    print("DONE")
    print(f"Output: {OUTPUT_PATH}")
    print(f"\nNext step: run validate_embeddings.py to verify quality")
    print("=" * 60)


if __name__ == "__main__":
    main()
