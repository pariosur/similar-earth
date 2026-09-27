package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/pariosur/tierraai/internal/similarity"
)

type searchContext struct {
	Reference   *searchReference   `json:"reference,omitempty"`
	Countries   searchRegions      `json:"countries,omitempty"`
	Continents  searchRegions      `json:"continents,omitempty"`
	Temperature *searchTemperature `json:"temperature,omitempty"`
}

type searchReference struct {
	Name string  `json:"name"`
	Lat  float64 `json:"-"`
	Lng  float64 `json:"-"`
}

type searchRegions struct {
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

type searchTemperature struct {
	Comparison string   `json:"comparison,omitempty"`
	MinC       *float64 `json:"min_c,omitempty"`
	MaxC       *float64 `json:"max_c,omitempty"`
}

type naturalSearchRequest struct {
	Prompt  string         `json:"prompt"`
	Context *searchContext `json:"context,omitempty"`
}

type parsedSearch struct {
	ReferencePlace string            `json:"reference_place"`
	Countries      searchRegions     `json:"countries"`
	Continents     searchRegions     `json:"continents"`
	Temperature    searchTemperature `json:"temperature"`
	Clarification  string            `json:"clarification"`
	Unsupported    []string          `json:"unsupported"`
}

// Search results are spread out so one region doesn't take all ten slots.
const (
	searchResultSpacingKm    = 300
	searchReferenceExcludeKm = 100
	searchMaxPerCountry      = 3
)

type geonamesResult struct {
	Name         string `json:"name"`
	Population   int64  `json:"population"`
	FeatureClass string `json:"fcl"`
	Lat          string `json:"lat"`
	Lng          string `json:"lng"`
	CountryName  string `json:"countryName"`
	CountryCode  string `json:"countryCode"`
	AdminName1   string `json:"adminName1"`
	FeatureCode  string `json:"fcode"`
}

type geonamesResponse struct {
	Results []geonamesResult `json:"geonames"`
}

var (
	searchDailyCount atomic.Int64
	searchDailyDate  atomic.Int64
)

func naturalSearchDailyAllowed() bool {
	day := time.Now().Unix() / 86400
	if searchDailyDate.Load() != day {
		searchDailyDate.Store(day)
		searchDailyCount.Store(0)
	}
	limit := int64(500)
	if value, err := strconv.ParseInt(os.Getenv("SEARCH_DAILY_LIMIT"), 10, 64); err == nil && value > 0 {
		limit = value
	}
	return searchDailyCount.Add(1) <= limit
}

func (s *Server) handleNaturalSearch(c *fiber.Ctx) error {
	if len(c.Body()) > 4096 {
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{"error": "search request must be under 4 KB"})
	}
	if s.searchData == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "search datasets are not installed"})
	}
	select {
	case s.searchSlots <- struct{}{}:
		defer func() { <-s.searchSlots }()
	default:
		return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "a search is already running; try again shortly"})
	}
	var req naturalSearchRequest
	if err := c.BodyParser(&req); err != nil || strings.TrimSpace(req.Prompt) == "" || len(req.Prompt) > 500 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "prompt must be between 1 and 500 characters"})
	}
	if req.Context != nil {
		if err := validateSearchState(req.Context); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		for _, country := range append(append([]string{}, req.Context.Countries.Include...), req.Context.Countries.Exclude...) {
			if !s.searchData.HasCountry(country) {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid country in search context"})
			}
		}
	}
	if !naturalSearchDailyAllowed() {
		return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "daily search limit reached"})
	}
	ctx := c.Context()
	parsed, err := parseSearchPrompt(ctx, req.Prompt, req.Context)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "could not interpret search; try rephrasing"})
	}
	if question, ask := clarificationFor(parsed, req.Context); ask {
		return c.JSON(fiber.Map{"clarification": question, "unsupported": parsed.Unsupported, "context": req.Context})
	}
	state, err := mergeSearchContext(req.Context, parsed)
	if err != nil {
		var ambiguous *ambiguousPlaceError
		if errors.As(err, &ambiguous) {
			return c.JSON(fiber.Map{"clarification": "Which place did you mean: " + strings.Join(ambiguous.names, "; ") + "?", "context": req.Context})
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	if state.Reference == nil {
		return c.JSON(fiber.Map{"clarification": "Which place should I use as the reference?", "context": state})
	}
	if err := validateSearchState(state); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	for _, country := range append(append([]string{}, state.Countries.Include...), state.Countries.Exclude...) {
		if !s.searchData.HasCountry(country) {
			return c.JSON(fiber.Map{"clarification": fmt.Sprintf("I couldn't identify the country %q. Which country did you mean?", country), "context": req.Context})
		}
	}

	includeCountries := s.searchData.CountryIDs(state.Countries.Include)
	excludeCountries := s.searchData.CountryIDs(state.Countries.Exclude)
	// Geocoded points often land in a gap of the land mask; use the nearest land pixel.
	refRow, refCol, ok := s.grid.NearestLand(state.Reference.Lat, state.Reference.Lng, similarity.PinSnapRadius)
	if !ok {
		return c.JSON(fiber.Map{"clarification": "I have no satellite data near that place. Try a nearby place.", "context": state})
	}
	state.Reference.Lat, state.Reference.Lng = s.grid.CellCenter(refRow, refCol)
	pins := []similarity.Pin{{Lat: state.Reference.Lat, Lng: state.Reference.Lng, Label: state.Reference.Name}}
	_, refTemp, refOK := s.searchData.At(refRow*int(s.grid.Width) + refCol)
	if state.Temperature.Comparison != "" && !refOK {
		return c.JSON(fiber.Map{"clarification": "I don't have annual temperature data for that reference. Try another place.", "context": state})
	}

	matches, err := s.engine.FindFilteredTopMatches(pins, 10, func(index int, _, _ float64) bool {
		country, temperature, ok := s.searchData.At(index)
		if !ok {
			return false
		}
		if len(includeCountries) > 0 {
			if _, ok := includeCountries[country.ID]; !ok {
				return false
			}
		}
		if _, ok := excludeCountries[country.ID]; ok {
			return false
		}
		if !containsFold(state.Continents.Include, country.Continent) && len(state.Continents.Include) > 0 {
			return false
		}
		if containsFold(state.Continents.Exclude, country.Continent) {
			return false
		}
		t := state.Temperature
		if t.MinC != nil && temperature < *t.MinC || t.MaxC != nil && temperature > *t.MaxC {
			return false
		}
		if refOK && t.Comparison == "warmer" && temperature <= refTemp || refOK && t.Comparison == "colder" && temperature >= refTemp {
			return false
		}
		return true
	}, &similarity.Diversity{
		MinDistanceKm: searchResultSpacingKm,
		ExcludeKm:     searchReferenceExcludeKm,
		MaxPerGroup:   searchMaxPerCountry,
		Group: func(index int) int {
			country, _, _ := s.searchData.At(index)
			return int(country.ID)
		},
	})
	if err != nil {
		return c.JSON(fiber.Map{"clarification": "I have no satellite data near that place. Try a nearby place.", "context": state})
	}
	if len(matches) < 5 {
		return c.JSON(fiber.Map{"clarification": "Those filters leave fewer than five matches. Broaden the region or temperature filters.", "context": state})
	}
	names := s.reverseGeocodeMatches(ctx, matches)
	out := make([]fiber.Map, 0, len(matches))
	for i, match := range matches {
		row, col, _ := s.grid.LatLngToRowCol(match.Lat, match.Lng)
		_, temperature, _ := s.searchData.At(row*int(s.grid.Width) + col)
		item := fiber.Map{"lat": match.Lat, "lng": match.Lng, "name": names[i], "score": match.Score}
		if refOK && (state.Temperature.Comparison != "" || state.Temperature.MinC != nil || state.Temperature.MaxC != nil) {
			item["temperature_difference_c"] = math.Round((temperature-refTemp)*10) / 10
		}
		out = append(out, item)
	}
	// The context never carries coordinates (clients can't inject them), so the
	// geocoded reference is returned separately for drawing it on the map.
	reference := fiber.Map{"name": state.Reference.Name, "lat": state.Reference.Lat, "lng": state.Reference.Lng}
	return c.JSON(fiber.Map{"matches": out, "reference": reference, "context": state, "explanation": explainSearch(state, parsed.Unsupported)})
}

func parseSearchPrompt(ctx context.Context, prompt string, state *searchContext) (parsedSearch, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return parsedSearch{}, fmt.Errorf("OPENAI_API_KEY is not configured")
	}
	input, _ := json.Marshal(fiber.Map{"prompt": prompt, "current_state": state})
	body := fiber.Map{
		"model": "gpt-6-luna", "max_completion_tokens": 300,
		"messages": []fiber.Map{
			{"role": "system", "content": "You turn a place-search prompt into filters for a satellite-similarity search. Every search ranks places by how closely their Google AlphaEarth satellite embeddings match one reference place, so \"like\", \"similar to\", \"resembling\" or \"looks like\" X always means X is reference_place: never ask what similarity means and never list it as unsupported. Fields: reference_place (the place as the user names it, e.g. \"Atacama Desert\"); included/excluded country names or continents; annual mean temperature bounds in Celsius; warmer/colder than the reference. current_state holds earlier turns: keep its values unless the prompt changes them, and leave reference_place empty to keep its reference. Put other constraints you cannot express with these fields (rainfall, soil, population, ...) in unsupported. Use clarification only when neither the prompt nor current_state names a reference place. Never return coordinates, scores, or explanations. Do not invent values. Return strict JSON matching the schema."},
			{"role": "user", "content": string(input)},
		},
		"response_format": fiber.Map{"type": "json_schema", "json_schema": fiber.Map{
			"name": "search_filters", "strict": true,
			"schema": fiber.Map{"type": "object", "additionalProperties": false, "required": []string{"reference_place", "countries", "continents", "temperature", "clarification", "unsupported"}, "properties": fiber.Map{
				"reference_place": fiber.Map{"type": "string"},
				"countries":       regionSchema(), "continents": regionSchema(),
				"temperature": fiber.Map{"type": "object", "additionalProperties": false, "required": []string{"comparison", "min_c", "max_c"}, "properties": fiber.Map{
					"comparison": fiber.Map{"type": "string", "enum": []string{"", "warmer", "colder"}},
					"min_c":      fiber.Map{"type": []string{"number", "null"}}, "max_c": fiber.Map{"type": []string{"number", "null"}},
				}},
				"clarification": fiber.Map{"type": "string"}, "unsupported": fiber.Map{"type": "array", "items": fiber.Map{"type": "string"}},
			}},
		}},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return parsedSearch{}, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewReader(encoded))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return parsedSearch{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return parsedSearch{}, fmt.Errorf("model returned %s", resp.Status)
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&result); err != nil || len(result.Choices) != 1 {
		return parsedSearch{}, fmt.Errorf("invalid model response")
	}
	return decodeParsedSearch(result.Choices[0].Message.Content)
}

func decodeParsedSearch(content string) (parsedSearch, error) {
	var parsed parsedSearch
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parsed); err != nil {
		return parsedSearch{}, err
	}
	if len(parsed.Unsupported) > 10 || len(parsed.ReferencePlace) > 200 {
		return parsedSearch{}, fmt.Errorf("model output exceeds limits")
	}
	if err := validateSearchState(&searchContext{Countries: parsed.Countries, Continents: parsed.Continents, Temperature: &parsed.Temperature}); err != nil {
		return parsedSearch{}, err
	}
	return parsed, nil
}

func regionSchema() fiber.Map {
	return fiber.Map{"type": "object", "additionalProperties": false, "required": []string{"include", "exclude"}, "properties": fiber.Map{
		"include": fiber.Map{"type": "array", "items": fiber.Map{"type": "string"}}, "exclude": fiber.Map{"type": "array", "items": fiber.Map{"type": "string"}},
	}}
}

func mergeSearchContext(current *searchContext, parsed parsedSearch) (*searchContext, error) {
	state := &searchContext{}
	if current != nil {
		*state = *current
	}
	placeName := parsed.ReferencePlace
	if placeName == "" && state.Reference != nil {
		placeName = state.Reference.Name
	}
	if placeName != "" {
		place, err := geocodeReference(placeName)
		if err != nil {
			return nil, err
		}
		state.Reference = place
	}
	state.Countries = mergeRegions(state.Countries, parsed.Countries)
	state.Continents = mergeRegions(state.Continents, parsed.Continents)
	if parsed.Temperature.Comparison != "" || parsed.Temperature.MinC != nil || parsed.Temperature.MaxC != nil {
		state.Temperature = &parsed.Temperature
	}
	if state.Temperature == nil {
		state.Temperature = &searchTemperature{}
	}
	return state, nil
}

func validateSearchState(state *searchContext) error {
	if state.Reference != nil && (strings.TrimSpace(state.Reference.Name) == "" || len(state.Reference.Name) > 200) {
		return fmt.Errorf("invalid reference place")
	}
	if state.Temperature != nil {
		if state.Temperature.Comparison != "" && state.Temperature.Comparison != "warmer" && state.Temperature.Comparison != "colder" {
			return fmt.Errorf("invalid temperature comparison")
		}
		for _, value := range []*float64{state.Temperature.MinC, state.Temperature.MaxC} {
			if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < -90 || *value > 60) {
				return fmt.Errorf("temperature must be between -90 and 60°C")
			}
		}
		if state.Temperature.MinC != nil && state.Temperature.MaxC != nil && *state.Temperature.MinC > *state.Temperature.MaxC {
			return fmt.Errorf("minimum temperature must not exceed maximum temperature")
		}
	}
	regions := [][]string{state.Countries.Include, state.Countries.Exclude, state.Continents.Include, state.Continents.Exclude}
	count := 0
	for _, values := range regions {
		count += len(values)
		for _, value := range values {
			if strings.TrimSpace(value) == "" || len(value) > 100 {
				return fmt.Errorf("invalid region filter")
			}
		}
	}
	if count > 20 {
		return fmt.Errorf("too many region filters")
	}
	continents := []string{"Africa", "Asia", "Europe", "North America", "South America", "Oceania", "Antarctica"}
	for _, name := range append(append([]string{}, state.Continents.Include...), state.Continents.Exclude...) {
		if !containsFold(continents, name) {
			return fmt.Errorf("unknown continent %q", name)
		}
	}
	return nil
}

func mergeRegions(old, next searchRegions) searchRegions {
	return searchRegions{Include: appendUnique(old.Include, next.Include...), Exclude: appendUnique(old.Exclude, next.Exclude...)}
}

func appendUnique(values []string, add ...string) []string {
	out := append([]string{}, values...)
	for _, value := range add {
		if !containsFold(out, value) {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

func containsFold(values []string, item string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(item)) {
			return true
		}
	}
	return false
}

func geocodeReference(query string) (*searchReference, error) {
	username := os.Getenv("GEONAMES_USERNAME")
	if username == "" {
		return nil, fmt.Errorf("GEONAMES_USERNAME is not configured")
	}
	u := "https://secure.geonames.org/searchJSON?q=" + url.QueryEscape(query) + "&maxRows=5&style=FULL&username=" + url.QueryEscape(username)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geocoder returned %s", resp.Status)
	}
	var data geonamesResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&data); err != nil {
		return nil, err
	}
	if len(data.Results) == 0 {
		return nil, fmt.Errorf("place not found")
	}
	selected, err := chooseGeonamesResult(query, data.Results)
	if err != nil {
		return nil, err
	}
	var latValue, lngValue float64
	if _, err := fmt.Sscan(selected.Lat, &latValue); err != nil {
		return nil, err
	}
	if _, err := fmt.Sscan(selected.Lng, &lngValue); err != nil {
		return nil, err
	}
	return &searchReference{Name: geonamesLabel(*selected), Lat: latValue, Lng: lngValue}, nil
}

type ambiguousPlaceError struct{ names []string }

func (e *ambiguousPlaceError) Error() string { return "ambiguous place" }

// chooseGeonamesResult picks the place a search refers to. GeoNames ranks
// results by relevance, so the first one wins unless another result with the
// same name is a comparable place: the same kind of feature and a similar
// population (Springfield, IL vs Springfield, MO). A region such as the
// Scottish Highlands doesn't compete with small US towns of that name.
func chooseGeonamesResult(query string, results []geonamesResult) (*geonamesResult, error) {
	var regions []geonamesResult
	for _, result := range results {
		if result.FeatureCode == "ADM1" || strings.HasPrefix(result.FeatureCode, "PCL") {
			regions = append(regions, result)
		}
	}
	if len(regions) == 1 {
		return &regions[0], nil
	}
	if len(regions) > 1 {
		return nil, &ambiguousPlaceError{names: geonamesLabels(regions)}
	}

	top := results[0]
	rivals := []geonamesResult{top}
	for _, result := range results[1:] {
		if strings.EqualFold(strings.TrimSpace(result.Name), strings.TrimSpace(top.Name)) &&
			result.FeatureClass == top.FeatureClass && comparablePopulation(result.Population, top.Population) {
			rivals = append(rivals, result)
		}
	}
	if len(rivals) > 1 {
		return nil, &ambiguousPlaceError{names: geonamesLabels(rivals)}
	}
	return &top, nil
}

// comparablePopulation reports whether two places are within 4x of each other
// in population; places without a population count as comparable.
func comparablePopulation(a, b int64) bool {
	if a == 0 || b == 0 {
		return a == b
	}
	return min(a, b)*4 >= max(a, b)
}

func geonamesLabels(results []geonamesResult) []string {
	labels := make([]string, 0, len(results))
	for _, result := range results {
		labels = append(labels, geonamesLabel(result))
	}
	return labels
}

func geonamesLabel(place geonamesResult) string {
	parts := []string{place.Name}
	if place.AdminName1 != "" && place.AdminName1 != place.Name {
		parts = append(parts, place.AdminName1)
	}
	if place.CountryName != "" {
		parts = append(parts, place.CountryName)
	}
	return strings.Join(parts, ", ")
}

func (s *Server) reverseGeocodeMatches(ctx context.Context, matches []similarity.TopMatch) []string {
	names := make([]string, len(matches))
	username := os.Getenv("GEONAMES_USERNAME")
	if username == "" {
		return names
	}
	ctx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer cancel()
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)
	client := &http.Client{Timeout: 1200 * time.Millisecond}
	for i, match := range matches {
		row, col, ok := s.grid.LatLngToRowCol(match.Lat, match.Lng)
		if ok {
			country, _, _ := s.searchData.At(row*int(s.grid.Width) + col)
			names[i] = fmt.Sprintf("%.2f°, %.2f°, %s", match.Lat, match.Lng, country.Name)
		}
		wg.Add(1)
		go func(i int, match similarity.TopMatch) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			u := fmt.Sprintf("https://secure.geonames.org/findNearbyPlaceNameJSON?lat=%f&lng=%f&maxRows=1&username=%s", match.Lat, match.Lng, url.QueryEscape(username))
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
			if err != nil {
				return
			}
			resp, err := client.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return
			}
			var result geonamesResponse
			if json.NewDecoder(io.LimitReader(resp.Body, 16*1024)).Decode(&result) == nil && len(result.Results) > 0 {
				names[i] = geonamesLabel(result.Results[0])
			}
		}(i, match)
	}
	wg.Wait()
	return names
}

func explainSearch(state *searchContext, ignored []string) string {
	parts := []string{"Ranked by satellite similarity"}
	if len(state.Countries.Include) > 0 {
		parts = append(parts, "included countries: "+strings.Join(state.Countries.Include, ", "))
	}
	if len(state.Countries.Exclude) > 0 {
		parts = append(parts, "excluded countries: "+strings.Join(state.Countries.Exclude, ", "))
	}
	if len(state.Continents.Include) > 0 {
		parts = append(parts, "included continents: "+strings.Join(state.Continents.Include, ", "))
	}
	if len(state.Continents.Exclude) > 0 {
		parts = append(parts, "excluded continents: "+strings.Join(state.Continents.Exclude, ", "))
	}
	if state.Temperature != nil {
		if state.Temperature.Comparison != "" {
			parts = append(parts, state.Temperature.Comparison+" than "+state.Reference.Name+" annual mean")
		}
		if state.Temperature.MinC != nil {
			parts = append(parts, fmt.Sprintf("annual mean at least %.1f°C", *state.Temperature.MinC))
		}
		if state.Temperature.MaxC != nil {
			parts = append(parts, fmt.Sprintf("annual mean at most %.1f°C", *state.Temperature.MaxC))
		}
	}
	if len(ignored) > 0 {
		parts = append(parts, "not applied: "+strings.Join(ignored, ", "))
	}
	return strings.Join(parts, "; ") + "."
}

// clarificationFor decides whether to ask instead of searching. Similarity always
// means AlphaEarth embedding similarity, so once there is a reference place the
// search runs; anything the model couldn't map to a filter is listed in the
// explanation rather than blocking. Ambiguous place names are caught later by
// the geocoder.
func clarificationFor(parsed parsedSearch, current *searchContext) (string, bool) {
	if parsed.ReferencePlace != "" || current != nil && current.Reference != nil {
		return "", false
	}
	if parsed.Clarification != "" {
		return parsed.Clarification, true
	}
	return "Which place should the results look like?", true
}
