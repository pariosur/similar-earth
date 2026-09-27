package grid

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"unsafe"

	"github.com/pariosur/tierraai/internal/mmapfile"
)

// LoadGrid memory-maps a grid.bin file. The embeddings and land mask point
// straight into the mapping, so they live in the OS page cache (evictable,
// never swapped) instead of the Go heap. Call Preload to warm the cache.
func LoadGrid(path string) (*Grid, error) {
	file, err := mmapfile.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open grid file: %w", err)
	}
	g, err := parseGrid(file.Bytes())
	if err != nil {
		file.Close()
		return nil, err
	}
	g.file = file

	log.Printf("Grid mapped: %dx%d (%d pixels), data=%.1f MB, mask=%.1f KB",
		g.Width, g.Height, g.PixelCount(),
		float64(len(g.Data))/(1024*1024), float64(len(g.LandMask))/1024)

	return g, nil
}

func parseGrid(buf []byte) (*Grid, error) {
	if len(buf) < HeaderSize {
		return nil, fmt.Errorf("read header: file is %d bytes, want at least %d", len(buf), HeaderSize)
	}
	header := buf[:HeaderSize]

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

	// Data section, then the bit-packed land mask.
	pixelCount := int(g.Width) * int(g.Height)
	dataSize := pixelCount * BandsPerPixel
	maskSize := (pixelCount + 7) / 8
	if len(buf) < HeaderSize+dataSize+maskSize {
		return nil, fmt.Errorf("grid file truncated: %d bytes, want %d", len(buf), HeaderSize+dataSize+maskSize)
	}
	if dataSize > 0 {
		// int8 and byte share a layout, so view the mapped bytes in place.
		g.Data = unsafe.Slice((*int8)(unsafe.Pointer(&buf[HeaderSize])), dataSize)
	}
	g.LandMask = buf[HeaderSize+dataSize : HeaderSize+dataSize+maskSize : HeaderSize+dataSize+maskSize]

	// Compute cell dimensions.
	g.CellWidth = (g.East - g.West) / float64(g.Width)
	g.CellHeight = (g.North - g.South) / float64(g.Height)

	return g, nil
}
