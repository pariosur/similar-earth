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
func (e *Engine) FindFilteredTopMatches(pins []Pin, count int, filter CandidateFilter) ([]TopMatch, error) {
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
		if emb, ok := g.Lookup(pin.Lat, pin.Lng); ok {
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
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		start, end := worker*chunk, (worker+1)*chunk
		if end > total {
			end = total
		}
		wg.Add(1)
		go func(worker, start, end int) {
			defer wg.Done()
			best := topCandidateHeap(count)
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
				pushCandidate(&best, rankedCandidate{index: index, score: score, bestPin: uint8(bestPin)})
			}
			local[worker] = best
		}(worker, start, end)
	}
	wg.Wait()

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
