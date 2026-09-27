package searchdata

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/pariosur/tierraai/internal/grid"
	"github.com/pariosur/tierraai/internal/mmapfile"
)

var magic = [8]byte{'S', 'E', 'S', 'R', 'C', 'H', '0', '1'}
var countryAliases = map[string]string{"usa": "united states of america", "united states": "united states of america", "uk": "united kingdom", "russia": "russian federation", "south korea": "republic of korea", "north korea": "democratic people's republic of korea"}

type Country struct {
	ID        uint16 `json:"id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Continent string `json:"continent"`
}

type Data struct {
	file      *mmapfile.File
	pixels    []byte
	countries map[uint16]Country
}

// Load memory-maps the per-pixel metadata file so it sits in the page cache
// rather than the Go heap. Call Preload to warm it.
func Load(path string, g *grid.Grid) (data *Data, err error) {
	file, err := mmapfile.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read search metadata: %w", err)
	}
	defer func() {
		if err != nil {
			file.Close()
		}
	}()
	pixels := file.Bytes()
	if len(pixels) < 16 || string(pixels[:8]) != string(magic[:]) {
		return nil, fmt.Errorf("invalid search metadata header")
	}
	width, height := binary.LittleEndian.Uint32(pixels[8:12]), binary.LittleEndian.Uint32(pixels[12:16])
	count := uint64(width) * uint64(height)
	if width != g.Width || height != g.Height || uint64(len(pixels)-16) != count*4 {
		return nil, fmt.Errorf("search metadata dimensions or size do not match grid")
	}
	manifestPath := path[:len(path)-len(".bin")] + ".json"
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read country manifest: %w", err)
	}
	var list []Country
	if err := json.Unmarshal(manifest, &list); err != nil {
		return nil, fmt.Errorf("parse country manifest: %w", err)
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("country manifest is empty")
	}
	countries := make(map[uint16]Country, len(list))
	for _, country := range list {
		if country.ID == 0 || country.Code == "" || country.Name == "" {
			return nil, fmt.Errorf("invalid country manifest entry")
		}
		countries[country.ID] = country
	}
	return &Data{file: file, pixels: pixels[16:], countries: countries}, nil
}

// Preload faults the whole metadata file into the page cache.
func (d *Data) Preload() int { return mmapfile.Touch(d.pixels) }

// Close unmaps the metadata file. d must not be used afterwards.
func (d *Data) Close() error {
	d.pixels = nil
	return d.file.Close()
}

func (d *Data) HasCountry(name string) bool {
	for _, country := range d.countries {
		if equalCountryName(name, country) {
			return true
		}
	}
	return false
}

func (d *Data) CountryIDs(values []string) map[uint16]struct{} {
	ids := make(map[uint16]struct{})
	for id, country := range d.countries {
		if CountryIncluded(values, country) {
			ids[id] = struct{}{}
		}
	}
	return ids
}

func CountryIncluded(values []string, country Country) bool {
	for _, name := range values {
		if equalCountryName(name, country) {
			return true
		}
	}
	return false
}

func equalCountryName(name string, country Country) bool {
	if strings.EqualFold(strings.TrimSpace(name), country.Name) || strings.EqualFold(strings.TrimSpace(name), country.Code) {
		return true
	}
	canonical, ok := countryAliases[strings.ToLower(strings.TrimSpace(name))]
	return ok && strings.EqualFold(canonical, country.Name)
}

func (d *Data) At(index int) (Country, float64, bool) {
	if index < 0 || index >= len(d.pixels)/4 {
		return Country{}, 0, false
	}
	off := index * 4
	country := d.countries[binary.LittleEndian.Uint16(d.pixels[off:off+2])]
	temp := int16(binary.LittleEndian.Uint16(d.pixels[off+2 : off+4]))
	if country.ID == 0 || temp == -32768 {
		return country, 0, false
	}
	return country, float64(temp) / 100, true
}
