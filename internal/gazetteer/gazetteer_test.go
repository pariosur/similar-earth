package gazetteer

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// row builds a cities dump line: id, name, ascii, alternates, lat, lng, class, code, country, cc2, admin1.
func row(name, lat, lng, country, admin1 string) string {
	return strings.Join([]string{"1", name, name, "", lat, lng, "P", "PPL", country, "", admin1}, "\t")
}

func writeDump(t *testing.T, zipped bool) string {
	t.Helper()
	dir := t.TempDir()
	dump := strings.Join([]string{
		row("Hualañé", "-34.97", "-71.80", "CL", "07"),
		row("Talca", "-35.43", "-71.66", "CL", "07"),
		row("Suva", "-18.14", "178.44", "FJ", "C"),
	}, "\n") + "\n"
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("admin1CodesASCII.txt", "CL.07\tMaule Region\tMaule Region\t3880306\n")
	write("countryInfo.txt", "#ISO\tISO3\tISO-Numeric\tfips\tCountry\nCL\tCHL\t152\tCI\tChile\nFJ\tFJI\t242\tFJ\tFiji\n")
	if !zipped {
		write("cities.txt", dump)
		return filepath.Join(dir, "cities.txt")
	}
	path := filepath.Join(dir, "cities.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("cities.txt")
	w.Write([]byte(dump))
	zw.Close()
	f.Close()
	return path
}

func TestNearestLabelsWithRegionAndCountry(t *testing.T) {
	for _, zipped := range []bool{false, true} {
		g, err := Load(writeDump(t, zipped))
		if err != nil {
			t.Fatal(err)
		}
		if g.Len() != 3 {
			t.Fatalf("loaded %d places, want 3", g.Len())
		}
		place, km, ok := g.Nearest(-35.0, -71.8, 50)
		if !ok || place.Label() != "Hualañé, Maule Region, Chile" || km > 5 {
			t.Fatalf("got %q at %.1f km (%v)", place.Label(), km, ok)
		}
	}
}

func TestNearestRespectsMaxDistance(t *testing.T) {
	g, err := Load(writeDump(t, false))
	if err != nil {
		t.Fatal(err)
	}
	if place, km, ok := g.Nearest(-30.0, -71.8, 50); ok {
		t.Fatalf("found %q %.0f km away, beyond the 50 km limit", place.Name, km)
	}
}

func TestNearestWrapsAroundAntimeridian(t *testing.T) {
	g, err := Load(writeDump(t, false))
	if err != nil {
		t.Fatal(err)
	}
	// Just across 180° from Suva (178.44°E), at -179.9°.
	place, _, ok := g.Nearest(-18.14, -179.9, 300)
	if !ok || place.Name != "Suva" || place.Label() != "Suva, Fiji" {
		t.Fatalf("got %q (%v), want Suva, Fiji", place.Label(), ok)
	}
}
