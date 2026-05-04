package config

import (
	"os"
	"strconv"
)

// Config holds all environment-based configuration for the TierraAI server.
type Config struct {
	Port          string
	DatabaseURL   string
	GEEServiceURL string
	GridPath      string
	CacheDir         string
	COGBaseURL       string
	COGZoomThreshold int
}

// Load reads configuration from environment variables, applying defaults.
func Load() *Config {
	threshold := 9
	if v := os.Getenv("COG_ZOOM_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			threshold = n
		}
	}
	return &Config{
		Port:             envOrDefault("PORT", "8080"),
		DatabaseURL:      envOrDefault("DATABASE_URL", "postgres://tierraai:tierraai@localhost:5432/tierraai?sslmode=disable"),
		GEEServiceURL:    envOrDefault("GEE_SERVICE_URL", "http://localhost:8001"),
		GridPath:         envOrDefault("GRID_PATH", "./data/grid.bin"),
		CacheDir:         envOrDefault("CACHE_DIR", "./cache"),
		COGBaseURL:       envOrDefault("COG_BASE_URL", "https://data.source.coop/"),
		COGZoomThreshold: threshold,
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
