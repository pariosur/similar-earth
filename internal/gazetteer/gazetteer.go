// Package gazetteer names coordinates offline from GeoNames' cities dump
// (https://download.geonames.org/export/dump/: cities5000.zip,
// admin1CodesASCII.txt and countryInfo.txt, CC-BY 4.0), so labeling search
// results doesn't spend GeoNames API credits (1,000/hour on the free plan).
package gazetteer

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Place is a populated place from the dump.
type Place struct {
	Name    string
	Admin1  string // first-level region, e.g. "Maule Region"; may be empty
	Country string // country name; may be empty
	Lat     float64
	Lng     float64
}

// Label formats the place as "Name, Region, Country".
func (p Place) Label() string {
	parts := []string{p.Name}
	if p.Admin1 != "" && p.Admin1 != p.Name {
		parts = append(parts, p.Admin1)
	}
	if p.Country != "" {
		parts = append(parts, p.Country)
	}
	return strings.Join(parts, ", ")
}

// Gazetteer finds the nearest place to a coordinate.
type Gazetteer struct {
	cells map[int][]Place // 1° cells
	count int
}

// Len returns the number of places loaded.
func (g *Gazetteer) Len() int { return g.count }

// Load reads a cities dump (.zip or .txt). Region and country names are read
// from admin1CodesASCII.txt and countryInfo.txt in the same directory when present.
func Load(citiesPath string) (*Gazetteer, error) {
	dir := filepath.Dir(citiesPath)
	admin1, err := readNames(filepath.Join(dir, "admin1CodesASCII.txt"), 0, 1)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	countries, err := readNames(filepath.Join(dir, "countryInfo.txt"), 0, 4)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	r, closeFn, err := openDump(citiesPath)
	if err != nil {
		return nil, err
	}
	defer closeFn()

	g := &Gazetteer{cells: make(map[int][]Place)}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024) // alternate-name columns are long
	for scanner.Scan() {
		cols := strings.Split(scanner.Text(), "\t")
		if len(cols) < 11 {
			continue
		}
		lat, errLat := strconv.ParseFloat(cols[4], 64)
		lng, errLng := strconv.ParseFloat(cols[5], 64)
		if errLat != nil || errLng != nil || cols[1] == "" {
			continue
		}
		place := Place{Name: cols[1], Admin1: admin1[cols[8]+"."+cols[10]], Country: countries[cols[8]], Lat: lat, Lng: lng}
		key := cellKey(cellOf(lat), cellOf(lng))
		g.cells[key] = append(g.cells[key], place)
		g.count++
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", citiesPath, err)
	}
	if g.count == 0 {
		return nil, fmt.Errorf("%s has no places", citiesPath)
	}
	return g, nil
}

// Nearest returns the closest place within maxKm and its distance in km.
func (g *Gazetteer) Nearest(lat, lng, maxKm float64) (Place, float64, bool) {
	latCells := int(math.Ceil(maxKm / 111))
	lngCells := 180
	if c := math.Cos((math.Abs(lat) + float64(latCells)) * math.Pi / 180); c > 0.01 {
		lngCells = min(180, int(math.Ceil(maxKm/(111*c))))
	}
	var best Place
	bestKm := math.Inf(1)
	row, col := cellOf(lat), cellOf(lng)
	for dr := -latCells; dr <= latCells; dr++ {
		for dc := -lngCells; dc <= lngCells; dc++ {
			c := ((col+dc)%360 + 360) % 360 // wrap around the antimeridian
			for _, place := range g.cells[cellKey(row+dr, c)] {
				if km := haversineKm(lat, lng, place.Lat, place.Lng); km < bestKm {
					best, bestKm = place, km
				}
			}
		}
	}
	return best, bestKm, bestKm <= maxKm
}

func cellOf(deg float64) int { return int(math.Floor(deg)) }

// cellKey packs a 1° cell; longitudes are normalized to 0..359.
func cellKey(row, col int) int { return row*360 + ((col%360)+360)%360 }

func openDump(path string) (io.Reader, func(), error) {
	if !strings.HasSuffix(path, ".zip") {
		f, err := os.Open(path)
		if err != nil {
			return nil, nil, err
		}
		return f, func() { f.Close() }, nil
	}
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, nil, err
	}
	for _, file := range z.File {
		if strings.HasSuffix(file.Name, ".txt") {
			rc, err := file.Open()
			if err != nil {
				z.Close()
				return nil, nil, err
			}
			return rc, func() { rc.Close(); z.Close() }, nil
		}
	}
	z.Close()
	return nil, nil, fmt.Errorf("%s contains no .txt dump", path)
}

// readNames maps one tab-separated column to another, skipping # comments.
func readNames(path string, keyCol, nameCol int) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return map[string]string{}, err
	}
	defer f.Close()
	names := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		if cols := strings.Split(line, "\t"); len(cols) > max(keyCol, nameCol) {
			names[cols[keyCol]] = cols[nameCol]
		}
	}
	return names, scanner.Err()
}

func haversineKm(lat1, lng1, lat2, lng2 float64) float64 {
	toRad := math.Pi / 180
	dLat, dLng := (lat2-lat1)*toRad, (lng2-lng1)*toRad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*toRad)*math.Cos(lat2*toRad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * 6371 * math.Asin(math.Min(1, math.Sqrt(a)))
}
