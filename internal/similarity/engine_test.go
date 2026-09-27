package similarity

import (
	"testing"

	"github.com/pariosur/tierraai/internal/grid"
)

func TestComputeSkipsBestPinArrayForSingleReference(t *testing.T) {
	g := &grid.Grid{
		Width: 2, Height: 1, West: 0, East: 2, South: 0, North: 1,
		CellWidth: 1, CellHeight: 1, LandMask: []byte{0xc0}, Data: make([]int8, 2*grid.BandsPerPixel),
	}
	result, err := NewEngine(g).Compute(&Query{Pins: []Pin{{Lat: .5, Lng: .5}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.BestPinIndex != nil {
		t.Fatal("single-reference query should not allocate best-pin indices")
	}
}
