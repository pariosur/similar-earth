package grid

import "math/rand"

// NewTestGrid creates a small synthetic grid for development and testing.
// It uses a random land mask (~70% land) and random int8 embeddings.
func NewTestGrid(width, height int) *Grid {
	rng := rand.New(rand.NewSource(42))

	g := &Grid{
		Width:  uint32(width),
		Height: uint32(height),
		West:   -180,
		South:  -60,
		East:   180,
		North:  70,
	}

	// Set scale to 1.0 and offset to 0.0 (identity transform).
	for i := 0; i < BandsPerPixel; i++ {
		g.Scale[i] = 1.0
		g.Offset[i] = 0.0
	}

	pixelCount := width * height

	// Build land mask with ~70% land.
	maskSize := (pixelCount + 7) / 8
	g.LandMask = make([]byte, maskSize)
	for i := 0; i < pixelCount; i++ {
		if rng.Float64() < 0.70 {
			byteIdx := i / 8
			bitIdx := uint(i % 8)
			g.LandMask[byteIdx] |= 1 << bitIdx
		}
	}

	// Fill data with random int8 embeddings for all pixels.
	dataSize := pixelCount * BandsPerPixel
	g.Data = make([]int8, dataSize)
	for i := range g.Data {
		g.Data[i] = int8(rng.Intn(256) - 128)
	}

	// Compute cell dimensions.
	g.CellWidth = (g.East - g.West) / float64(g.Width)
	g.CellHeight = (g.North - g.South) / float64(g.Height)

	return g
}
