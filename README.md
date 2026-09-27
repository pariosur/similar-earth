# Similar Earth

[![CI](https://github.com/pariosur/similar-earth/actions/workflows/ci.yml/badge.svg)](https://github.com/pariosur/similar-earth/actions/workflows/ci.yml)

**Discover similar places anywhere on Earth using satellite data.**

**[Live Demo: similar.earth](https://similar.earth)**

![Similar Earth](frontend/public/og-image.png)

Similar Earth finds everywhere on Earth that looks like a place you know. Describe it ("places like Tuscany, but warmer, outside Europe", "mangrove coasts in Africa"), browse curated maps, or drop your own pins. The system compares satellite fingerprints against every land pixel on the planet and shows you what matches.

Built on [Google AlphaEarth](https://developers.google.com/earth-engine/datasets/catalog/GOOGLE_SATELLITE_EMBEDDING_V1_ANNUAL) satellite embeddings (2025) at 10m resolution. Open source.

## How it works

1. **Describe or pin a place.** A named place, a kind of place (any featured map's theme, such as mangroves, glaciers or coffee), or pins on the map.
2. **We scan the planet.** Your reference gets compared against every land pixel on Earth using 64-dimensional satellite embeddings.
3. **See what matches.** Searches return up to 20 matches spread across regions, filterable by country, continent and annual temperature. Maps show a global heatmap from yellow (moderate) to red (strong). Zoom in for 10m field-level detail.

A place lights up if it looks like any of your pins. The engine uses MAX similarity, so diverse reference points (highland + coastal farms, for example) both contribute.

## Featured maps

Ships with 14 featured maps across 4 categories (48 maps in total):

| Category | Maps |
|----------|------|
| Agriculture | Hass Avocado, Specialty Coffee, Wine Grapes, Cacao, Macadamia, Sugarcane |
| Energy | Solar Farms |
| Natural Ecosystems | Mangroves, Tropical Dry Forest, Glaciers, Tropical Coastline, Desert Regions, Volcanic Terrain |
| Climate Risk | Wildfire Zones |

Each map uses verified reference coordinates. Featured maps have pre-rendered tiles for instant loading.

## Quick start

### Prerequisites

- [Go](https://go.dev/) 1.22+
- [Node.js](https://nodejs.org/) 20+
- [Python](https://python.org/) 3.11+
- [Docker](https://docker.com/) (for PostgreSQL)
- [Google Earth Engine](https://earthengine.google.com/) account (for data pipeline and 10m refinement)

### 1. Clone and install

```bash
git clone https://github.com/pariosur/similar-earth.git
cd similar-earth
cp .env.example .env

# Frontend
cd frontend && npm install && cd ..

# Python
cd python && pip install -r requirements.txt && cd ..
```

### 2. Generate the embedding grid

Requires a Google Earth Engine account.

```bash
# Export AlphaEarth embeddings from Earth Engine (runs 2-4 hours on GEE servers)
python scripts/export_global_embeddings.py

# Download the GeoTIFF tiles from Google Drive, then convert:
python scripts/build_grid_bin.py ~/Downloads/alphaearth_embeddings_2km_2025*.tif
```

Creates `data/grid.bin` (~8.5 GB): 64-dimensional int8 embeddings for every 2km land pixel on Earth.

### 3. Precompute featured maps

```bash
# Compute similarity scores for all featured maps
python scripts/precompute_layers.py

# Pre-render PNG tiles for instant loading
python scripts/prerender_tiles.py --max-zoom 8
```

### 4. Place search (optional)

```bash
# Align annual-mean temperature (e.g. ERA5-Land) and Natural Earth countries to the grid
python scripts/build_search_metadata.py data/grid.bin temperature.tif ne_admin0.gpkg data/search_metadata.bin
```

Set `OPENAI_API_KEY` (prompt parsing) and `GEONAMES_USERNAME` (place lookup) to enable the Find places tab. For offline result names, put `cities5000.zip`, `admin1CodesASCII.txt` and `countryInfo.txt` from [GeoNames](https://download.geonames.org/export/dump/) in `data/`.

### 5. Run

```bash
make dev
```

Starts PostgreSQL (Docker), Go API server, Python GEE service, and Vite frontend. Open http://localhost:3000.

## Architecture

```
Frontend (React + MapLibre GL)
    |
Go API Server (Fiber)
    |-- Pre-rendered tiles (static PNGs, zoom 2-8)
    |-- Similarity engine (int8 dot product, parallel)
    |-- Grid (8.5 GB, memory-mapped)
    |-- Place search (OpenAI prompt parser + filtered scan)
    |-- COG fetcher (10m on-demand via Earth Engine)
    |
Python GEE Service (FastAPI)
    |-- Earth Engine computePixels (10m refinement)
    |-- Terrain, landcover, biophysical data
    |
PostgreSQL
    |-- Maps, queries, event logs
```

### Data pipeline

```
Earth Engine (AlphaEarth V1 Annual)
  -> export_global_embeddings.py -> 64-band GeoTIFF (2km)
  -> build_grid_bin.py -> grid.bin (8.5 GB, int8)
  -> precompute_layers.py -> scores_*.bin (665 MB per map)
  -> build_search_metadata.py -> search_metadata.bin (country + temperature per pixel)
  -> prerender_tiles.py -> tiles/{slug}/{z}/{x}/{y}.png
```

### Resolution: 2km vs 10m

The app operates at two resolution levels:

- **2km (Global scan):** Pre-computed. Every land pixel on Earth is compared against your reference pins using the 8.5 GB memory-mapped embedding grid. Results are instant because the similarity scores are already calculated. This is what you see at zoom levels 2-9.
- **10m (Detail scan):** On-demand. When you zoom past level 10 and click "10M", the app fetches 10-meter AlphaEarth embeddings from Google Earth Engine in real time. Each 256x256 tile takes ~3 seconds on first load. This reveals field-level patterns invisible at 2km.

### Tile caching

HD tiles are expensive (each one calls Earth Engine), so they're cached aggressively:

1. **In-memory cache:** up to 10,000 tiles in an LRU cache. Instant on repeat views.
2. **Disk cache:** HD tiles are saved to `tiles/{slug}/{z}/{x}/{y}.png` on first compute. Survives server restarts.
3. **Pre-rendered tiles:** Featured maps have tiles pre-rendered at zoom 2-8 via `scripts/prerender_tiles.py`. Served as static PNGs by nginx, no computation needed.

First-time HD views take ~3s per tile, but everything after that is instant.

## Adding a new map

1. Add reference coordinates to `data/layer_references.json`:

```json
{
  "my-map": {
    "name": "My Map",
    "category": "Conservation",
    "featured": true,
    "pins": [
      {"lat": 12.34, "lng": 56.78, "label": "Location Name, Country"}
    ]
  }
}
```

2. Precompute and render:

```bash
python scripts/precompute_layers.py --layer my-map
python scripts/prerender_tiles.py --layer my-map
```

3. Restart the server. New map appears in the gallery.

## Contributing

Contributions welcome, especially:

- Verified reference coordinates for new maps
- Bug fixes and performance improvements
- New categories and use cases

When adding coordinates, verify each one in Google Maps satellite view. Quality over quantity.

## Tech stack

- **Backend:** Go (Fiber), PostgreSQL
- **Frontend:** React, TypeScript, Vite, Tailwind CSS, MapLibre GL JS
- **Data:** Google AlphaEarth embeddings, Earth Engine, ESA WorldCover
- **Basemap:** CARTO Dark Matter (free, no API key)

## Attribution

The AlphaEarth Foundations Satellite Embedding dataset is produced by Google and Google DeepMind, licensed under [CC-BY 4.0](https://creativecommons.org/licenses/by/4.0/). Annual mean temperature (1991–2020) from Copernicus ERA5-Land; place names from [GeoNames](https://www.geonames.org/) (CC-BY 4.0); country borders from Natural Earth.

## License

MIT. See [LICENSE](LICENSE).
