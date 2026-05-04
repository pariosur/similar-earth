package similarity

import (
	"math"
	"sort"

	"github.com/pariosur/tierraai/internal/grid"
)

// TopMatch represents a high-scoring location found by the similarity engine.
type TopMatch struct {
	Lat          float64 `json:"lat"`
	Lng          float64 `json:"lng"`
	Score        float64 `json:"score"`
	BestPinIndex int     `json:"best_pin_index"`
}

// FindTopMatches scans the scores array for the highest-scoring pixels
// that are not within excludeRadiusKm of any reference pin.
func FindTopMatches(result *QueryResult, g *grid.Grid, pins []Pin, count int, excludeRadiusKm float64) []TopMatch {
	if result.Scores == nil || len(result.Scores) == 0 {
		return nil
	}

	w := result.Width
	h := result.Height

	// Convert exclude radius to grid cells (~0.018 degrees per km at equator)
	excludeCells := int(excludeRadiusKm / (g.CellWidth * 111.0))
	if excludeCells < 1 {
		excludeCells = 1
	}

	// Build a set of excluded pixel indices (near reference pins)
	excluded := make(map[int]bool)
	for _, pin := range pins {
		row, col, ok := g.LatLngToRowCol(pin.Lat, pin.Lng)
		if !ok {
			continue
		}
		for dr := -excludeCells; dr <= excludeCells; dr++ {
			for dc := -excludeCells; dc <= excludeCells; dc++ {
				r, c := row+dr, col+dc
				if r >= 0 && r < h && c >= 0 && c < w {
					excluded[r*w+c] = true
				}
			}
		}
	}

	// Collect top candidates using a min-heap approach (simple sort for now)
	type candidate struct {
		idx   int
		score float32
	}

	// Sample every Nth pixel to avoid scanning all 40M
	step := 1
	totalPixels := w * h
	if totalPixels > 1000000 {
		step = totalPixels / 1000000 // sample ~1M pixels
	}

	var candidates []candidate
	for i := 0; i < totalPixels; i += step {
		s := result.Scores[i]
		if s < 0.5 {
			continue // below threshold
		}
		if excluded[i] {
			continue
		}
		candidates = append(candidates, candidate{idx: i, score: s})
	}

	// Sort by score descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// Deduplicate: skip candidates too close to already-selected ones
	minDistCells := int(50.0 / (g.CellWidth * 111.0)) // 50km between results
	if minDistCells < 2 {
		minDistCells = 2
	}

	var results []TopMatch
	selectedPixels := make([]int, 0, count)
	pinCount := make(map[int]int)   // track matches per reference pin
	regionCount := make(map[int]int) // track matches per geographic region
	const maxPerPin = 2              // diversity cap per pin
	const maxPerRegion = 3           // diversity cap per region

	for _, c := range candidates {
		if len(results) >= count {
			break
		}

		row := c.idx / w
		col := c.idx % w

		// Check distance from already selected results
		tooClose := false
		for _, prev := range selectedPixels {
			pr, pc := prev/w, prev%w
			dist := math.Abs(float64(row-pr)) + math.Abs(float64(col-pc))
			if dist < float64(minDistCells) {
				tooClose = true
				break
			}
		}
		if tooClose {
			continue
		}

		bestPin := 0
		if result.BestPinIndex != nil && c.idx < len(result.BestPinIndex) {
			bestPin = int(result.BestPinIndex[c.idx])
		}

		// Diversity: skip if this pin already has enough matches
		if pinCount[bestPin] >= maxPerPin {
			continue
		}

		lat := g.North - (float64(row)+0.5)*g.CellHeight
		lng := g.West + (float64(col)+0.5)*g.CellWidth

		// Geographic diversity: ~30° lat/lng grid = roughly continental regions
		region := (int(math.Floor(lat/30))+3)*12 + int(math.Floor(lng/30))+6
		if regionCount[region] >= maxPerRegion {
			continue
		}

		results = append(results, TopMatch{
			Lat:          math.Round(lat*10000) / 10000,
			Lng:          math.Round(lng*10000) / 10000,
			Score:        math.Round(float64(c.score)*10000) / 10000,
			BestPinIndex: bestPin,
		})
		selectedPixels = append(selectedPixels, c.idx)
		pinCount[bestPin]++
		regionCount[region]++
	}

	return results
}
