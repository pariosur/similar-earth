package grid

import (
	"math/bits"
	"sync"
	"sync/atomic"

	"github.com/pariosur/tierraai/internal/mmapfile"
)

// Grid holds a global embedding grid. For grids loaded from disk, Data and
// LandMask point into a read-only memory mapping and must not be written.
type Grid struct {
	Width  uint32
	Height uint32

	West  float64
	South float64
	East  float64
	North float64

	Scale  [BandsPerPixel]float32
	Offset [BandsPerPixel]float32

	// Data is a flat array of int8 embeddings: [row][col][band].
	// Length = Width * Height * BandsPerPixel.
	Data []int8

	// LandMask is bit-packed: bit i corresponds to pixel i (row-major).
	// Length = ceil(Width * Height / 8).
	LandMask []byte

	// CellWidth and CellHeight are the geographic size of a single pixel.
	CellWidth  float64
	CellHeight float64

	file     *mmapfile.File // backing mapping; nil for in-memory grids
	warm     atomic.Bool    // set once Preload finishes
	landOnce sync.Once
	land     []int32
}

// IsLand returns true if the pixel at (row, col) is land.
func (g *Grid) IsLand(row, col int) bool {
	if row < 0 || col < 0 || row >= int(g.Height) || col >= int(g.Width) {
		return false
	}
	idx := row*int(g.Width) + col
	byteIdx := idx / 8
	bitIdx := uint(idx % 8)
	if byteIdx >= len(g.LandMask) {
		return false
	}
	return g.LandMask[byteIdx]&(1<<(7-bitIdx)) != 0
}

// Lookup converts a lat/lng to the corresponding 64-dim embedding vector.
// Returns nil, false if the coordinate is out of bounds or over water.
func (g *Grid) Lookup(lat, lng float64) ([]int8, bool) {
	row, col, ok := g.LatLngToRowCol(lat, lng)
	if !ok {
		return nil, false
	}
	if !g.IsLand(row, col) {
		return nil, false
	}
	start := (row*int(g.Width) + col) * BandsPerPixel
	end := start + BandsPerPixel
	if end > len(g.Data) {
		return nil, false
	}
	return g.Data[start:end], true
}

// NearestLand returns the land pixel closest to (lat, lng) within maxRadius
// pixels. The land mask has scattered gaps (pixels with no embedding inside
// otherwise covered land, ~30% around Napa or central Iceland), so exact
// lookups of geocoded points or clicks often miss by a pixel.
func (g *Grid) NearestLand(lat, lng float64, maxRadius int) (row, col int, ok bool) {
	r0, c0, inBounds := g.LatLngToRowCol(lat, lng)
	if !inBounds {
		return 0, 0, false
	}
	for radius := 0; radius <= maxRadius; radius++ {
		best := -1
		for dr := -radius; dr <= radius; dr++ {
			for dc := -radius; dc <= radius; dc++ {
				if max(abs(dr), abs(dc)) != radius || !g.IsLand(r0+dr, c0+dc) {
					continue
				}
				if d := dr*dr + dc*dc; best < 0 || d < best {
					best, row, col = d, r0+dr, c0+dc
				}
			}
		}
		if best >= 0 {
			return row, col, true
		}
	}
	return 0, 0, false
}

// LookupNearest is Lookup that falls back to the nearest land pixel within
// maxRadius pixels.
func (g *Grid) LookupNearest(lat, lng float64, maxRadius int) ([]int8, bool) {
	row, col, ok := g.NearestLand(lat, lng, maxRadius)
	if !ok {
		return nil, false
	}
	start := (row*int(g.Width) + col) * BandsPerPixel
	if start+BandsPerPixel > len(g.Data) {
		return nil, false
	}
	return g.Data[start : start+BandsPerPixel], true
}

// CellCenter returns the coordinates of a pixel's center.
func (g *Grid) CellCenter(row, col int) (lat, lng float64) {
	return g.North - (float64(row)+0.5)*g.CellHeight, g.West + (float64(col)+0.5)*g.CellWidth
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// PixelCount returns the total number of pixels in the grid.
func (g *Grid) PixelCount() int {
	return int(g.Width) * int(g.Height)
}

// LandPixelCount returns the number of land pixels (set bits in the land mask).
func (g *Grid) LandPixelCount() int {
	count := 0
	for _, b := range g.LandMask {
		count += bits.OnesCount8(b)
	}
	// Clamp to actual pixel count in case trailing bits are set.
	total := g.PixelCount()
	if count > total {
		return total
	}
	return count
}

// LandPixelIndices returns all land pixel indices (flat row-major). It is
// computed on first call and shared afterwards, so callers must not modify it.
// Indices are int32 to halve its size; the global grid has ~139M pixels.
func (g *Grid) LandPixelIndices() []int32 {
	g.landOnce.Do(func() {
		total := g.PixelCount()
		indices := make([]int32, 0, g.LandPixelCount())
		for byteIdx, b := range g.LandMask {
			if b == 0 {
				continue
			}
			for bit := 7; bit >= 0; bit-- {
				if b&(1<<uint(bit)) != 0 {
					px := byteIdx*8 + (7 - bit)
					if px < total {
						indices = append(indices, int32(px))
					}
				}
			}
		}
		g.land = indices
	})
	return g.land
}

// LatLngToRowCol converts geographic coordinates to grid row and column.
// Row 0 is the top (north), col 0 is the left (west).
func (g *Grid) LatLngToRowCol(lat, lng float64) (row, col int, ok bool) {
	if lat < g.South || lat > g.North || lng < g.West || lng > g.East {
		return 0, 0, false
	}
	col = int((lng - g.West) / g.CellWidth)
	row = int((g.North - lat) / g.CellHeight)

	// Clamp to valid range (edge case for exact boundary values).
	if col >= int(g.Width) {
		col = int(g.Width) - 1
	}
	if row >= int(g.Height) {
		row = int(g.Height) - 1
	}
	return row, col, true
}
