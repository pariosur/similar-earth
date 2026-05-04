package grid

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"os"
)

// LoadGrid reads a grid.bin file and returns an in-memory Grid.
func LoadGrid(path string) (*Grid, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open grid file: %w", err)
	}
	defer f.Close()

	// Read header.
	header := make([]byte, HeaderSize)
	if _, err := io.ReadFull(f, header); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	// Validate magic bytes.
	magic := string(header[0:8])
	if magic != Magic {
		return nil, fmt.Errorf("invalid magic bytes: got %q, want %q", magic, Magic)
	}

	// Validate version.
	version := binary.LittleEndian.Uint32(header[8:12])
	if version != Version {
		return nil, fmt.Errorf("unsupported version: got %d, want %d", version, Version)
	}

	g := &Grid{}
	bands := binary.LittleEndian.Uint32(header[12:16])
	g.Width = binary.LittleEndian.Uint32(header[16:20])
	g.Height = binary.LittleEndian.Uint32(header[20:24])
	if bands != BandsPerPixel {
		return nil, fmt.Errorf("unexpected band count: got %d, want %d", bands, BandsPerPixel)
	}

	g.West = math.Float64frombits(binary.LittleEndian.Uint64(header[24:32]))
	g.South = math.Float64frombits(binary.LittleEndian.Uint64(header[32:40]))
	g.East = math.Float64frombits(binary.LittleEndian.Uint64(header[40:48]))
	g.North = math.Float64frombits(binary.LittleEndian.Uint64(header[48:56]))

	// Read scale and offset arrays (each 64 * 4 = 256 bytes).
	for i := 0; i < BandsPerPixel; i++ {
		off := 56 + i*4
		g.Scale[i] = math.Float32frombits(binary.LittleEndian.Uint32(header[off : off+4]))
	}
	for i := 0; i < BandsPerPixel; i++ {
		off := 56 + 256 + i*4
		g.Offset[i] = math.Float32frombits(binary.LittleEndian.Uint32(header[off : off+4]))
	}

	// Read data section.
	pixelCount := int(g.Width) * int(g.Height)
	dataSize := pixelCount * BandsPerPixel
	dataBuf := make([]byte, dataSize)
	if _, err := io.ReadFull(f, dataBuf); err != nil {
		return nil, fmt.Errorf("read data section: %w", err)
	}
	g.Data = make([]int8, dataSize)
	for i, b := range dataBuf {
		g.Data[i] = int8(b)
	}

	// Read land mask section.
	maskSize := (pixelCount + 7) / 8
	g.LandMask = make([]byte, maskSize)
	if _, err := io.ReadFull(f, g.LandMask); err != nil {
		return nil, fmt.Errorf("read land mask: %w", err)
	}

	// Compute cell dimensions.
	g.CellWidth = (g.East - g.West) / float64(g.Width)
	g.CellHeight = (g.North - g.South) / float64(g.Height)

	landCount := g.LandPixelCount()
	dataMB := float64(dataSize) / (1024 * 1024)
	maskKB := float64(maskSize) / 1024

	log.Printf("Grid loaded: %dx%d (%d pixels, %d land), data=%.1f MB, mask=%.1f KB",
		g.Width, g.Height, pixelCount, landCount, dataMB, maskKB)

	return g, nil
}
