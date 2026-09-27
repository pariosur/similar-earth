package grid

import (
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/pariosur/tierraai/internal/mmapfile"
)

const pageShift = 12 // 4 KiB pages

// Preload faults the land mask and every page holding a land pixel's
// embedding into the page cache, so the first scans after a cold start run
// at memory speed. Ocean-only pages are skipped since scans never read them.
// Safe to run while requests are being served; it only reads.
func (g *Grid) Preload() {
	start := time.Now()
	pages := mmapfile.Touch(g.LandMask)

	land := g.LandPixelIndices()
	workers := runtime.NumCPU()
	chunk := (len(land) + workers - 1) / workers
	counts := make([]int, workers)
	sums := make([]int8, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		lo, hi := w*chunk, min((w+1)*chunk, len(land))
		if lo >= hi {
			continue
		}
		wg.Add(1)
		go func(w int, pixels []int32) {
			defer wg.Done()
			var sink int8
			lastPage := -1
			for _, px := range pixels {
				// A pixel's 64 bytes can straddle a page boundary because
				// the data section starts at file offset HeaderSize.
				first := int(px) * BandsPerPixel
				for _, off := range [2]int{first, first + BandsPerPixel - 1} {
					if page := (HeaderSize + off) >> pageShift; page != lastPage {
						sink += g.Data[off]
						lastPage = page
						counts[w]++
					}
				}
			}
			sums[w] = sink
		}(w, land[lo:hi])
	}
	wg.Wait()
	for w, c := range counts {
		pages += c
		preloadSink += sums[w]
	}

	g.warm.Store(true)
	log.Printf("Grid preloaded: %d land pixels, %d pages (%.1f MB) in %s",
		len(land), pages, float64(pages)*4096/(1024*1024), time.Since(start).Round(time.Millisecond))
}

// Warm reports whether Preload has finished.
func (g *Grid) Warm() bool { return g.warm.Load() }

// Close unmaps the grid file, if it was mapped. The grid must not be used
// afterwards.
func (g *Grid) Close() error {
	if g.file == nil {
		return nil
	}
	err := g.file.Close()
	g.file, g.Data, g.LandMask = nil, nil, nil
	return err
}

// preloadSink keeps Preload's page reads from being optimized away.
var preloadSink int8
