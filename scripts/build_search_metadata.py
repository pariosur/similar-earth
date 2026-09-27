#!/usr/bin/env python3
"""Align annual-mean temperature and Natural Earth country IDs to grid.bin.

Inputs are prepared offline. Temperature must be a global annual-mean raster
in degrees Celsius. Countries must be Natural Earth Admin 0 polygons.
"""
import argparse
import json
import struct
from pathlib import Path

import numpy as np
import rasterio
from rasterio.enums import Resampling
from rasterio.features import rasterize
from rasterio.transform import Affine
from rasterio.vrt import WarpedVRT

MAGIC = b"SESRCH01"
HEADER = struct.Struct("<8sII")
MISSING_TEMP = -32768


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("grid", type=Path)
    parser.add_argument("temperature", type=Path, help="annual-mean temperature GeoTIFF in degrees Celsius")
    parser.add_argument("countries", type=Path, help="Natural Earth Admin 0 GeoPackage or Shapefile")
    parser.add_argument("output", type=Path)
    args = parser.parse_args()

    try:
        import geopandas as gpd
    except ImportError as exc:
        raise SystemExit("Install scripts/requirements-search.txt first") from exc

    with args.grid.open("rb") as f:
        header = f.read(568)
    if len(header) != 568 or header[:8] != b"TRGRID01":
        raise SystemExit("invalid grid.bin header")
    _, _, _, width, height = struct.unpack_from("<8sIIII", header)
    west, south, east, north = struct.unpack_from("<dddd", header, 24)
    transform = Affine((east - west) / width, 0, west, 0, -(north - south) / height, north)

    countries = gpd.read_file(args.countries).to_crs("EPSG:4326")
    features = []
    manifest = []
    for row in countries.itertuples():
        code = getattr(row, "ISO_A3", getattr(row, "ADM0_A3", "-99"))
        name = getattr(row, "NAME", getattr(row, "ADMIN", ""))
        continent = getattr(row, "CONTINENT", "")
        if not row.geometry or not code or code == "-99" or not name:
            continue
        country_id = len(manifest) + 1
        features.append((row.geometry, country_id))
        manifest.append({"id": country_id, "code": str(code), "name": str(name), "continent": str(continent)})
    if not manifest:
        raise SystemExit("country source has no usable country features")
    if len(manifest) > 65534:
        raise SystemExit("too many country polygons")

    args.output.parent.mkdir(parents=True, exist_ok=True)
    with rasterio.open(args.temperature) as source, args.output.open("wb") as out:
        out.write(HEADER.pack(MAGIC, width, height))
        with WarpedVRT(source, crs="EPSG:4326", transform=transform, width=width, height=height,
                       resampling=Resampling.bilinear, nodata=np.nan) as temp:
            rows_per_block = 128
            for row in range(0, height, rows_per_block):
                count = min(rows_per_block, height - row)
                window = rasterio.windows.Window(0, row, width, count)
                values = temp.read(1, window=window, out_dtype="float32", masked=True).filled(np.nan)
                ids = rasterize(features, out_shape=(count, width), transform=temp.window_transform(window),
                                fill=0, dtype="uint16")
                centidegrees = np.full((count, width), MISSING_TEMP, dtype="<i2")
                valid = np.isfinite(values)
                centidegrees[valid] = np.clip(np.rint(values[valid] * 100), -32767, 32767).astype("<i2")
                records = np.empty((count, width, 4), dtype=np.uint8)
                records[:, :, :2] = ids.astype("<u2").view(np.uint8).reshape(count, width, 2)
                records[:, :, 2:] = centidegrees.view(np.uint8).reshape(count, width, 2)
                out.write(records.tobytes())

    args.output.with_suffix(".json").write_text(json.dumps(manifest, separators=(",", ":")))
    print(f"wrote {args.output} ({args.output.stat().st_size:,} bytes), {len(manifest)} country features")


if __name__ == "__main__":
    main()
