package similarity

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/pariosur/tierraai/internal/grid"
)

// empiricalMax is the normalisation divisor for int8 dot products.
// Calibrated against real AlphaEarth 2025 embeddings: max observed ~85K,
// p99 ~58K. Using 85000 so the full score range is spread across 0-1
// and only the top ~1-2% of land pixels reach high scores.
const empiricalMax float32 = 105000

// Engine performs similarity searches against a Grid.
type Engine struct {
	Grid *grid.Grid
}

// NewEngine creates a similarity engine backed by the given grid.
func NewEngine(g *grid.Grid) *Engine {
	return &Engine{Grid: g}
}

// Compute runs a MAX-dot-product similarity scan for all grid pixels
// against the reference pins in query. It uses goroutine parallelism,
// partitioning grid rows across NumCPU workers.
func (e *Engine) Compute(query *Query) (*QueryResult, error) {
	start := time.Now()

	g := e.Grid
	w := int(g.Width)
	h := int(g.Height)
	total := w * h

	// --- resolve reference embeddings ---
	query.RefEmbeddings = make([][]int8, len(query.Pins))
	for i, pin := range query.Pins {
		emb, ok := g.Lookup(pin.Lat, pin.Lng)
		if !ok {
			// Pin is on water or out of bounds; leave nil.
			continue
		}
		// Copy so we don't alias the grid slice.
		cp := make([]int8, len(emb))
		copy(cp, emb)
		query.RefEmbeddings[i] = cp
	}

	// Filter to valid (land) reference embeddings.
	type refEntry struct {
		idx int
		emb []int8
	}
	var refs []refEntry
	for i, emb := range query.RefEmbeddings {
		if emb != nil {
			refs = append(refs, refEntry{idx: i, emb: emb})
		}
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("no valid reference embeddings (all pins are on water or out of bounds)")
	}

	scores := make([]float32, total)
	bestPin := make([]uint8, total)

	// --- build land pixel index for faster iteration ---
	landPixels := g.LandPixelIndices()

	// --- parallel scan over land pixels only ---
	numWorkers := runtime.NumCPU()
	landCount := len(landPixels)
	if numWorkers > landCount {
		numWorkers = 1
	}
	chunkSize := landCount / numWorkers

	var wg sync.WaitGroup
	for wi := 0; wi < numWorkers; wi++ {
		start := wi * chunkSize
		end := start + chunkSize
		if wi == numWorkers-1 {
			end = landCount
		}

		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			for i := s; i < e; i++ {
				px := landPixels[i]
				dataOff := px * grid.BandsPerPixel
				pixelEmb := g.Data[dataOff : dataOff+grid.BandsPerPixel]

				var maxDot int32
				var maxIdx int
				for _, ref := range refs {
					dot := DotProductInt8(pixelEmb, ref.emb)
					if dot > maxDot {
						maxDot = dot
						maxIdx = ref.idx
					}
				}

				score := float32(maxDot) / empiricalMax
				if score < 0 {
					score = 0
				}
				if score > 1 {
					score = 1
				}
				scores[px] = score
				bestPin[px] = uint8(maxIdx)
			}
		}(start, end)
	}
	wg.Wait()

	// Collect valid reference embeddings for COG refinement.
	validRefs := make([][]int8, 0, len(refs))
	for _, ref := range refs {
		validRefs = append(validRefs, ref.emb)
	}

	return &QueryResult{
		QueryID:       query.ID,
		Scores:        scores,
		BestPinIndex:  bestPin,
		Width:         w,
		Height:        h,
		ComputeMs:     time.Since(start).Milliseconds(),
		RefEmbeddings: validRefs,
	}, nil
}
