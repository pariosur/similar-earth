package api

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/google/uuid"

	"github.com/pariosur/tierraai/internal/cog"
	"github.com/pariosur/tierraai/internal/db"
	"github.com/pariosur/tierraai/internal/gee"
	"github.com/pariosur/tierraai/internal/grid"
	"github.com/pariosur/tierraai/internal/similarity"
	"github.com/pariosur/tierraai/internal/tiles"
)

// validSlug checks that a slug/ID contains only safe characters (no path traversal).
var slugRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func validSlug(s string) bool {
	return slugRe.MatchString(s) && len(s) <= 100
}

// Server is the main HTTP server for the Similar Earth API.
type Server struct {
	app       *fiber.App
	grid      *grid.Grid
	engine    *similarity.Engine
	renderer  *tiles.Renderer
	tileCache *tiles.TileCache
	db        *db.DB
	geeClient        *gee.Client
	cogClient        *cog.Client
	cogZoomThreshold int
	startTime        time.Time

	// In-memory store for active query results.
	queries   map[uuid.UUID]*similarity.QueryResult
	queriesMu sync.RWMutex

	// Track computing status for async queries.
	computing   map[uuid.UUID]bool
	computingMu sync.RWMutex

	// Pre-computed layer results.
	precomputed *similarity.PrecomputedStore
	layerMeta    map[string]LayerMeta
}

// NewServer creates the API server with all dependencies wired up.
func NewServer(g *grid.Grid, database *db.DB, geeClient *gee.Client, cogClient *cog.Client, cogZoomThreshold int) *Server {
	engine := similarity.NewEngine(g)
	renderer := tiles.NewRenderer()
	tileCache := tiles.NewTileCache(10000) // cache up to 10k tiles
	precomputed := similarity.NewPrecomputedStore()

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		BodyLimit:             1 * 1024 * 1024, // 1 MB
	})

	s := &Server{
		app:              app,
		grid:             g,
		engine:           engine,
		renderer:         renderer,
		tileCache:        tileCache,
		db:               database,
		geeClient:        geeClient,
		cogClient:        cogClient,
		cogZoomThreshold: cogZoomThreshold,
		startTime:        time.Now(),
		queries:          make(map[uuid.UUID]*similarity.QueryResult),
		computing:        make(map[uuid.UUID]bool),
		precomputed:      precomputed,
		layerMeta:         make(map[string]LayerMeta),
	}

	// Middleware
	app.Use(cors.New(cors.Config{
		AllowOrigins: "https://similar.earth,http://localhost:3000",
		AllowMethods: "GET,POST,OPTIONS",
		AllowHeaders: "Content-Type,Accept",
	}))
	app.Use(logger.New(logger.Config{
		Format: "${time} ${status} ${method} ${path} ${latency}\n",
	}))
	app.Use(recover.New())
	app.Use(func(c *fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", "DENY")
		c.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		return c.Next()
	})

	// Rate limiter for expensive endpoints
	queryLimiter := limiter.New(limiter.Config{
		Max:        10,
		Expiration: 1 * time.Minute,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "rate limit exceeded, try again later"})
		},
	})
	createLimiter := limiter.New(limiter.Config{
		Max:        30,
		Expiration: 1 * time.Hour,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "rate limit exceeded, try again later"})
		},
	})

	// Rate limiter for HD tile requests (each one costs GEE compute)
	hdTileLimiter := limiter.New(limiter.Config{
		Max:        60,
		Expiration: 1 * time.Minute,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "tile rate limit exceeded"})
		},
	})

	// Routes
	api := app.Group("/api")
	api.Get("/layers", s.handleLayers)
	api.Get("/layers/:layerId/tiles/:z/:x/:y", hdTileLimiter, s.handleLayerTile)

	// Serve pre-rendered tiles — return transparent PNG for missing tiles (no 404s)
	app.Use("/tiles", func(c *fiber.Ctx) error {
		// Sanitize path to prevent directory traversal
		reqPath := c.Path()[len("/tiles"):]
		cleaned := filepath.Clean(reqPath)
		if strings.Contains(cleaned, "..") {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid path"})
		}
		path := filepath.Join("./tiles", cleaned)
		if _, err := os.Stat(path); err == nil {
			c.Set("Cache-Control", "public, max-age=86400")
			return c.SendFile(path)
		}
		// Return 1x1 transparent PNG for missing tiles
		c.Set("Content-Type", "image/png")
		c.Set("Cache-Control", "public, max-age=86400")
		return c.Send(transparentPNG)
	})
	api.Get("/layers/:layerId/top", s.handleLayerTopMatches)
	api.Get("/query/:id/top", s.handleQueryTopMatches)
	api.Post("/maps", createLimiter, s.handleCreateMap)
	api.Get("/maps", s.handleListMaps)
	api.Get("/maps/:id", s.handleGetMap)
	api.Post("/maps/:id/star", createLimiter, s.handleStarMap)
	api.Post("/query", queryLimiter, s.handleQuery)
	api.Get("/query/:id/status", s.handleQueryStatus)
	api.Get("/tiles/:id/:z/:x/:y", s.handleTile)
	api.Get("/query/:id/point", s.handlePoint)
	api.Get("/query/:id/export", s.handleExport)
	api.Get("/health", s.handleHealth)

	return s
}

// transparentPNG is a valid 1x1 transparent RGBA PNG (70 bytes, generated by Pillow).
var transparentPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x60, 0x60, 0x60, 0x60,
	0x00, 0x00, 0x00, 0x05, 0x00, 0x01, 0xa5, 0xf6, 0x45, 0x40, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45,
	0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

// ReferencePin is a reference location coordinate.
type ReferencePin struct {
	Lat   float64 `json:"lat"`
	Lng   float64 `json:"lng"`
	Label string  `json:"label"`
}

// LayerMeta holds display metadata for a layer.
type BakedMatch struct {
	Lat          float64 `json:"lat"`
	Lng          float64 `json:"lng"`
	Score        float64 `json:"score"`
	BestPinIndex int     `json:"best_pin_index"`
	Name         string  `json:"name"`
	SimilarTo    string  `json:"similar_to"`
}

type LayerMeta struct {
	Name        string
	Description string
	Category    string
	Featured    bool
	Pins        []ReferencePin
	TopMatches  []BakedMatch
}

// LoadLayerMeta loads layer metadata from a JSON references file.
func (s *Server) LoadLayerMeta(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var data map[string]struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Category    string `json:"category"`
		Featured    bool   `json:"featured"`
		Pins        []struct {
			Lat   float64 `json:"lat"`
			Lng   float64 `json:"lng"`
			Label string  `json:"label"`
		} `json:"pins"`
		TopMatches []BakedMatch `json:"top_matches"`
	}
	if err := json.NewDecoder(f).Decode(&data); err != nil {
		return err
	}

	for id, meta := range data {
		pins := make([]ReferencePin, len(meta.Pins))
		for i, f := range meta.Pins {
			pins[i] = ReferencePin{Lat: f.Lat, Lng: f.Lng, Label: f.Label}
		}
		s.layerMeta[id] = LayerMeta{Name: meta.Name, Description: meta.Description, Category: meta.Category, Featured: meta.Featured, Pins: pins, TopMatches: meta.TopMatches}
	}
	log.Printf("Loaded metadata for %d layers", len(data))
	return nil
}

// LoadPrecomputed loads all pre-computed layer score files from a directory.
func (s *Server) LoadPrecomputed(dir string) error {
	return s.precomputed.LoadDir(dir)
}

// Start begins listening on the given address (e.g. ":8080").
func (s *Server) Start(addr string) error {
	return s.app.Listen(addr)
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown() error {
	return s.app.Shutdown()
}
