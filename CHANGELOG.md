# Changelog

## v0.2.0 — 2026-04-29

Major UX overhaul based on external developer reviews.

### New featured maps
- Volcanic Terrain (10 reference pins across 6 continents)
- Desert Regions (10 pins from Sahara to Atacama)
- Wildfire Zones (12 pins, California to Yakutia)
- 14 featured maps total across 4 categories

### UX improvements (48 items shipped)
- "Reference Pins" label on featured maps (vs "Your Pins" in Create only)
- Dynamic intro card: "Where else on Earth looks like Hass Avocado?"
- Result detail drawer with coordinates, matched pin, Zoom/Copy/Google Maps
- Map-specific descriptions for all featured maps
- Category-specific disclaimers (agriculture, energy, climate risk)
- Context-aware How It Works (shows active map name + pin count)
- Always-visible one-liner explaining the mechanism
- Score tooltips explaining similarity percentages
- Data source attribution (Google AlphaEarth · 2km/10m · 2025)
- Attribution footer (Built by Pablo Rios · Open Source)
- Dynamic browser tab title per active map

### Create flow
- Two-phase mobile create: floating pin pill → compact form
- "Preview Similarity" replaces "Build Map" (publishing is optional)
- Unlisted/Public visibility selector with privacy notice
- Auto reverse-geocode dropped pins (friendly names)
- Undo last pin button on mobile
- Search bar adds pins directly in Create mode
- "Start Over" confirmation dialog
- Tighter publish validation (min title, pins, category)
- Duplicate pin prevention (~1km minimum distance)

### Mobile
- Horizontal map chip strip for one-tap map switching
- Discovery/Reference Pins toggle in collapsed sheet
- Full-width "Scan at 10m resolution" CTA
- Tap map to collapse expanded sheet
- Safe-area padding for iOS home indicator
- Disabled browser page zoom (prevents accidental zoom)
- Responsive CSS variables for all layout constants

### Navigation & markers
- Pin ↔ Discovery selection with highlight + related markers
- Selection banner showing the connection ("similar to X")
- MapLegend with marker key + similarity gradient + opacity slider
- Click map to clear selection, Escape key shortcut
- Discovery markers (crimson circles) distinct from reference pins (gold squares)
- Heatmap renders below map labels (place names visible)
- Crossfade between map layers (no blank flash on switch)
- Smooth zoom-out to global view on map switch
- Auto-expand category containing active map
- Map count in category headers

### Performance & caching
- Backend top-matches cache (in-memory, per-layer)
- Server-side geocoding with cache (instant for repeat requests)
- Frontend discovery cache (shared hook, in-flight dedupe)
- HD tile auto-restore on zoom back in
- GEE credit guardrails (60 tiles/min per IP, 5000/day global)

### Infrastructure
- Self-hosted Plausible analytics
- Renamed "crop" → "layer" across entire codebase
- Security: path traversal fix, CORS restriction, rate limiting, input validation, security headers
- Error states on all API failure paths

### Export & sharing
- Copy as CSV / Copy GeoJSON for discovery results
- Copy link with "Copied!" feedback
- Share button (clipboard on desktop, native share on mobile)

## v0.1.0 — 2026-04-08

Initial public release.

### Features
- Global satellite similarity search using Google AlphaEarth embeddings (2km resolution)
- 10m field-level detail via on-demand Earth Engine COG refinement
- 11 featured maps across 5 categories (agriculture, energy, conservation, climate, tourism)
- Create and publish custom similarity maps
- Top discoveries with reverse geocoding
- Point inspection with terrain, climate, and land cover data
- Share maps via URL with `?s=slug` parameters

### UI
- Dark/light theme with always-dark sidebar
- Mobile-responsive bottom sheet layout
- Context chip showing active map and pin count
- Copy/share button (clipboard on desktop, native share on mobile)
- "How it works" onboarding page

### Infrastructure
- Go (Fiber) API server with in-memory 8.5GB embedding grid
- React + TypeScript + Vite + Tailwind CSS v4 frontend
- MapLibre GL JS with CARTO basemaps
- PostgreSQL for maps and queries
- Self-hosted Plausible analytics
- Nginx reverse proxy with Let's Encrypt SSL

### Security
- Path traversal protection on all file-serving endpoints
- CORS restricted to production domain
- Rate limiting on query and map creation endpoints
- Input validation on all user-submitted data
- Security headers (HSTS, X-Frame-Options, X-Content-Type-Options)
