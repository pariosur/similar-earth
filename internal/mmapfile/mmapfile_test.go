package mmapfile

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenReadsContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.bin")
	want := bytes.Repeat([]byte{1, 2, 3, 4, 5}, 5000)
	if err := os.WriteFile(path, want, 0600); err != nil {
		t.Fatal(err)
	}
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if !bytes.Equal(f.Bytes(), want) {
		t.Fatal("mapped contents differ from file")
	}
	if pages := Touch(f.Bytes()); pages != 7 {
		t.Fatalf("Touch = %d pages, want 7", pages)
	}
}

func TestOpenEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.bin")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Bytes()) != 0 {
		t.Fatal("expected empty mapping")
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
