// Package mmapfile maps large read-only data files into memory.
//
// Mapped pages live in the OS page cache rather than the Go heap, so the
// kernel can drop them under memory pressure (and re-read them from the file)
// instead of pushing them to swap. They also stay cached across process
// restarts, so a redeploy usually starts warm.
package mmapfile

import "unsafe"

// File is a read-only view of a file's contents. Writing to Bytes() crashes
// the process on platforms where the file is memory-mapped.
type File struct {
	data   []byte
	mapped bool
}

// Bytes returns the file contents. The slice is valid until Close.
func (f *File) Bytes() []byte { return f.data }

// Touch reads one byte per page of b so the kernel faults it into the page
// cache, and returns the number of pages touched.
func Touch(b []byte) int {
	const pageSize = 4096
	var sink byte
	pages := 0
	for i := 0; i < len(b); i += pageSize {
		sink += b[i]
		pages++
	}
	touchSink = sink
	return pages
}

// touchSink keeps Touch's reads from being optimized away.
var touchSink byte

// LittleEndian reports whether the host byte order is little-endian, which
// callers need before reinterpreting mapped bytes as wider numeric types.
func LittleEndian() bool {
	x := uint16(1)
	return *(*byte)(unsafe.Pointer(&x)) == 1
}
