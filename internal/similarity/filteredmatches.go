package similarity

import (
	"container/heap"
	"fmt"
	"math"
	"runtime"
	"sort"
	"sync"

	"github.com/pariosur/tierraai/internal/grid"
)

type CandidateFilter func(index int, lat, lng float64) bool

// PinSnapRadius is how far (in pixels, ~2 km each) a pin may be moved to the
// nearest land pixel when it falls in a gap of the land mask.
const PinSnapRadius = 3

// Diversity spreads results across the planet. Without it, the best matches
// are the top pixels of a single region: the few thousand best pixels usually
// cover one valley, so spacing alone can't move results elsewhere.
type Diversity struct {
	MinDistanceKm float64 // great-circle spacing between results
	ExcludeKm     float64 // skip results this close to a reference pin
	MaxPerGroup   int     // cap per group, e.g. country; 0 = no cap
	Group         func(index int) int
}

type rankedCandidate struct {
	index   int
	score   float32
	bestPin uint8
}

type candidateHeap []rankedCandidate

func (h candidateHeap) Len() int { return len(h) }
func (h candidateHeap) Less(i, j int) bool {
	if h[i].score == h[j].score {
		return h[i].index > h[j].index
	}
	return h[i].score < h[j].score
}
func (h candidateHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *candidateHeap) Push(x any)   { *h = append(*h, x.(rankedCandidate)) }
func (h *candidateHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// FindFilteredTopMatches filters every scored pixel before retaining and ranking candidates.
func FindFilteredTopMatches(result *QueryResult, g *grid.Grid, pins []Pin, count int, filter CandidateFilter) []TopMatch {
	if count <= 0 || len(result.Scores) == 0 || result.Width <= 0 {
		return nil
	}

	w, h := result.Width, result.Height
	excluded := excludedPixels(g, pins, w, h)

	// Keep a bounded oversample for geographic spacing without sorting the whole global grid.
	best := topCandidateHeap(count)
	for i, score := range result.Scores {
		if i >= w*h || score <= 0 {
			continue
		}
		row, col := i/w, i%w
		lat := g.North - (float64(row)+0.5)*g.CellHeight
		lng := g.West + (float64(col)+0.5)*g.CellWidth
		if _, skip := excluded[i]; skip || filter != nil && !filter(i, lat, lng) {
			continue
		}
		candidate := rankedCandidate{index: i, score: score}
		if i < len(result.BestPinIndex) {
			candidate.bestPin = result.BestPinIndex[i]
		}
		pushCandidate(&best, candidate)
	}
	return selectTopMatches(best, g, w, count, result.BestPinIndex)
}

// FindFilteredTopMatches scans the full grid without allocating a score per pixel.
// With diversity, it keeps the best pixel per ~1° cell instead of the best pixels
// overall, then picks spaced-out results from those regional peaks.
func (e *Engine) FindFilteredTopMatches(pins []Pin, count int, filter CandidateFilter, diversity *Diversity) ([]TopMatch, error) {
	g := e.Grid
	w, h := int(g.Width), int(g.Height)
	if count <= 0 || w <= 0 || h <= 0 {
		return nil, nil
	}

	type refEntry struct {
		idx int
		emb []int8
	}
	refs := make([]refEntry, 0, len(pins))
	for i, pin := range pins {
		if emb, ok := g.LookupNearest(pin.Lat, pin.Lng, PinSnapRadius); ok {
			refs = append(refs, refEntry{idx: i, emb: emb})
		}
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("no valid reference embeddings (all pins are on water or out of bounds)")
	}

	total := w * h
	workers := runtime.NumCPU()
	if workers > total {
		workers = total
	}
	if workers < 1 {
		workers = 1
	}
	chunk := (total + workers - 1) / workers
	local := make([]candidateHeap, workers)
	excluded := excludedPixels(g, pins, w, h)
	cellPx, cellCols, cells := diversityCells(g, w, h)
	localCells := make([][]rankedCandidate, workers)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		start, end := worker*chunk, (worker+1)*chunk
		if end > total {
			end = total
		}
		wg.Add(1)
		go func(worker, start, end int) {
			defer wg.Done()
			var best candidateHeap
			var peaks []rankedCandidate
			if diversity != nil {
				peaks = make([]rankedCandidate, cells)
			} else {
				best = topCandidateHeap(count)
			}
			for index := start; index < end; index++ {
				row, col := index/w, index%w
				if !g.IsLand(row, col) {
					continue
				}
				lat := g.North - (float64(row)+0.5)*g.CellHeight
				lng := g.West + (float64(col)+0.5)*g.CellWidth
				if _, skip := excluded[index]; skip {
					continue
				}
				if filter != nil && !filter(index, lat, lng) {
					continue
				}
				var maxDot int32
				bestPin := 0
				for _, ref := range refs {
					dot := DotProductInt8(g.Data[index*grid.BandsPerPixel:(index+1)*grid.BandsPerPixel], ref.emb)
					if dot > maxDot {
						maxDot, bestPin = dot, ref.idx
					}
				}
				score := float32(maxDot) / empiricalMax
				if score > 1 {
					score = 1
				}
				if score <= 0 {
					continue
				}
				candidate := rankedCandidate{index: index, score: score, bestPin: uint8(bestPin)}
				if peaks != nil {
					if cell := (row/cellPx)*cellCols + col/cellPx; score > peaks[cell].score {
						peaks[cell] = candidate
					}
					continue
				}
				pushCandidate(&best, candidate)
			}
			local[worker], localCells[worker] = best, peaks
		}(worker, start, end)
	}
	wg.Wait()

	if diversity != nil {
		// Workers cover disjoint pixel ranges, so a cell's peak is the best across workers.
		peaks := make([]rankedCandidate, 0, cells)
		for cell := 0; cell < cells; cell++ {
			var peak rankedCandidate
			for _, workerPeaks := range localCells {
				if c := workerPeaks[cell]; c.score > peak.score || c.score == peak.score && c.score > 0 && c.index < peak.index {
					peak = c
				}
			}
			if peak.score > 0 {
				peaks = append(peaks, peak)
			}
		}
		return selectDiverse(peaks, g, w, count, pins, diversity), nil
	}

	best := topCandidateHeap(count)
	for _, candidates := range local {
		for _, candidate := range candidates {
			pushCandidate(&best, candidate)
		}
	}
	return selectTopMatches(best, g, w, count, nil), nil
}

func excludedPixels(g *grid.Grid, pins []Pin, w, h int) map[int]struct{} {
	excluded := make(map[int]struct{}, len(pins)*9)
	for _, pin := range pins {
		row, col, ok := g.LatLngToRowCol(pin.Lat, pin.Lng)
		if !ok {
			continue
		}
		for dr := -1; dr <= 1; dr++ {
			for dc := -1; dc <= 1; dc++ {
				r, c := row+dr, col+dc
				if r >= 0 && r < h && c >= 0 && c < w {
					excluded[r*w+c] = struct{}{}
				}
			}
		}
	}
	return excluded
}

// topCandidateHeap keeps enough candidates to preserve geographic spacing.
func topCandidateHeap(count int) candidateHeap {
	limit := count * 1000
	if limit > 10000 {
		limit = 10000
	}
	best := make(candidateHeap, 0, limit)
	heap.Init(&best)
	return best
}

func pushCandidate(best *candidateHeap, candidate rankedCandidate) {
	if len(*best) < cap(*best) {
		heap.Push(best, candidate)
	} else if candidate.score > (*best)[0].score || candidate.score == (*best)[0].score && candidate.index < (*best)[0].index {
		(*best)[0] = candidate
		heap.Fix(best, 0)
	}
}

func selectTopMatches(best candidateHeap, g *grid.Grid, w, count int, bestPinIndices []uint8) []TopMatch {
	sort.Slice(best, func(i, j int) bool {
		if best[i].score == best[j].score {
			return best[i].index < best[j].index
		}
		return best[i].score > best[j].score
	})

	minDist := int(50 / (g.CellWidth * 111))
	if minDist < 2 {
		minDist = 2
	}
	selected := make([]TopMatch, 0, count)
	selectedPixels := make([]int, 0, count)
	for _, candidate := range best {
		if len(selected) == count {
			break
		}
		row, col := candidate.index/w, candidate.index%w
		tooClose := false
		for _, prev := range selectedPixels {
			if math.Abs(float64(row-prev/w))+math.Abs(float64(col-prev%w)) < float64(minDist) {
				tooClose = true
				break
			}
		}
		if tooClose {
			continue
		}
		bestPin := int(candidate.bestPin)
		if candidate.index < len(bestPinIndices) {
			bestPin = int(bestPinIndices[candidate.index])
		}
		selected = append(selected, TopMatch{
			Lat:          math.Round((g.North-(float64(row)+0.5)*g.CellHeight)*10000) / 10000,
			Lng:          math.Round((g.West+(float64(col)+0.5)*g.CellWidth)*10000) / 10000,
			Score:        math.Round(float64(candidate.score)*10000) / 10000,
			BestPinIndex: bestPin,
		})
		selectedPixels = append(selectedPixels, candidate.index)
	}
	return selected
}

// diversityCells splits the grid into ~1° cells for regional peak tracking.
func diversityCells(g *grid.Grid, w, h int) (cellPx, cellCols, cells int) {
	cellPx = int(math.Round(1 / g.CellWidth))
	if cellPx < 1 {
		cellPx = 1
	}
	cellCols = (w + cellPx - 1) / cellPx
	return cellPx, cellCols, cellCols * ((h + cellPx - 1) / cellPx)
}

// selectDiverse picks up to count regional peaks, best first, at least
// MinDistanceKm apart, away from the reference pins and at most MaxPerGroup per
// group. If that leaves too few (e.g. a search limited to one country), it
// fills first without the group cap, then with a third of the spacing.
func selectDiverse(peaks []rankedCandidate, g *grid.Grid, w, count int, pins []Pin, d *Diversity) []TopMatch {
	sort.Slice(peaks, func(i, j int) bool {
		if peaks[i].score == peaks[j].score {
			return peaks[i].index < peaks[j].index
		}
		return peaks[i].score > peaks[j].score
	})
	type chosen struct {
		lat, lng float64
		match    TopMatch
	}
	var picked []chosen
	taken := make(map[int]bool)
	perGroup := make(map[int]int)
	passes := []struct {
		km       float64
		groupCap int
	}{{d.MinDistanceKm, d.MaxPerGroup}, {d.MinDistanceKm, 0}, {d.MinDistanceKm / 3, 0}}
	for _, pass := range passes {
		for i, peak := range peaks {
			if len(picked) == count {
				break
			}
			if taken[i] {
				continue
			}
			lat, lng := g.CellCenter(peak.index/w, peak.index%w)
			if nearAny(lat, lng, d.ExcludeKm, len(pins), func(k int) (float64, float64) { return pins[k].Lat, pins[k].Lng }) ||
				nearAny(lat, lng, pass.km, len(picked), func(k int) (float64, float64) { return picked[k].lat, picked[k].lng }) {
				continue
			}
			group := 0
			if d.Group != nil {
				group = d.Group(peak.index)
				if pass.groupCap > 0 && perGroup[group] >= pass.groupCap {
					continue
				}
			}
			taken[i] = true
			perGroup[group]++
			picked = append(picked, chosen{lat, lng, TopMatch{
				Lat:          math.Round(lat*10000) / 10000,
				Lng:          math.Round(lng*10000) / 10000,
				Score:        math.Round(float64(peak.score)*10000) / 10000,
				BestPinIndex: int(peak.bestPin),
			}})
		}
	}
	sort.SliceStable(picked, func(i, j int) bool { return picked[i].match.Score > picked[j].match.Score })
	matches := make([]TopMatch, len(picked))
	for i, p := range picked {
		matches[i] = p.match
	}
	return matches
}

func nearAny(lat, lng, km float64, n int, at func(int) (float64, float64)) bool {
	if km <= 0 {
		return false
	}
	for k := 0; k < n; k++ {
		if plat, plng := at(k); haversineKm(lat, lng, plat, plng) < km {
			return true
		}
	}
	return false
}

func haversineKm(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusKm = 6371
	toRad := math.Pi / 180
	dLat, dLng := (lat2-lat1)*toRad, (lng2-lng1)*toRad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*toRad)*math.Cos(lat2*toRad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earthRadiusKm * math.Asin(math.Min(1, math.Sqrt(a)))
}
