package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pariosur/tierraai/internal/similarity"

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
		{Name: "Springfield", CountryCode: "US", CountryName: "United States", AdminName1: "Illinois"},
		{Name: "Springfield", CountryCode: "US", CountryName: "United States", AdminName1: "Missouri"},
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
	got := explainSearch(state, []string{"good wine", "low rainfall"})
	if !strings.Contains(got, "not applied: good wine, low rainfall") {
		t.Fatalf("explanation %q does not list ignored constraints", got)
	}
	if got := explainSearch(state, nil); strings.Contains(got, "not applied") {
		t.Fatalf("explanation %q mentions ignored constraints when there are none", got)
	}
}
