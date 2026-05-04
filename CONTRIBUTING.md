# Contributing to Similar Earth

Thanks for your interest in contributing! Here's how to get started.

## Ways to contribute

- **Reference pins for new maps** — the highest-impact contribution. Add verified locations for new similarity maps (e.g., coral reefs, rice paddies, wind farms).
- **Bug fixes** — found something broken? Open an issue or submit a PR.
- **Performance improvements** — the similarity engine processes 40M pixels. Faster is always better.
- **New features** — check the issues tab for ideas, or propose your own.

## Adding a new map

1. Add reference coordinates to `data/layer_references.json`:

```json
{
  "my-map": {
    "name": "My Map",
    "description": "Short description of what these locations have in common.",
    "category": "Natural Ecosystems",
    "featured": true,
    "pins": [
      {"lat": 12.34, "lng": 56.78, "label": "Location Name, Country"}
    ]
  }
}
```

2. Verify each pin in Google Maps satellite view. Quality over quantity — 10 well-placed pins beat 50 random ones.

3. Precompute scores and tiles:
```bash
python scripts/precompute_layers.py --layer my-map
python scripts/prerender_tiles.py --crop my-map --max-zoom 8
```

4. Submit a PR with the updated JSON and a brief description of why these locations are interesting.

## Development setup

```bash
git clone https://github.com/pariosur/similar-earth.git
cd similar-earth
cp .env.example .env

# Frontend
cd frontend && npm install && cd ..

# Start everything
make dev
```

This starts PostgreSQL (Docker), Go API server, Python GEE service, and Vite frontend at http://localhost:3000.

## Code structure

```
frontend/src/
  components/map/     — map overlays (markers, legend, search, drawer)
  components/sidebar/ — gallery, create flow, point inspection
  hooks/              — shared state hooks (useDiscoveries, useMapInit, etc.)
  stores/             — Zustand store (queryStore, themeStore)
  api/                — API client + types

internal/
  api/                — Go HTTP handlers + server
  similarity/         — embedding comparison engine
  tiles/              — tile renderer + cache
  grid/               — 8.5GB grid loader
  db/                 — PostgreSQL queries + migrations

data/
  layer_references.json — all layer metadata + baked top matches
  grid.bin              — global embedding grid (not in repo, generated)
```

## PR guidelines

- Keep changes focused — one feature or fix per PR
- Test on both desktop and mobile (Chrome DevTools device mode is fine)
- Frontend changes: run `npx vite build` to verify no errors
- Backend changes: run `go build ./cmd/server/` to verify compilation
- Don't commit `.env`, `data/grid.bin`, or `data/scores_*.bin`

## Questions?

Open an issue or reach out on GitHub. Happy mapping!
