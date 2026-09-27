package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pariosur/tierraai/internal/similarity"

	"github.com/pariosur/tierraai/internal/gazetteer"
	"github.com/pariosur/tierraai/internal/grid"
)

func TestStoreQueryResultKeepsOnlyLatestHeatmap(t *testing.T) {
	s := &Server{queries: make(map[uuid.UUID]*similarity.QueryResult), queryExpiry: make(map[uuid.UUID]time.Time)}
	first, second := uuid.New(), uuid.New()
	s.storeQueryResult(first, &similarity.QueryResult{})
	s.storeQueryResult(second, &similarity.QueryResult{})
	if len(s.queries) != 1 || s.queries[second] == nil {
		t.Fatalf("retained query results = %d, latest result missing", len(s.queries))
	}
	if _, ok := s.queries[first]; ok {
		t.Fatal("older heatmap was not evicted")
	}
}

func TestSearchEndpointRequiresMetadata(t *testing.T) {
	t.Setenv("SEARCH_METADATA_PATH", t.TempDir()+"/missing.bin")
	s := NewServer(grid.NewTestGrid(2, 2), nil, nil, nil, 9)
	req := httptest.NewRequest(http.MethodPost, "/api/search", strings.NewReader(`{"prompt":"places like Tuscany"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestSearchContextDoesNotAcceptClientCoordinates(t *testing.T) {
	var state searchContext
	if err := json.Unmarshal([]byte(`{"reference":{"name":"Tuscany","lat":91,"lng":-181}}`), &state); err != nil {
		t.Fatal(err)
	}
	if state.Reference == nil || state.Reference.Name != "Tuscany" || state.Reference.Lat != 0 || state.Reference.Lng != 0 {
		t.Fatalf("client coordinates were trusted: %#v", state.Reference)
	}
}

func TestChooseGeonamesResultClarifiesAmbiguousPlaces(t *testing.T) {
	results := []geonamesResult{
		{Name: "Springfield", CountryCode: "US", CountryName: "United States", AdminName1: "Illinois", Lat: "39.80", Lng: "-89.64"},
		{Name: "Springfield", CountryCode: "US", CountryName: "United States", AdminName1: "Missouri", Lat: "37.21", Lng: "-93.29"},
	}
	if _, err := chooseGeonamesResult("Springfield", results); err == nil {
		t.Fatal("expected ambiguity to require clarification")
	}
}

func TestChooseGeonamesResultPrefersBroadAdministrativeRegion(t *testing.T) {
	results := []geonamesResult{
		{Name: "Tuscany", CountryCode: "IT", CountryName: "Italy", FeatureCode: "ADM1", Lat: "43", Lng: "11"},
		{Name: "Tuscany", CountryCode: "IT", CountryName: "Italy", FeatureCode: "PPL", Lat: "43.7", Lng: "10.4"},
	}
	result, err := chooseGeonamesResult("Tuscany", results)
	if err != nil || result.FeatureCode != "ADM1" {
		t.Fatalf("got %v, %v", result, err)
	}
}

func TestDecodeParsedSearchValidatesStructuredOutput(t *testing.T) {
	valid := `{"reference_place":"Tuscany","countries":{"include":[],"exclude":[]},"continents":{"include":["South America"],"exclude":[]},"temperature":{"comparison":"warmer","min_c":null,"max_c":null},"clarification":"","unsupported":[]}`
	parsed, err := decodeParsedSearch(valid)
	if err != nil || parsed.ReferencePlace != "Tuscany" || parsed.Continents.Include[0] != "South America" {
		t.Fatalf("got %#v, %v", parsed, err)
	}
	if _, err := decodeParsedSearch(strings.TrimSuffix(valid, `}`) + `,"lat":40}`); err == nil {
		t.Fatal("expected model-supplied coordinate to be rejected")
	}
}

func TestValidateSearchStateRejectsInvalidTemperatureRange(t *testing.T) {
	min, max := 20.0, 10.0
	state := &searchContext{Temperature: &searchTemperature{MinC: &min, MaxC: &max}}
	if err := validateSearchState(state); err == nil {
		t.Fatal("expected inverted temperature range to be rejected")
	}
}

func TestClarificationOnlyWithoutReferencePlace(t *testing.T) {
	// "like Atacama" is embedding similarity: a reference place means search,
	// even if the model also asked something or flagged part of the prompt.
	withRef := parsedSearch{ReferencePlace: "Atacama Desert", Clarification: "Similar in climate or terrain?", Unsupported: []string{"similar to Atacama"}}
	if q, ask := clarificationFor(withRef, nil); ask {
		t.Fatalf("asked %q despite a reference place", q)
	}
	// A reference kept from an earlier turn counts too.
	current := &searchContext{Reference: &searchReference{Name: "Tuscany"}}
	if _, ask := clarificationFor(parsedSearch{Unsupported: []string{"wine"}}, current); ask {
		t.Fatal("asked despite a reference in the current context")
	}
	if q, ask := clarificationFor(parsedSearch{Clarification: "Which place?"}, nil); !ask || q != "Which place?" {
		t.Fatalf("got %q, %v; want the model's question", q, ask)
	}
	if q, ask := clarificationFor(parsedSearch{}, nil); !ask || q == "" {
		t.Fatalf("got %q, %v; want a default question", q, ask)
	}
}

func TestExplainSearchListsIgnoredConstraints(t *testing.T) {
	state := &searchContext{Reference: &searchReference{Name: "Tuscany"}}
	got := explainSearch(state, "Tuscany", []string{"good wine", "low rainfall"})
	if !strings.Contains(got, "not applied: good wine, low rainfall") {
		t.Fatalf("explanation %q does not list ignored constraints", got)
	}
	if got := explainSearch(state, "Tuscany", nil); strings.Contains(got, "not applied") {
		t.Fatalf("explanation %q mentions ignored constraints when there are none", got)
	}
}

func TestChooseGeonamesResultTrustsTopRankedDistinctPlace(t *testing.T) {
	results := []geonamesResult{
		{Name: "Scottish Highlands", CountryCode: "GB", CountryName: "United Kingdom", FeatureClass: "L", FeatureCode: "RGN"},
		{Name: "Scottish Highlands", CountryCode: "US", CountryName: "United States", AdminName1: "Utah", FeatureClass: "P", FeatureCode: "PPL"},
		{Name: "Scottish Highlands", CountryCode: "US", CountryName: "United States", AdminName1: "Tennessee", FeatureClass: "P", FeatureCode: "PPL"},
	}
	result, err := chooseGeonamesResult("Scottish Highlands", results)
	if err != nil || result.CountryCode != "GB" {
		t.Fatalf("got %v, %v; want the Scottish region", result, err)
	}
	// A city far larger than a same-named town is not ambiguous either.
	cities := []geonamesResult{
		{Name: "Paris", CountryCode: "FR", FeatureClass: "P", Population: 2_138_551},
		{Name: "Paris", CountryCode: "US", AdminName1: "Texas", FeatureClass: "P", Population: 24_476},
	}
	if result, err := chooseGeonamesResult("Paris", cities); err != nil || result.CountryCode != "FR" {
		t.Fatalf("got %v, %v; want Paris, France", result, err)
	}
}

func TestChooseGeonamesResultPicksCountry(t *testing.T) {
	results := []geonamesResult{
		{Name: "Iceland", CountryCode: "IS", CountryName: "Iceland", FeatureClass: "A", FeatureCode: "PCLI"},
		{Name: "Iceland", CountryCode: "US", AdminName1: "Nebraska", FeatureClass: "P", FeatureCode: "PPL"},
	}
	if result, err := chooseGeonamesResult("Iceland", results); err != nil || result.CountryCode != "IS" {
		t.Fatalf("got %v, %v; want the country", result, err)
	}
}

func TestChooseGeonamesResultPrefersExactName(t *testing.T) {
	results := []geonamesResult{
		{Name: "Viana do Alentejo Municipality", CountryCode: "PT", FeatureClass: "A", FeatureCode: "ADM2"},
		{Name: "Alentejo", CountryCode: "PT", FeatureClass: "L", FeatureCode: "RGN"},
	}
	if result, err := chooseGeonamesResult("Alentejo", results); err != nil || result.Name != "Alentejo" {
		t.Fatalf("got %v, %v; want the Alentejo region", result, err)
	}
}

func TestSearchThemesUseGalleryMapsWithEnoughPins(t *testing.T) {
	pins := func(n int) []ReferencePin { return make([]ReferencePin, n) }
	s := &Server{layerMeta: map[string]LayerMeta{
		"mangroves": {Name: "Mangroves", Pins: pins(10)},
		"tea":       {Name: "Tea", Pins: pins(2)},
		"coffee":    {Name: "Specialty Coffee", Pins: pins(24)},
	}}
	themes := s.searchThemes()
	if len(themes) != 2 || themes[0].ID != "coffee" || themes[1].ID != "mangroves" {
		t.Fatalf("got themes %+v, want coffee and mangroves only", themes)
	}
	if findTheme(themes, "tea") != nil {
		t.Fatal("a map with 2 pins should not be a theme")
	}
}

func TestMergeSearchContextSwitchesToTheme(t *testing.T) {
	themes := []searchTheme{{ID: "mangroves", Name: "Mangroves"}}
	current := &searchContext{Reference: &searchReference{Name: "Tuscany"}, Continents: searchRegions{Include: []string{"Africa"}}}
	state, err := mergeSearchContext(current, parsedSearch{Theme: "mangroves"}, themes)
	if err != nil {
		t.Fatal(err)
	}
	if state.Theme != "mangroves" || state.Reference != nil {
		t.Fatalf("got theme %q, reference %v; want the theme to replace the place", state.Theme, state.Reference)
	}
	if len(state.Continents.Include) != 1 {
		t.Fatal("region filters from the earlier turn were dropped")
	}
	if _, err := mergeSearchContext(nil, parsedSearch{Theme: "unicorns"}, themes); err == nil {
		t.Fatal("expected an unknown theme to be rejected")
	}
}

func TestClarificationSkippedForTheme(t *testing.T) {
	if q, ask := clarificationFor(parsedSearch{Theme: "glaciers", Clarification: "Which place?"}, nil); ask {
		t.Fatalf("asked %q despite a theme", q)
	}
	if _, ask := clarificationFor(parsedSearch{}, &searchContext{Theme: "glaciers"}); ask {
		t.Fatal("asked despite a theme in the current context")
	}
}

func TestNameMatchesOffline(t *testing.T) {
	dir := t.TempDir()
	dump := "1\tHualañé\tHualane\t\t-34.97\t-71.80\tP\tPPL\tCL\t\t07\n"
	if err := os.WriteFile(filepath.Join(dir, "cities.txt"), []byte(dump), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "countryInfo.txt"), []byte("CL\tCHL\t152\tCI\tChile\n"), 0600); err != nil {
		t.Fatal(err)
	}
	g, err := gazetteer.Load(filepath.Join(dir, "cities.txt"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{gazetteer: g}
	names := s.nameMatchesOffline([]similarity.TopMatch{{Lat: -34.98, Lng: -71.81}, {Lat: -35.5, Lng: -71.8}})
	if names[0] != "Hualañé, Chile" {
		t.Fatalf("near match named %q", names[0])
	}
	if !strings.HasPrefix(names[1], "59 km from Hualañé") {
		t.Fatalf("far match named %q, want a distance to the nearest town", names[1])
	}
}

func TestChooseGeonamesResultMergesNearbyDuplicates(t *testing.T) {
	results := []geonamesResult{
		{Name: "Sundarbans", CountryCode: "BD", FeatureClass: "L", Lat: "21.95", Lng: "89.18"},
		{Name: "Sundarbans", CountryCode: "BD", AdminName1: "Khulna Division", FeatureClass: "L", Lat: "22.10", Lng: "89.40"},
		{Name: "Sundarbans", CountryCode: "BD", AdminName1: "Khulna Division", FeatureClass: "L", Lat: "21.80", Lng: "89.60"},
	}
	if result, err := chooseGeonamesResult("Sundarbans", results); err != nil || result.Lat != "21.95" {
		t.Fatalf("got %v, %v; want the first Sundarbans", result, err)
	}
}
