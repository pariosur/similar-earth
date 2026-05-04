package grid

// Grid binary format constants.
const (
	// Magic is the 8-byte magic string at the start of grid.bin files.
	Magic = "TRGRID01"

	// Version is the current grid format version.
	Version = 1

	// HeaderSize is the total header size in bytes:
	//   8 (magic) + 4 (version) + 4 (width) + 4 (height) + 4 (bands) +
	//   8 (west) + 8 (south) + 8 (east) + 8 (north) +
	//   256 (scale: 64 * float32) + 256 (offset: 64 * float32) = 568
	HeaderSize = 568

	// BandsPerPixel is the number of embedding dimensions per pixel.
	BandsPerPixel = 64
)
