#!/usr/bin/env python3
"""
Automatically generate reference farm coordinates for all major crops
using Earth Engine cropland data + known production region bounding boxes.

For each crop:
1. Define production region bounding boxes from FAO/USDA data
2. Sample ESA WorldCover cropland pixels within those regions
3. Verify they're on land (not water) using the grid
4. Output a JSON file with all crops and their reference coordinates

Usage:
    python scripts/generate_layer_references.py [--output data/layer_references.json]
"""

import ee
import json
import time
import sys
import os

# Initialize Earth Engine
PROJECT = os.environ.get("GEE_PROJECT", "tierra-ai")
try:
    ee.Initialize(project=PROJECT)
except Exception:
    ee.Authenticate()
    ee.Initialize(project=PROJECT)

# ESA WorldCover cropland class
WORLDCOVER = ee.ImageCollection("ESA/WorldCover/v200").first()
CROPLAND = WORLDCOVER.eq(40).selfMask()  # class 40 = cropland

# ============================================================================
# Layer definitions: production regions as bounding boxes
# Each crop has multiple regions to ensure geographic diversity
# Regions are [west, south, east, north]
# ============================================================================

LAYER_REGIONS = {
    # CEREALS
    "wheat": {
        "name": "Wheat",
        "category": "Cereals",
        "regions": {
            "Kansas, USA": [-99.5, 37.0, -96.5, 39.5],
            "Punjab, India": [74.0, 29.5, 76.5, 31.5],
            "Beauce, France": [1.0, 47.5, 2.5, 48.5],
            "NSW, Australia": [147.0, -34.0, 150.0, -32.0],
            "Pampas, Argentina": [-62.0, -36.0, -59.0, -34.0],
            "Shandong, China": [116.0, 35.5, 118.5, 37.5],
        },
    },
    "rice": {
        "name": "Rice",
        "category": "Cereals",
        "regions": {
            "Mekong Delta, Vietnam": [105.5, 9.5, 106.5, 10.5],
            "West Bengal, India": [87.5, 22.5, 89.0, 24.0],
            "Jiangsu, China": [119.0, 31.5, 121.0, 33.0],
            "Central Luzon, Philippines": [120.5, 15.0, 121.5, 16.0],
            "Sacramento Valley, USA": [-122.5, 38.5, -121.5, 39.5],
            "Chao Phraya, Thailand": [100.0, 14.5, 101.0, 15.5],
        },
    },
    "maize": {
        "name": "Maize (Corn)",
        "category": "Cereals",
        "regions": {
            "Iowa, USA": [-94.0, 41.0, -91.5, 43.0],
            "Mato Grosso, Brazil": [-56.0, -14.0, -53.0, -12.0],
            "Henan, China": [113.0, 33.5, 115.0, 35.0],
            "Cordoba, Argentina": [-64.0, -33.0, -62.0, -31.0],
            "Rift Valley, Kenya": [35.5, -0.5, 36.5, 0.5],
            "Brittany, France": [-3.5, 47.5, -1.5, 48.5],
        },
    },
    "barley": {
        "name": "Barley",
        "category": "Cereals",
        "regions": {
            "Bavaria, Germany": [10.5, 48.0, 12.5, 49.5],
            "Saskatchewan, Canada": [-107.0, 50.5, -104.0, 52.0],
            "Castilla, Spain": [-4.0, 40.5, -2.0, 42.0],
            "Victoria, Australia": [143.0, -37.0, 146.0, -35.5],
            "Rajasthan, India": [74.0, 26.0, 76.0, 28.0],
        },
    },
    "sorghum": {
        "name": "Sorghum",
        "category": "Cereals",
        "regions": {
            "Texas, USA": [-100.0, 31.0, -97.0, 33.5],
            "Nigeria Savanna": [7.0, 9.0, 9.0, 11.0],
            "Maharashtra, India": [74.5, 17.5, 76.5, 19.5],
            "Queensland, Australia": [149.0, -24.0, 151.0, -22.0],
            "Jalisco, Mexico": [-104.0, 20.0, -102.0, 21.5],
        },
    },
    "millet": {
        "name": "Millet",
        "category": "Cereals",
        "regions": {
            "Rajasthan, India": [71.0, 25.0, 73.0, 27.0],
            "Niger Sahel": [3.0, 13.0, 5.0, 14.5],
            "Mali": [-7.0, 12.0, -5.0, 14.0],
            "Northern Nigeria": [7.0, 11.0, 9.5, 13.0],
        },
    },
    # OILSEEDS
    "soybean": {
        "name": "Soybean",
        "category": "Oilseeds",
        "regions": {
            "Illinois, USA": [-90.0, 39.0, -87.5, 41.0],
            "Mato Grosso, Brazil": [-55.0, -13.0, -52.5, -11.0],
            "Santa Fe, Argentina": [-61.5, -33.5, -59.5, -31.5],
            "Heilongjiang, China": [126.0, 46.0, 128.5, 48.0],
            "Parana, Brazil": [-52.5, -24.5, -50.0, -22.5],
        },
    },
    "oil-palm": {
        "name": "Oil Palm",
        "category": "Oilseeds",
        "regions": {
            "Riau, Sumatra": [101.0, 0.5, 103.0, 2.0],
            "Sabah, Malaysia": [116.5, 5.0, 118.0, 6.5],
            "Cross River, Nigeria": [8.0, 5.5, 9.5, 6.5],
            "Esmeraldas, Ecuador": [-80.0, 0.0, -79.0, 1.0],
            "Kalimantan, Indonesia": [109.0, -1.5, 111.0, 0.0],
        },
    },
    "sunflower": {
        "name": "Sunflower",
        "category": "Oilseeds",
        "regions": {
            "Krasnodar, Russia": [38.0, 44.5, 40.5, 46.0],
            "Buenos Aires, Argentina": [-61.0, -37.0, -58.5, -35.0],
            "Andalucia, Spain": [-5.5, 37.0, -3.5, 38.5],
            "South Dakota, USA": [-100.0, 43.0, -97.5, 45.0],
            "Karnataka, India": [75.5, 15.0, 77.5, 17.0],
        },
    },
    "rapeseed": {
        "name": "Rapeseed (Canola)",
        "category": "Oilseeds",
        "regions": {
            "Alberta, Canada": [-114.0, 51.0, -111.0, 53.0],
            "Mecklenburg, Germany": [11.5, 53.5, 13.5, 54.5],
            "Hubei, China": [112.0, 29.5, 114.0, 31.0],
            "NSW, Australia": [148.0, -34.5, 150.5, -32.5],
        },
    },
    "groundnut": {
        "name": "Groundnut (Peanut)",
        "category": "Oilseeds",
        "regions": {
            "Georgia, USA": [-84.0, 31.0, -82.0, 33.0],
            "Gujarat, India": [70.5, 21.0, 72.5, 23.0],
            "Senegal": [-16.0, 13.5, -14.0, 15.0],
            "Shandong, China": [117.0, 35.0, 119.0, 37.0],
            "Northern Nigeria": [7.5, 10.0, 9.5, 12.0],
        },
    },
    "coconut": {
        "name": "Coconut",
        "category": "Oilseeds",
        "regions": {
            "Kerala, India": [75.5, 8.5, 77.0, 10.5],
            "Davao, Philippines": [125.5, 6.5, 126.5, 7.5],
            "Sulawesi, Indonesia": [121.0, -2.0, 123.0, 0.0],
            "Bahia, Brazil": [-39.5, -14.5, -38.0, -13.0],
            "Sri Lanka South": [80.0, 6.0, 81.0, 7.0],
        },
    },
    # BEVERAGES (coffee, cacao, tea already partially covered)
    "tea": {
        "name": "Tea",
        "category": "Beverages",
        "regions": {
            "Assam, India": [92.5, 26.0, 94.5, 27.5],
            "Fujian, China": [117.0, 25.5, 119.0, 27.0],
            "Central Province, Sri Lanka": [80.3, 6.8, 80.8, 7.5],
            "Kericho, Kenya": [35.0, -0.8, 35.6, 0.0],
            "Shizuoka, Japan": [137.8, 34.6, 138.5, 35.2],
        },
    },
    "tobacco": {
        "name": "Tobacco",
        "category": "Beverages",
        "regions": {
            "Virginia, USA": [-80.0, 36.5, -78.0, 38.0],
            "Yunnan, China": [102.0, 24.0, 104.0, 26.0],
            "Minas Gerais, Brazil": [-44.0, -19.5, -42.0, -17.5],
            "Malawi Central": [33.5, -14.5, 34.5, -13.0],
        },
    },
    # FRUITS
    "banana": {
        "name": "Banana",
        "category": "Fruits",
        "regions": {
            "Mindanao, Philippines": [125.0, 6.5, 126.5, 8.0],
            "Tamil Nadu, India": [77.0, 10.0, 78.5, 11.5],
            "Uraba, Colombia": [-76.8, 7.5, -76.0, 8.5],
            "Costa Rica Caribbean": [-83.5, 9.5, -82.5, 10.5],
            "Guangdong, China": [110.0, 21.0, 112.0, 23.0],
        },
    },
    "mango": {
        "name": "Mango",
        "category": "Fruits",
        "regions": {
            "UP, India": [80.0, 26.5, 82.0, 28.0],
            "Sinaloa, Mexico": [-108.0, 24.0, -106.5, 25.5],
            "Guimaras, Philippines": [122.4, 10.4, 122.8, 10.8],
            "Queensland, Australia": [145.5, -17.5, 147.0, -16.0],
            "Mali South": [-8.5, 11.0, -6.5, 13.0],
        },
    },
    "citrus": {
        "name": "Citrus",
        "category": "Fruits",
        "regions": {
            "Florida, USA": [-82.0, 27.0, -80.5, 28.5],
            "Sao Paulo, Brazil": [-50.0, -22.5, -48.0, -20.5],
            "Valencia, Spain": [-1.0, 38.5, 0.5, 40.0],
            "Guangxi, China": [107.0, 23.0, 109.0, 25.0],
            "Western Cape, South Africa": [18.5, -34.0, 19.5, -33.0],
        },
    },
    "apple": {
        "name": "Apple",
        "category": "Fruits",
        "regions": {
            "Washington, USA": [-121.0, 46.5, -119.5, 48.0],
            "Shaanxi, China": [107.5, 34.5, 109.5, 36.0],
            "South Tyrol, Italy": [10.5, 46.2, 11.5, 47.0],
            "Kashmir, India": [74.0, 33.5, 75.5, 34.5],
            "Hawke's Bay, NZ": [176.0, -40.0, 177.0, -39.0],
        },
    },
    "pineapple": {
        "name": "Pineapple",
        "category": "Fruits",
        "regions": {
            "Costa Rica Pacific": [-84.5, 9.0, -83.5, 10.0],
            "Lampung, Indonesia": [104.5, -5.5, 105.5, -4.5],
            "Bahia, Brazil": [-40.0, -13.5, -38.5, -12.0],
            "Rayong, Thailand": [101.0, 12.5, 102.0, 13.5],
        },
    },
    # SUGAR & FIBER (sugarcane already covered)
    "sugarbeet": {
        "name": "Sugar Beet",
        "category": "Sugar & Fiber",
        "regions": {
            "Picardy, France": [2.0, 49.0, 3.5, 50.0],
            "Minnesota, USA": [-97.0, 47.0, -95.0, 48.5],
            "Bavaria, Germany": [10.0, 48.5, 12.0, 49.5],
            "Krasnodar, Russia": [38.0, 44.5, 40.0, 46.0],
        },
    },
    "cotton": {
        "name": "Cotton",
        "category": "Sugar & Fiber",
        "regions": {
            "Texas, USA": [-101.0, 31.5, -98.0, 34.0],
            "Gujarat, India": [71.0, 22.0, 73.0, 24.0],
            "Xinjiang, China": [80.0, 39.0, 82.0, 41.0],
            "Mato Grosso, Brazil": [-56.0, -14.0, -53.0, -12.0],
            "Gezira, Sudan": [32.5, 13.5, 34.0, 15.0],
        },
    },
    # ROOTS & TUBERS
    "potato": {
        "name": "Potato",
        "category": "Roots & Tubers",
        "regions": {
            "Idaho, USA": [-115.0, 42.5, -113.0, 44.0],
            "UP, India": [79.0, 26.5, 81.0, 28.0],
            "Inner Mongolia, China": [111.0, 41.0, 113.0, 43.0],
            "Lower Saxony, Germany": [9.0, 52.0, 11.0, 53.0],
            "Andes, Peru": [-76.0, -13.0, -74.5, -11.5],
        },
    },
    "cassava": {
        "name": "Cassava",
        "category": "Roots & Tubers",
        "regions": {
            "Nakhon Ratchasima, Thailand": [101.5, 14.0, 103.0, 15.5],
            "Ogun, Nigeria": [3.0, 6.5, 4.5, 7.5],
            "Parana, Brazil": [-52.5, -24.0, -50.5, -22.5],
            "DR Congo": [19.0, -5.0, 21.0, -3.0],
            "Mozambique": [34.0, -16.0, 36.0, -14.0],
        },
    },
    "sweet-potato": {
        "name": "Sweet Potato",
        "category": "Roots & Tubers",
        "regions": {
            "Sichuan, China": [103.0, 29.0, 105.0, 31.0],
            "North Carolina, USA": [-79.0, 34.5, -77.0, 36.0],
            "Uganda": [32.0, 0.5, 33.5, 2.0],
            "Papua New Guinea": [143.0, -6.5, 145.0, -5.0],
        },
    },
    # PULSES
    "beans": {
        "name": "Beans",
        "category": "Pulses",
        "regions": {
            "Michigan, USA": [-86.0, 42.5, -84.0, 44.0],
            "Parana, Brazil": [-52.0, -24.0, -50.0, -22.5],
            "Rwanda": [29.0, -2.5, 30.0, -1.5],
            "Jalisco, Mexico": [-104.0, 20.5, -102.0, 22.0],
            "Karnataka, India": [75.5, 14.0, 77.0, 16.0],
        },
    },
    "lentils": {
        "name": "Lentils",
        "category": "Pulses",
        "regions": {
            "Saskatchewan, Canada": [-107.0, 50.0, -104.0, 52.0],
            "MP, India": [77.0, 23.0, 79.0, 25.0],
            "Southeast Turkey": [39.0, 37.0, 41.0, 38.5],
            "South Australia": [138.0, -34.5, 140.0, -33.0],
        },
    },
    "chickpea": {
        "name": "Chickpea",
        "category": "Pulses",
        "regions": {
            "MP, India": [77.0, 22.5, 79.5, 24.5],
            "Montana, USA": [-110.0, 47.0, -107.5, 48.5],
            "Konya, Turkey": [32.0, 37.0, 34.0, 38.5],
            "Cordoba, Spain": [-5.0, 37.5, -4.0, 38.5],
        },
    },
    # NUTS
    "cashew": {
        "name": "Cashew",
        "category": "Nuts",
        "regions": {
            "Kerala, India": [75.5, 10.5, 76.5, 12.0],
            "Ivory Coast": [-6.0, 7.0, -4.5, 8.5],
            "Nampula, Mozambique": [39.0, -16.0, 41.0, -14.5],
            "Ceara, Brazil": [-39.5, -5.0, -38.0, -3.5],
            "Binh Phuoc, Vietnam": [106.5, 11.5, 107.5, 12.5],
        },
    },
    "almond": {
        "name": "Almond",
        "category": "Nuts",
        "regions": {
            "California, USA": [-120.5, 36.0, -119.0, 37.5],
            "Andalucia, Spain": [-3.5, 36.5, -2.0, 37.5],
            "Puglia, Italy": [16.5, 40.5, 18.0, 41.5],
            "South Australia": [138.0, -35.0, 140.0, -33.5],
        },
    },
    "pistachio": {
        "name": "Pistachio",
        "category": "Nuts",
        "regions": {
            "California, USA": [-120.0, 35.5, -118.5, 37.0],
            "Kerman, Iran": [56.5, 29.5, 58.0, 31.0],
            "Gaziantep, Turkey": [36.5, 36.5, 38.0, 37.5],
        },
    },
    # SPICES
    "vanilla": {
        "name": "Vanilla",
        "category": "Spices",
        "regions": {
            "SAVA, Madagascar": [49.5, -14.5, 50.5, -13.5],
            "Veracruz, Mexico": [-97.0, 19.5, -96.0, 20.5],
            "Uganda": [31.0, 0.0, 32.0, 1.5],
            "Tahiti, French Polynesia": [-149.6, -17.8, -149.3, -17.5],
        },
    },
    "pepper": {
        "name": "Black Pepper",
        "category": "Spices",
        "regions": {
            "Kerala, India": [75.5, 10.0, 77.0, 12.0],
            "Lampung, Indonesia": [104.5, -5.5, 106.0, -4.5],
            "Gia Lai, Vietnam": [107.5, 13.5, 108.5, 14.5],
            "Bahia, Brazil": [-40.0, -15.0, -39.0, -13.5],
        },
    },
    # OTHER
    "olive": {
        "name": "Olive",
        "category": "Other",
        "regions": {
            "Andalucia, Spain": [-4.5, 37.0, -3.0, 38.5],
            "Puglia, Italy": [16.5, 40.0, 18.5, 41.5],
            "Crete, Greece": [24.0, 34.8, 26.0, 35.5],
            "Sfax, Tunisia": [10.0, 34.0, 11.0, 35.0],
            "Mendoza, Argentina": [-69.0, -33.5, -67.5, -32.0],
        },
    },
    "rubber": {
        "name": "Rubber",
        "category": "Other",
        "regions": {
            "South Thailand": [98.5, 7.5, 100.0, 9.0],
            "Sumatra, Indonesia": [103.5, -2.5, 105.0, -1.0],
            "Kerala, India": [75.5, 9.0, 77.0, 10.5],
            "Ivory Coast": [-6.0, 5.5, -4.5, 7.0],
        },
    },
}


def sample_region(region_name, bbox, n_points=3, seed=42):
    """Sample cropland pixels from a bounding box."""
    west, south, east, north = bbox
    geom = ee.Geometry.Rectangle([west, south, east, north])

    try:
        points = CROPLAND.sample(
            region=geom,
            scale=1000,
            numPixels=n_points,
            seed=seed,
            geometries=True,
        )
        feats = points.getInfo()["features"]
        results = []
        for f in feats:
            lng, lat = f["geometry"]["coordinates"]
            results.append({
                "lat": round(lat, 4),
                "lng": round(lng, 4),
                "label": region_name,
            })
        return results
    except Exception as e:
        print(f"    WARNING: Failed to sample {region_name}: {e}")
        return []


def main():
    import argparse
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", default="data/layer_references.json")
    parser.add_argument("--layer", help="Only generate for this crop")
    parser.add_argument("--points-per-region", type=int, default=3)
    args = parser.parse_args()

    layers_to_process = LAYER_REGIONS
    if args.crop:
        if args.crop not in LAYER_REGIONS:
            print(f"Unknown crop: {args.crop}")
            sys.exit(1)
        layers_to_process = {args.crop: LAYER_REGIONS[args.crop]}

    # Load existing references if any
    existing = {}
    if os.path.exists(args.output):
        with open(args.output) as f:
            existing = json.load(f)

    all_crops = dict(existing)
    total_farms = 0
    t0 = time.time()

    for crop_id, crop_def in layers_to_process.items():
        print(f"\n{'='*50}")
        print(f"{crop_def['name']} ({crop_id})")
        print(f"{'='*50}")

        farms = []
        for region_name, bbox in crop_def["regions"].items():
            print(f"  Sampling: {region_name}...")
            points = sample_region(region_name, bbox, n_points=args.points_per_region)
            farms.extend(points)
            print(f"    Got {len(points)} points")
            time.sleep(0.5)  # Rate limit

        print(f"  Total: {len(farms)} reference farms")
        total_farms += len(farms)

        all_crops[crop_id] = {
            "name": crop_def["name"],
            "category": crop_def["category"],
            "farms": farms,
        }

    # Save
    os.makedirs(os.path.dirname(args.output), exist_ok=True)
    with open(args.output, "w") as f:
        json.dump(all_crops, f, indent=2)

    elapsed = time.time() - t0
    print(f"\n{'='*50}")
    print(f"Done! {len(all_crops)} crops, {total_farms} total farms")
    print(f"Saved to {args.output}")
    print(f"Time: {elapsed:.0f}s")
    print(f"{'='*50}")


if __name__ == "__main__":
    main()
