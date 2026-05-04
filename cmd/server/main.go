package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/pariosur/tierraai/internal/api"
	"github.com/pariosur/tierraai/internal/cog"
	"github.com/pariosur/tierraai/internal/config"
	"github.com/pariosur/tierraai/internal/db"
	"github.com/pariosur/tierraai/internal/gee"
	"github.com/pariosur/tierraai/internal/grid"
)

func main() {
	cfg := config.Load()

	// Load grid: try real file first, fall back to test grid.
	var g *grid.Grid
	g, err := grid.LoadGrid(cfg.GridPath)
	if err != nil {
		log.Printf("WARNING: Could not load grid from %s: %v", cfg.GridPath, err)
		log.Printf("WARNING: Using synthetic test grid (360x130) for development")
		g = grid.NewTestGrid(360, 130)
	}

	landCount := g.LandPixelCount()
	dataMB := float64(g.PixelCount()*grid.BandsPerPixel) / (1024 * 1024)
	log.Printf("Grid ready: %dx%d, %d land pixels (%.1f%%), %.1f MB embeddings",
		g.Width, g.Height, landCount,
		float64(landCount)/float64(g.PixelCount())*100,
		dataMB,
	)

	// Connect to PostgreSQL.
	ctx := context.Background()
	database, err := db.NewDB(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	// Run migrations.
	if err := database.Migrate(ctx); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}
	log.Println("Database migrations applied")

	// Create GEE client.
	geeClient := gee.NewClient(cfg.GEEServiceURL)
	log.Printf("GEE client configured: %s", cfg.GEEServiceURL)

	// Create COG client (uses same Python service for 10m refinement).
	cogClient := cog.NewClient(cfg.GEEServiceURL, 4) // max 4 concurrent EE requests
	log.Printf("COG client configured: %s (zoom threshold: %d)", cfg.GEEServiceURL, cfg.COGZoomThreshold)

	// Create and start API server.
	srv := api.NewServer(g, database, geeClient, cogClient, cfg.COGZoomThreshold)

	// Load pre-computed layer scores from data/ directory.
	if err := srv.LoadPrecomputed("data"); err != nil {
		log.Printf("WARNING: Could not load pre-computed scores: %v", err)
	}

	// Load layer metadata.
	if err := srv.LoadLayerMeta("data/layer_references.json"); err != nil {
		log.Printf("WARNING: Could not load layer metadata: %v", err)
	}

	// Seed featured maps from crop references.
	if err := database.SeedFeaturedMaps(ctx, "data/layer_references.json", "tiles"); err != nil {
		log.Printf("WARNING: Could not seed featured maps: %v", err)
	}

	// Graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		log.Println("Shutting down server...")
		if err := srv.Shutdown(); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
	}()

	log.Printf("Similar Earth server starting on :%s", cfg.Port)
	if err := srv.Start(":" + cfg.Port); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
