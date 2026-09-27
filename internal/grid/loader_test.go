package grid

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// writeGridFile encodes g in the grid.bin format.
func writeGridFile(t *testing.T, g *Grid) string {
	t.Helper()
	header := make([]byte, HeaderSize)
	copy(header, Magic)
	binary.LittleEndian.PutUint32(header[8:12], Version)
	binary.LittleEndian.PutUint32(header[12:16], BandsPerPixel)
	binary.LittleEndian.PutUint32(header[16:20], g.Width)
	binary.LittleEndian.PutUint32(header[20:24], g.Height)
	for i, v := range []float64{g.West, g.South, g.East, g.North} {
		binary.LittleEndian.PutUint64(header[24+i*8:], math.Float64bits(v))
	}
	for i := 0; i < BandsPerPixel; i++ {
		binary.LittleEndian.PutUint32(header[56+i*4:], math.Float32bits(g.Scale[i]))
		binary.LittleEndian.PutUint32(header[56+256+i*4:], math.Float32bits(g.Offset[i]))
	}
	buf := append(header, make([]byte, len(g.Data))...)
	for i, v := range g.Data {
		buf[HeaderSize+i] = byte(v)
	}
	buf = append(buf, g.LandMask...)
	path := filepath.Join(t.TempDir(), "grid.bin")
	if err := os.WriteFile(path, buf, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadGridMatchesSource(t *testing.T) {
	src := NewTestGrid(90, 40)
	g, err := LoadGrid(writeGridFile(t, src))
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()

	if g.Width != src.Width || g.Height != src.Height || g.West != src.West || g.North != src.North || g.Scale != src.Scale {
		t.Fatalf("header mismatch: got %dx%d west=%v north=%v", g.Width, g.Height, g.West, g.North)
	}
	if len(g.Data) != len(src.Data) || len(g.LandMask) != len(src.LandMask) {
		t.Fatalf("section sizes: data %d/%d mask %d/%d", len(g.Data), len(src.Data), len(g.LandMask), len(src.LandMask))
	}
	for i := range src.Data {
		if g.Data[i] != src.Data[i] {
			t.Fatalf("Data[%d] = %d, want %d", i, g.Data[i], src.Data[i])
		}
	}
	got, want := g.LandPixelIndices(), src.LandPixelIndices()
	if len(got) == 0 || len(got) != len(want) {
		t.Fatalf("land pixels = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("land[%d] = %d, want %d", i, got[i], want[i])
		}
	}
	if &g.LandPixelIndices()[0] != &got[0] {
		t.Fatal("LandPixelIndices should be cached")
	}

	if g.Warm() {
		t.Fatal("grid warm before Preload")
	}
	g.Preload()
	if !g.Warm() {
		t.Fatal("grid not warm after Preload")
	}
}

func TestLoadGridRejectsTruncatedFile(t *testing.T) {
	path := writeGridFile(t, NewTestGrid(10, 10))
	if err := os.Truncate(path, HeaderSize+100); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGrid(path); err == nil {
		t.Fatal("expected error for truncated grid")
	}
}

func TestNearestLandSnapsAcrossMaskGaps(t *testing.T) {
	g := &Grid{Width: 5, Height: 5, West: 0, East: 5, South: 0, North: 5, CellWidth: 1, CellHeight: 1, LandMask: make([]byte, 4)}
	// Only (row 2, col 4) is land; IsLand reads bits most-significant first.
	i := 2*5 + 4
	g.LandMask[i/8] |= 1 << (7 - i%8)

	if _, _, ok := g.NearestLand(2.5, 2.5, 1); ok {
		t.Fatal("found land outside the search radius")
	}
	row, col, ok := g.NearestLand(2.5, 2.5, 2)
	if !ok || row != 2 || col != 4 {
		t.Fatalf("got (%d, %d, %v), want (2, 4, true)", row, col, ok)
	}
	if lat, lng := g.CellCenter(row, col); lat != 2.5 || lng != 4.5 {
		t.Fatalf("cell center = (%v, %v), want (2.5, 4.5)", lat, lng)
	}
}
