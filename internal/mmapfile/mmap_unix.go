//go:build linux || darwin

package mmapfile

import (
	"fmt"
	"os"
	"syscall"
)

// Open maps the whole file read-only. The file descriptor is closed right
// away; the mapping stays valid until Close.
func Open(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if size == 0 {
		return &File{}, nil
	}
	if int64(int(size)) != size {
		return nil, fmt.Errorf("mmap %s: file too large (%d bytes)", path, size)
	}

	data, err := syscall.Mmap(int(f.Fd()), 0, int(size), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("mmap %s: %w", path, err)
	}
	return &File{data: data, mapped: true}, nil
}

// Close unmaps the file. Any slice derived from Bytes() must not be used
// afterwards.
func (f *File) Close() error {
	if !f.mapped {
		f.data = nil
		return nil
	}
	err := syscall.Munmap(f.data)
	f.data, f.mapped = nil, false
	return err
}
