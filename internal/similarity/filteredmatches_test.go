package similarity

import (
	"testing"

	"github.com/pariosur/tierraai/internal/grid"
)

func TestFindFilteredTopMatchesFiltersBeforeRanking(t *testing.T) {
	g := &grid.Grid{Width: 4, Height: 4, West: 0, East: 4, South: 0, North: 4, CellWidth: 1, CellHeight: 1}
	result := &QueryResult{
		Width: 4, Height: 4,
		Scores: []float32{1, 0, 0, 0, 0, .9, 0, 0, 0, 0, 0, 0, 0, 0, 0, .8},
	}
	matches := FindFilteredTopMatches(result, g, nil, 2, func(index int, _, _ float64) bool {
		return index != 0
	})
	if len(matches) != 2 {
		t.Fatalf("got %d matches, want 2", len(matches))
	}
	if matches[0].Score != .9 || matches[1].Score != .8 {
		t.Fatalf("got scores %v and %v, want .9 and .8", matches[0].Score, matches[1].Score)
	}
}

func TestStreamingSearchMatchesFullGridResults(t *testing.T) {
	const width, height = 12, 8
	g := &grid.Grid{
		Width: width, Height: height, West: 0, East: width, South: 0, North: height,
		CellWidth: 1, CellHeight: 1, LandMask: make([]byte, (width*height+7)/8),
		Data: make([]int8, width*height*grid.BandsPerPixel),
	}
	for i := range g.LandMask {
		g.LandMask[i] = 0xff
	}
	for i := 0; i < width*height; i++ {
		g.Data[i*grid.BandsPerPixel] = int8(i + 1)
	}
	g.Data[0] = 127
	pins := []Pin{{Lat: 7.5, Lng: .5}}
	engine := NewEngine(g)
	full, err := engine.Compute(&Query{Pins: pins})
	if err != nil {
		t.Fatal(err)
	}
	filter := func(index int, _, _ float64) bool { return index%2 == 0 }
	want := FindFilteredTopMatches(full, g, pins, 10, func(index int, lat, lng float64) bool {
		return filter(index, lat, lng)
	})
	got, err := engine.FindFilteredTopMatches(pins, 10, filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d matches, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("match %d: got %#v, want %#v", i, got[i], want[i])
		}
	}
}
