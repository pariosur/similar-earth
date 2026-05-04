package grid

import "math/bits"

// Grid holds a global embedding grid in memory.
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

// LandPixelIndices returns a slice of all land pixel indices (flat row-major).
// This is cached after first call for repeated use.
func (g *Grid) LandPixelIndices() []int {
	total := g.PixelCount()
	indices := make([]int, 0, g.LandPixelCount())
	for byteIdx, b := range g.LandMask {
		if b == 0 {
			continue
		}
		for bit := 7; bit >= 0; bit-- {
			if b&(1<<uint(bit)) != 0 {
				px := byteIdx*8 + (7 - bit)
				if px < total {
					indices = append(indices, px)
				}
			}
		}
	}
	return indices
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
