package searchdata

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/pariosur/tierraai/internal/grid"
)

func TestLoadAndReadMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "search.bin")
	pixels := make([]byte, 24)
	copy(pixels, magic[:])
	binary.LittleEndian.PutUint32(pixels[8:12], 2)
	binary.LittleEndian.PutUint32(pixels[12:16], 1)
	binary.LittleEndian.PutUint16(pixels[16:18], 1)
	binary.LittleEndian.PutUint16(pixels[18:20], uint16(int16(1234)))
	binary.LittleEndian.PutUint16(pixels[20:22], 1)
	missing := int16(-32768)
	binary.LittleEndian.PutUint16(pixels[22:24], uint16(missing))
	if err := os.WriteFile(path, pixels, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "search.json"), []byte(`[{"id":1,"code":"IT","name":"Italy","continent":"Europe"}]`), 0600); err != nil {
		t.Fatal(err)
	}

	g := &grid.Grid{Width: 2, Height: 1}
	data, err := Load(path, g)
	if err != nil {
		t.Fatal(err)
	}
	country, temp, ok := data.At(0)
	if !ok || country.Code != "IT" || temp != 12.34 {
		t.Fatalf("At(0) = %#v, %v, %v", country, temp, ok)
	}
	if _, _, ok := data.At(1); ok {
		t.Fatal("expected missing temperature sentinel")
	}
	if !data.HasCountry("Italy") || !CountryIncluded([]string{"IT"}, country) {
		t.Fatal("country lookup failed")
	}
}
