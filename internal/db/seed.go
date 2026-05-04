package db

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/google/uuid"
)

// SeedFeaturedMaps loads layer_references.json and inserts featured layers as maps.
func (d *DB) SeedFeaturedMaps(ctx context.Context, refsPath string, tilesDir string) error {
	f, err := os.Open(refsPath)
	if err != nil {
		return fmt.Errorf("open refs: %w", err)
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
	}
	if err := json.NewDecoder(f).Decode(&data); err != nil {
		return fmt.Errorf("decode refs: %w", err)
	}

	count := 0
	for layerID, layer := range data {
		if !layer.Featured {
			continue
		}

		// Deterministic UUID from layer ID so it's stable across restarts
		id := uuid.NewSHA1(uuid.NameSpaceDNS, []byte("terra.layer."+layerID))

		pins := make([]map[string]interface{}, len(layer.Pins))
		for i, farm := range layer.Pins {
			pins[i] = map[string]interface{}{
				"lat":   farm.Lat,
				"lng":   farm.Lng,
				"label": farm.Label,
			}
		}
		pinsJSON, _ := json.Marshal(pins)

		// Check if pre-rendered tiles exist
		hasTiles := false
		tilePath := fmt.Sprintf("%s/%s", tilesDir, layerID)
		if info, err := os.Stat(tilePath); err == nil && info.IsDir() {
			hasTiles = true
		}

		m := &Map{
			ID:          id,
			Slug:        layerID,
			Title:       layer.Name,
			Description: layer.Description,
			Category:    layer.Category,
			Pins:        pinsJSON,
			PinCount:    len(layer.Pins),
			Author:      "Similar Earth",
			IsFeatured:  true,
			HasTiles:    hasTiles,
		}

		if err := d.SeedMap(ctx, m); err != nil {
			log.Printf("WARNING: failed to seed map %s: %v", layerID, err)
			continue
		}
		count++
	}

	log.Printf("Seeded %d featured maps", count)
	return nil
}
