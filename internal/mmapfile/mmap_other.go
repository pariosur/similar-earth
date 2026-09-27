//go:build !(linux || darwin)

package mmapfile

import "os"

// Open reads the whole file into memory on platforms without mmap support.
func Open(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return &File{data: data}, nil
}

// Close releases the file contents.
func (f *File) Close() error {
	f.data = nil
	return nil
}
