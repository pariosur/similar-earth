package similarity

import (
	"fmt"
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
	got, err := engine.FindFilteredTopMatches(pins, 10, filter, nil)
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

// diversityFixture is one row of 1° cells whose scores fall from west to east.
func diversityFixture() (*grid.Grid, []rankedCandidate) {
	g := &grid.Grid{Width: 40, Height: 20, West: 0, East: 40, South: 0, North: 20, CellWidth: 1, CellHeight: 1}
	peaks := make([]rankedCandidate, 0, 40)
	for col := 0; col < 40; col++ {
		peaks = append(peaks, rankedCandidate{index: 10*40 + col, score: 1 - float32(col)*0.01})
	}
	return g, peaks
}

func pickedCols(matches []TopMatch) []int {
	cols := make([]int, len(matches))
	for i, m := range matches {
		cols[i] = int(m.Lng)
	}
	return cols
}

func TestSelectDiverseSpacesAndCapsGroups(t *testing.T) {
	g, peaks := diversityFixture()
	d := &Diversity{MinDistanceKm: 300, MaxPerGroup: 3, Group: func(index int) int { return (index % 40) / 20 }}
	got := pickedCols(selectDiverse(peaks, g, 40, 6, nil, d))
	// ~2.7° is 300 km here: every third cell, three per half of the row.
	want := []int{0, 3, 6, 20, 23, 26}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("picked cols %v, want %v", got, want)
	}
}

func TestSelectDiverseFillsPastGroupCap(t *testing.T) {
	g, peaks := diversityFixture()
	// Everything in one group (a search limited to one country) still fills.
	d := &Diversity{MinDistanceKm: 300, MaxPerGroup: 3, Group: func(int) int { return 0 }}
	got := pickedCols(selectDiverse(peaks, g, 40, 6, nil, d))
	if want := []int{0, 3, 6, 9, 12, 15}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("picked cols %v, want %v", got, want)
	}
}

func TestSelectDiverseSkipsReferenceArea(t *testing.T) {
	g, peaks := diversityFixture()
	pins := []Pin{{Lat: 9.5, Lng: 0.5}}
	got := pickedCols(selectDiverse(peaks, g, 40, 2, pins, &Diversity{MinDistanceKm: 300, ExcludeKm: 150}))
	if want := []int{2, 5}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("picked cols %v, want %v", got, want)
	}
}

func TestEngineDiverseSearchSpreadsResults(t *testing.T) {
	const width, height = 40, 20
	g := &grid.Grid{
		Width: width, Height: height, West: 0, East: width, South: 0, North: height,
		CellWidth: 1, CellHeight: 1, LandMask: make([]byte, (width*height+7)/8),
		Data: make([]int8, width*height*grid.BandsPerPixel),
	}
	for i := range g.LandMask {
		g.LandMask[i] = 0xff
	}
	for i := 0; i < width*height; i++ {
		g.Data[i*grid.BandsPerPixel] = int8(1 + i%100)
	}
	pins := []Pin{{Lat: 0.5, Lng: 39.5}} // last pixel: the highest embedding value
	got, err := NewEngine(g).FindFilteredTopMatches(pins, 5, nil, &Diversity{MinDistanceKm: 300})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d matches, want 5", len(got))
	}
	for i := range got {
		for j := i + 1; j < len(got); j++ {
			if d := haversineKm(got[i].Lat, got[i].Lng, got[j].Lat, got[j].Lng); d < 300 {
				t.Fatalf("matches %d and %d are %.0f km apart, want >= 300", i, j, d)
			}
		}
		if i > 0 && got[i].Score > got[i-1].Score {
			t.Fatalf("matches not sorted by score: %v", got)
		}
	}
}
