package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/pariosur/tierraai/internal/db"
	"github.com/pariosur/tierraai/internal/similarity"
)

// handleQuery processes POST /api/query.
func (s *Server) handleQuery(c *fiber.Ctx) error {
	var req QueryRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid JSON body: " + err.Error(),
		})
	}

	// Validate pin count.
	if len(req.Pins) == 0 || len(req.Pins) > 50 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "pins count must be between 1 and 50",
		})
	}

	// Validate each pin.
	for i, pin := range req.Pins {
		if pin.Lat < -90 || pin.Lat > 90 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fmt.Sprintf("pin %d: lat must be between -90 and 90", i),
			})
		}
		if pin.Lng < -180 || pin.Lng > 180 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fmt.Sprintf("pin %d: lng must be between -180 and 180", i),
			})
		}
	}

	// Create query ID.
	queryID := uuid.New()

	// Build similarity pins.
	pins := make([]similarity.Pin, len(req.Pins))
	for i, p := range req.Pins {
		pins[i] = similarity.Pin{
			Lat:   p.Lat,
			Lng:   p.Lng,
			Label: p.Label,
		}
	}

	query := &similarity.Query{
		ID:   queryID,
		Pins: pins,
	}

	// Persist query as pending.
	pinsJSON, _ := json.Marshal(req.Pins)
	ctx := c.Context()
	if err := s.db.CreateQuery(ctx, queryID, pinsJSON, len(pins)); err != nil {
		log.Printf("ERROR: CreateQuery: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to persist query",
		})
	}

	// Mark as computing.
	s.computingMu.Lock()
	s.computing[queryID] = true
	s.computingMu.Unlock()

	// Log event.
	eventPayload, _ := json.Marshal(fiber.Map{
		"pin_count": len(pins),
		"intent":    req.Intent,
	})
	_ = s.db.LogEvent(ctx, "query_created", &queryID, eventPayload)

	// Compute similarity in background goroutine.
	go func() {
		start := time.Now()
		result, err := s.engine.Compute(query)
		computeMs := time.Since(start).Milliseconds()

		if err != nil {
			log.Printf("ERROR: Compute query %s: %v", queryID, err)
			_ = s.db.FailQuery(ctx, queryID, err.Error())
		} else {
			// Store result in memory.
			s.queriesMu.Lock()
			s.queries[queryID] = result
			s.queriesMu.Unlock()

			_ = s.db.CompleteQuery(ctx, queryID, int(computeMs))
			log.Printf("Query %s completed in %dms (%d pins)", queryID, computeMs, len(pins))
		}

		// Clear computing flag.
		s.computingMu.Lock()
		delete(s.computing, queryID)
		s.computingMu.Unlock()
	}()

	// Build tile URL template.
	tileURL := fmt.Sprintf("/api/tiles/%s/{z}/{x}/{y}.png", queryID.String())

	return c.Status(fiber.StatusAccepted).JSON(QueryResponse{
		ID:       queryID.String(),
		Status:   "computing",
		TileURL:  tileURL,
		PinCount: len(pins),
	})
}

// handleLayers serves GET /api/layers — list available pre-computed layers with metadata.
func (s *Server) handleLayers(c *fiber.Ctx) error {
	layerIDs := s.precomputed.LayerIDs()

	type pinCoord struct {
		Lat   float64 `json:"lat"`
		Lng   float64 `json:"lng"`
		Label string  `json:"label,omitempty"`
	}
	type layerInfo struct {
		ID          string      `json:"id"`
		Name        string      `json:"name"`
		Description string      `json:"description,omitempty"`
		Category    string      `json:"category"`
		PinCount    int        `json:"pin_count"`
		Pins        []pinCoord `json:"pins"`
		TileURL     string      `json:"tile_url"`
		Featured    bool        `json:"featured"`
		HasTiles    bool        `json:"has_tiles"`
	}

	// Sort: featured first, then has_tiles, then alphabetical
	layers := make([]layerInfo, 0, len(layerIDs))
	for _, id := range layerIDs {
		result := s.precomputed.Get(id)
		name := id
		category := ""
		pinCount := 0
		featured := false
		var layerPins []pinCoord
		if result != nil {
			pinCount = len(result.RefEmbeddings)
		}
		description := ""
		if meta, ok := s.layerMeta[id]; ok {
			name = meta.Name
			description = meta.Description
			category = meta.Category
			featured = meta.Featured
			layerPins = make([]pinCoord, len(meta.Pins))
			for i, f := range meta.Pins {
				layerPins[i] = pinCoord{Lat: f.Lat, Lng: f.Lng, Label: f.Label}
			}
		}

		// Check if pre-rendered tiles exist on disk
		hasTiles := false
		tilePath := fmt.Sprintf("./tiles/%s", id)
		if info, err := os.Stat(tilePath); err == nil && info.IsDir() {
			hasTiles = true
		}

		layers = append(layers, layerInfo{
			ID:          id,
			Name:        name,
			Description: description,
			Category:    category,
			PinCount: pinCount,
			Pins:      layerPins,
			TileURL:   fmt.Sprintf("/tiles/%s/{z}/{x}/{y}.png", id),
			Featured:  featured,
			HasTiles:  hasTiles,
		})
	}

	// Sort: featured first, then has_tiles, then alphabetical
	sort.Slice(layers, func(i, j int) bool {
		if layers[i].Featured != layers[j].Featured {
			return layers[i].Featured
		}
		if layers[i].HasTiles != layers[j].HasTiles {
			return layers[i].HasTiles
		}
		return layers[i].Name < layers[j].Name
	})

	return c.JSON(fiber.Map{"layers": layers})
}

// Global daily COG request counter to prevent GEE credit abuse.
var (
	cogDailyCount atomic.Int64
	cogDailyDate  atomic.Int64 // unix day number
	cogDailyLimit int64 = 5000 // max COG requests per day
)

func cogDailyAllowed() bool {
	today := time.Now().Unix() / 86400
	if cogDailyDate.Load() != today {
		cogDailyDate.Store(today)
		cogDailyCount.Store(0)
	}
	return cogDailyCount.Add(1) <= cogDailyLimit
}

// handleLayerTile serves GET /api/layers/:layerId/tiles/:z/:x/:y — pre-computed tiles.
func (s *Server) handleLayerTile(c *fiber.Ctx) error {
	layerID := c.Params("layerId")
	if !validSlug(layerID) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid layer ID"})
	}
	result := s.precomputed.Get(layerID)
	if result == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "layer not found"})
	}

	z, err := strconv.Atoi(c.Params("z"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid z"})
	}
	x, err := strconv.Atoi(c.Params("x"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid x"})
	}
	yStr := strings.TrimSuffix(c.Params("y"), ".png")
	y, err := strconv.Atoi(yStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid y"})
	}

	cacheKey := fmt.Sprintf("layer_%s/%d/%d/%d", layerID, z, x, y)
	if data, hit := s.tileCache.Get(cacheKey); hit {
		c.Set("Content-Type", "image/png")
		c.Set("Cache-Control", "public, max-age=86400")
		c.Set("X-Cache", "HIT")
		return c.Send(data)
	}

	// At high zoom, try 10m COG refinement.
	if z >= s.cogZoomThreshold && s.cogClient != nil && len(result.RefEmbeddings) > 0 {
		// Check if this HD tile was previously saved to disk.
		hdTilePath := fmt.Sprintf("./tiles/%s/%d/%d/%d.png", layerID, z, x, y)
		if diskData, err := os.ReadFile(hdTilePath); err == nil {
			s.tileCache.Put(cacheKey, diskData)
			c.Set("Content-Type", "image/png")
			c.Set("Cache-Control", "public, max-age=86400")
			c.Set("X-Cache", "DISK")
			return c.Send(diskData)
		}

		// Check daily COG limit to prevent GEE credit abuse
		if !cogDailyAllowed() {
			log.Printf("WARN: Daily COG limit reached (%d), falling back to 2km for layer=%s z=%d", cogDailyLimit, layerID, z)
		} else {
			scores, err := s.cogClient.FetchTileScores(c.Context(), z, x, y, 2025, result.RefEmbeddings)
			if err != nil {
				log.Printf("WARN: COG fetch layer=%s z=%d x=%d y=%d: %v, falling back to 2km", layerID, z, x, y, err)
			} else if scores != nil {
				data, err := s.renderer.RenderTileFromScores(scores)
				if err != nil {
					log.Printf("ERROR: RenderTileFromScores layer=%s z=%d: %v", layerID, z, err)
				} else {
					s.tileCache.Put(cacheKey, data)

					// Save to disk for persistence across restarts.
					go func() {
						dir := fmt.Sprintf("./tiles/%s/%d/%d", layerID, z, x)
						if err := os.MkdirAll(dir, 0755); err == nil {
							_ = os.WriteFile(hdTilePath, data, 0644)
						}
					}()

					c.Set("Content-Type", "image/png")
					c.Set("Cache-Control", "public, max-age=86400")
					c.Set("X-Cache", "COG")
					return c.Send(data)
				}
			}
		}
	}

	// Fallback: render from 2km grid.
	if err := result.EnsureLoaded(); err != nil {
		log.Printf("ERROR: EnsureLoaded for %s: %v", layerID, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to load layer data"})
	}

	data, err := s.renderer.RenderTile(result, z, x, y, s.grid)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "tile render failed"})
	}

	s.tileCache.Put(cacheKey, data)
	c.Set("Content-Type", "image/png")
	c.Set("Cache-Control", "public, max-age=86400")
	c.Set("X-Cache", "MISS")
	return c.Send(data)
}

// ============================================================================
// Top matches endpoints
// ============================================================================

// Cache for layer top matches (deterministic for featured maps).
var (
	topMatchesCache   = make(map[string]fiber.Map)
	topMatchesCacheMu sync.RWMutex
	geocodeCache      sync.Map // lat,lng -> name
)

// reverseGeocode calls Nominatim to get a human-readable name for a lat/lng.
func reverseGeocode(lat, lng float64) string {
	key := fmt.Sprintf("%.4f,%.4f", lat, lng)
	if v, ok := geocodeCache.Load(key); ok {
		return v.(string)
	}
	fallback := fmt.Sprintf("%.2f, %.2f", lat, lng)

	client := &http.Client{Timeout: 5 * time.Second}
	u := fmt.Sprintf("https://nominatim.openstreetmap.org/reverse?format=json&lat=%f&lon=%f&zoom=12&accept-language=en", lat, lng)
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "SimilarEarth/1.0")
	resp, err := client.Do(req)
	if err != nil {
		geocodeCache.Store(key, fallback)
		return fallback
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var data struct {
		Address struct {
			City         string `json:"city"`
			Town         string `json:"town"`
			Village      string `json:"village"`
			Municipality string `json:"municipality"`
			County       string `json:"county"`
			State        string `json:"state"`
			Region       string `json:"region"`
			Country      string `json:"country"`
		} `json:"address"`
		DisplayName string `json:"display_name"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		geocodeCache.Store(key, fallback)
		return fallback
	}

	a := data.Address
	local := firstNonEmpty(a.City, a.Town, a.Village, a.Municipality, a.County)
	region := firstNonEmpty(a.State, a.Region)
	country := a.Country

	var name string
	if local != "" && country != "" {
		name = local + ", " + country
	} else if region != "" && country != "" {
		name = region + ", " + country
	} else if country != "" {
		name = country
	} else if data.DisplayName != "" {
		parts := strings.SplitN(data.DisplayName, ",", 3)
		if len(parts) >= 2 {
			name = strings.TrimSpace(parts[0]) + "," + strings.TrimSpace(parts[1])
		} else {
			name = data.DisplayName
		}
	} else {
		name = fallback
	}

	geocodeCache.Store(key, name)
	return name
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// Suppress unused import warning

// handleLayerTopMatches serves GET /api/layers/:layerId/top
func (s *Server) handleLayerTopMatches(c *fiber.Ctx) error {
	layerID := c.Params("layerId")
	count := c.QueryInt("count", 20)
	excludeKm := c.QueryFloat("exclude_km", 100.0)
	cacheKey := fmt.Sprintf("%s_%d_%.0f", layerID, count, excludeKm)

	// Serve baked top matches from layer metadata (instant, no computation)
	if count == 10 && excludeKm == 100.0 {
		if meta, ok := s.layerMeta[layerID]; ok && len(meta.TopMatches) > 0 {
			matches := make([]fiber.Map, len(meta.TopMatches))
			matchNames := make([]string, len(meta.TopMatches))
			var pinLabels []string
			for _, p := range meta.Pins {
				pinLabels = append(pinLabels, p.Label)
			}
			for i, m := range meta.TopMatches {
				matches[i] = fiber.Map{"lat": m.Lat, "lng": m.Lng, "score": m.Score, "best_pin_index": m.BestPinIndex}
				matchNames[i] = m.Name
			}
			return c.JSON(fiber.Map{"matches": matches, "pin_labels": pinLabels, "match_names": matchNames})
		}
	}

	// Check in-memory cache
	topMatchesCacheMu.RLock()
	if cached, ok := topMatchesCache[cacheKey]; ok {
		topMatchesCacheMu.RUnlock()
		return c.JSON(cached)
	}
	topMatchesCacheMu.RUnlock()

	result := s.precomputed.Get(layerID)
	if result == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "layer not found"})
	}

	if err := result.EnsureLoaded(); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to load scores"})
	}

	// Get pins from layer metadata
	var pins []similarity.Pin
	if meta, ok := s.layerMeta[layerID]; ok {
		for _, f := range meta.Pins {
			pins = append(pins, similarity.Pin{Lat: f.Lat, Lng: f.Lng})
		}
	}

	matches := similarity.FindTopMatches(result, s.grid, pins, count, excludeKm)

	// Build pin labels for "similar to" display
	var pinLabels []string
	if meta, ok := s.layerMeta[layerID]; ok {
		for _, f := range meta.Pins {
			pinLabels = append(pinLabels, f.Label)
		}
	}

	// Geocode match locations server-side (with 1s delay for Nominatim rate limit)
	matchNames := make([]string, len(matches))
	for i, m := range matches {
		matchNames[i] = reverseGeocode(m.Lat, m.Lng)
		if i < len(matches)-1 {
			time.Sleep(1100 * time.Millisecond)
		}
	}

	resp := fiber.Map{"matches": matches, "pin_labels": pinLabels, "match_names": matchNames}

	// Cache the result (includes geocoded names)
	topMatchesCacheMu.Lock()
	topMatchesCache[cacheKey] = resp
	topMatchesCacheMu.Unlock()

	return c.JSON(resp)
}

// handleQueryTopMatches serves GET /api/query/:id/top
func (s *Server) handleQueryTopMatches(c *fiber.Ctx) error {
	queryID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid query id"})
	}

	s.queriesMu.RLock()
	result, ok := s.queries[queryID]
	s.queriesMu.RUnlock()
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "query not found"})
	}

	// Reconstruct pins from the query's stored data
	q, err := s.db.GetQuery(c.Context(), queryID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to get query"})
	}

	var pinDtos []PinDTO
	_ = json.Unmarshal(q.Pins, &pinDtos)
	var pins []similarity.Pin
	for _, p := range pinDtos {
		pins = append(pins, similarity.Pin{Lat: p.Lat, Lng: p.Lng})
	}

	count := c.QueryInt("count", 20)
	excludeKm := c.QueryFloat("exclude_km", 100.0)

	matches := similarity.FindTopMatches(result, s.grid, pins, count, excludeKm)

	var pinLabels []string
	for _, p := range pinDtos {
		pinLabels = append(pinLabels, p.Label)
	}

	return c.JSON(fiber.Map{"matches": matches, "pin_labels": pinLabels})
}

// ============================================================================
// Maps endpoints
// ============================================================================

// handleCreateMap serves POST /api/maps — publish a new map.
func (s *Server) handleCreateMap(c *fiber.Ctx) error {
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Category    string `json:"category"`
		Pins        []struct {
			Lat   float64 `json:"lat"`
			Lng   float64 `json:"lng"`
			Label string  `json:"label,omitempty"`
		} `json:"pins"`
		Author string `json:"author"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON"})
	}
	if req.Title == "" || len(req.Title) > 255 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "title is required and must be under 255 characters"})
	}
	if len(req.Description) > 2000 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "description must be under 2000 characters"})
	}
	if len(req.Pins) == 0 || len(req.Pins) > 50 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "1 to 50 pins required"})
	}
	for _, p := range req.Pins {
		if p.Lat < -90 || p.Lat > 90 || p.Lng < -180 || p.Lng > 180 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "pin coordinates out of range"})
		}
	}
	if req.Author == "" {
		req.Author = "anonymous"
	}
	if len(req.Author) > 100 {
		req.Author = req.Author[:100]
	}

	pinsJSON, _ := json.Marshal(req.Pins)
	m := &db.Map{
		Title:       req.Title,
		Description: req.Description,
		Category:    req.Category,
		Pins:        pinsJSON,
		PinCount:    len(req.Pins),
		Author:      req.Author,
	}

	if err := s.db.CreateMap(c.Context(), m); err != nil {
		log.Printf("ERROR: CreateMap: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to create map"})
	}

	return c.Status(fiber.StatusCreated).JSON(m)
}

// handleListMaps serves GET /api/maps — browse published maps.
func (s *Server) handleListMaps(c *fiber.Ctx) error {
	sortBy := c.Query("sort", "newest")
	limit := c.QueryInt("limit", 50)
	offset := c.QueryInt("offset", 0)

	if limit > 100 {
		limit = 100
	}

	maps, err := s.db.ListMaps(c.Context(), sortBy, limit, offset)
	if err != nil {
		log.Printf("ERROR: ListMaps: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to list maps"})
	}

	return c.JSON(fiber.Map{"maps": maps})
}

// handleGetMap serves GET /api/maps/:id — get a specific map.
func (s *Server) handleGetMap(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid map id"})
	}

	m, err := s.db.GetMap(c.Context(), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "map not found"})
	}

	return c.JSON(m)
}

// handleStarMap serves POST /api/maps/:id/star — star a map.
func (s *Server) handleStarMap(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid map id"})
	}

	if err := s.db.StarMap(c.Context(), id); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to star map"})
	}

	return c.JSON(fiber.Map{"ok": true})
}

// ============================================================================
// Query endpoints
// ============================================================================

// handleQueryStatus serves GET /api/query/:id/status — poll for async completion.
func (s *Server) handleQueryStatus(c *fiber.Ctx) error {
	queryID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid query id"})
	}

	// Check if result is ready.
	s.queriesMu.RLock()
	_, ready := s.queries[queryID]
	s.queriesMu.RUnlock()

	if ready {
		return c.JSON(fiber.Map{
			"id":     queryID.String(),
			"status": "completed",
		})
	}

	// Check if still computing.
	s.computingMu.RLock()
	_, computing := s.computing[queryID]
	s.computingMu.RUnlock()

	if computing {
		return c.JSON(fiber.Map{
			"id":     queryID.String(),
			"status": "computing",
		})
	}

	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
		"id":     queryID.String(),
		"status": "not_found",
	})
}

// handleTile serves GET /api/tiles/:id/:z/:x/:y (y may have .png suffix).
func (s *Server) handleTile(c *fiber.Ctx) error {
	// Parse query ID.
	queryID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid query id",
		})
	}

	z, err := strconv.Atoi(c.Params("z"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid z"})
	}
	x, err := strconv.Atoi(c.Params("x"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid x"})
	}

	// Strip .png suffix from y.
	yStr := strings.TrimSuffix(c.Params("y"), ".png")
	y, err := strconv.Atoi(yStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid y"})
	}

	// Look up result.
	s.queriesMu.RLock()
	result, ok := s.queries[queryID]
	s.queriesMu.RUnlock()
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "query not found (may have expired)",
		})
	}

	// Check cache.
	cacheKey := fmt.Sprintf("%s/%d/%d/%d", queryID.String(), z, x, y)
	if data, hit := s.tileCache.Get(cacheKey); hit {
		c.Set("Content-Type", "image/png")
		c.Set("Cache-Control", "public, max-age=3600")
		c.Set("X-Cache", "HIT")
		return c.Send(data)
	}

	// At high zoom, try COG-based 10m refinement.
	if z >= s.cogZoomThreshold && s.cogClient != nil && len(result.RefEmbeddings) > 0 {
		scores, err := s.cogClient.FetchTileScores(c.Context(), z, x, y, 2025, result.RefEmbeddings)
		if err != nil {
			log.Printf("WARN: COG fetch z=%d x=%d y=%d: %v, falling back to grid", z, x, y, err)
		} else if scores != nil {
			data, err := s.renderer.RenderTileFromScores(scores)
			if err != nil {
				log.Printf("ERROR: RenderTileFromScores z=%d x=%d y=%d: %v", z, x, y, err)
			} else {
				s.tileCache.Put(cacheKey, data)
				c.Set("Content-Type", "image/png")
				c.Set("Cache-Control", "public, max-age=3600")
				c.Set("X-Cache", "COG")
				return c.Send(data)
			}
		}
	}

	// Render tile from 2km grid (fallback or low zoom).
	data, err := s.renderer.RenderTile(result, z, x, y, s.grid)
	if err != nil {
		log.Printf("ERROR: RenderTile z=%d x=%d y=%d: %v", z, x, y, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "tile render failed",
		})
	}

	// Cache the result.
	s.tileCache.Put(cacheKey, data)

	c.Set("Content-Type", "image/png")
	c.Set("Cache-Control", "public, max-age=3600")
	c.Set("X-Cache", "MISS")
	return c.Send(data)
}

// handlePoint serves GET /api/query/:id/point?lat=...&lng=...
func (s *Server) handlePoint(c *fiber.Ctx) error {
	queryID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid query id",
		})
	}

	lat, err := strconv.ParseFloat(c.Query("lat"), 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid lat"})
	}
	lng, err := strconv.ParseFloat(c.Query("lng"), 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid lng"})
	}

	// Look up result.
	s.queriesMu.RLock()
	result, ok := s.queries[queryID]
	s.queriesMu.RUnlock()
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "query not found",
		})
	}

	// Get similarity score for this point.
	row, col, inBounds := s.grid.LatLngToRowCol(lat, lng)
	var sim float64
	var bestIdx int
	if inBounds {
		idx := row*result.Width + col
		if idx >= 0 && idx < len(result.Scores) {
			sim = float64(result.Scores[idx])
			bestIdx = int(result.BestPinIndex[idx])
		}
	}

	// Determine best pin label from the original query pins.
	// We stored the query in DB; for labels we use the in-memory Query
	// that was part of the Compute call. For now we just use the index.
	var bestLabel string
	// We don't have the original query pins in the result struct,
	// so best_pin_label is left empty unless we add it later.

	resp := PointResponse{
		Similarity:   sim,
		BestPinIndex: bestIdx,
		BestPinLabel: bestLabel,
	}

	// Fetch terrain, landcover, and biophysical from GEE in parallel.
	ctx := c.Context()
	var wg sync.WaitGroup
	var terrainData *TerrainData
	var landcoverData *LandcoverData
	var biophysicalData *BiophysicalData
	var terrainErr, landcoverErr, landDataErr error

	wg.Add(3)
	go func() {
		defer wg.Done()
		t, err := s.geeClient.FetchTerrain(ctx, lat, lng)
		if err != nil {
			terrainErr = err
			return
		}
		terrainData = &TerrainData{
			ElevationM: t.Elevation,
			SlopeDeg:   t.Slope,
			AspectDeg:  t.Aspect,
		}
	}()
	go func() {
		defer wg.Done()
		lc, err := s.geeClient.FetchLandcover(ctx, lat, lng)
		if err != nil {
			landcoverErr = err
			return
		}
		// Find the dominant class ID from percentages (best effort).
		classID := 0
		for code, name := range esaClassNames {
			if name == lc.DominantClass {
				classID = code
				break
			}
		}
		landcoverData = &LandcoverData{
			ClassName: lc.DominantClass,
			ClassID:   classID,
		}
	}()
	go func() {
		defer wg.Done()
		ld, err := s.geeClient.FetchLandData(ctx, lat, lng)
		if err != nil {
			landDataErr = err
			return
		}
		biophysicalData = &BiophysicalData{
			AnnualRainfallMm: ld.AnnualRainfallMm,
			MeanTempC:        ld.TempMeanC,
			SoilMoisture:     0, // not directly in the response; could derive from status
		}
	}()
	wg.Wait()

	if terrainErr != nil {
		log.Printf("WARN: FetchTerrain: %v", terrainErr)
	}
	if landcoverErr != nil {
		log.Printf("WARN: FetchLandcover: %v", landcoverErr)
	}
	if landDataErr != nil {
		log.Printf("WARN: FetchLandData: %v", landDataErr)
	}

	resp.Terrain = terrainData
	resp.Landcover = landcoverData
	resp.Biophysical = biophysicalData

	// Log event.
	eventPayload, _ := json.Marshal(fiber.Map{
		"lat":        lat,
		"lng":        lng,
		"similarity": sim,
	})
	_ = s.db.LogEvent(ctx, "point_inspected", &queryID, eventPayload)

	return c.JSON(resp)
}

// handleExport serves GET /api/query/:id/export — placeholder.
func (s *Server) handleExport(c *fiber.Ctx) error {
	return c.Status(fiber.StatusNotImplemented).JSON(fiber.Map{
		"error": "export not implemented yet",
	})
}

// handleHealth serves GET /api/health.
func (s *Server) handleHealth(c *fiber.Ctx) error {
	return c.JSON(HealthResponse{
		Status:     "ok",
		GridLoaded: s.grid != nil,
		GridPixels: s.grid.PixelCount(),
		LandPixels: s.grid.LandPixelCount(),
		UptimeS:    int64(time.Since(s.startTime).Seconds()),
	})
}

// esaClassNames maps ESA WorldCover class codes to names.
var esaClassNames = map[int]string{
	10:  "Tree cover",
	20:  "Shrubland",
	30:  "Grassland",
	40:  "Cropland",
	50:  "Built-up",
	60:  "Bare / sparse vegetation",
	70:  "Snow and ice",
	80:  "Permanent water bodies",
	90:  "Herbaceous wetland",
	95:  "Mangroves",
	100: "Moss and lichen",
}
