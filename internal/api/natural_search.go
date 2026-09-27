package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
	Theme       string             `json:"theme,omitempty"` // gallery map ID; used instead of a reference place
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
	Theme          string            `json:"theme"`
	Countries      searchRegions     `json:"countries"`
	Continents     searchRegions     `json:"continents"`
	Temperature    searchTemperature `json:"temperature"`
	Clarification  string            `json:"clarification"`
	Unsupported    []string          `json:"unsupported"`
}

// Search results are spread out so one region doesn't take all ten slots.
const (
	searchResultCount        = 20
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
	// Set instead of results on errors, e.g. when the hourly credit limit is used up
	Status *struct {
		Message string `json:"message"`
	} `json:"status"`
}

// errGeocoderUnavailable means GeoNames refused the lookup (credit limit, outage),
// as opposed to finding no such place.
var errGeocoderUnavailable = errors.New("place lookup is temporarily unavailable")

var (
	placeNameCache   sync.Map // "lat,lng" rounded to 0.01° -> GeoNames place label
	referenceCache   sync.Map // lowercased query -> searchReference
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
	themes := s.searchThemes()
	var req naturalSearchRequest
	if err := c.BodyParser(&req); err != nil || strings.TrimSpace(req.Prompt) == "" || len(req.Prompt) > 500 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "prompt must be between 1 and 500 characters"})
	}
	if req.Context != nil {
		if err := validateSearchState(req.Context); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		if req.Context.Theme != "" && findTheme(themes, req.Context.Theme) == nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid search theme"})
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
	parsed, err := parseSearchPrompt(ctx, req.Prompt, req.Context, themes)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "could not interpret search; try rephrasing"})
	}
	if question, ask := clarificationFor(parsed, req.Context); ask {
		return c.JSON(fiber.Map{"clarification": question, "unsupported": parsed.Unsupported, "context": req.Context})
	}
	state, err := mergeSearchContext(req.Context, parsed, themes)
	if err != nil {
		var ambiguous *ambiguousPlaceError
		if errors.As(err, &ambiguous) {
			return c.JSON(fiber.Map{"clarification": "Which place did you mean: " + strings.Join(ambiguous.names, "; ") + "?", "context": req.Context})
		}
		if errors.Is(err, errGeocoderUnavailable) {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Place lookup is busy right now. Try again in a few minutes, or search for a kind of place like \"mangrove coasts\"."})
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	if state.Reference == nil && state.Theme == "" {
		return c.JSON(fiber.Map{"clarification": "Which place should the results look like?", "context": state})
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
	// The reference is a named place or a theme's curated sites. Points often land
	// in a gap of the land mask, so each snaps to the nearest land pixel.
	theme := findTheme(themes, state.Theme)
	var pins []similarity.Pin
	var refTemps []float64
	addPin := func(lat, lng float64, label string) bool {
		row, col, ok := s.grid.NearestLand(lat, lng, similarity.PinSnapRadius)
		if !ok {
			return false
		}
		lat, lng = s.grid.CellCenter(row, col)
		pins = append(pins, similarity.Pin{Lat: lat, Lng: lng, Label: label})
		if _, temp, ok := s.searchData.At(row*int(s.grid.Width) + col); ok {
			refTemps = append(refTemps, temp)
		}
		return true
	}
	if theme != nil {
		for _, pin := range theme.Pins {
			addPin(pin.Lat, pin.Lng, pin.Label)
		}
	} else if addPin(state.Reference.Lat, state.Reference.Lng, state.Reference.Name) {
		state.Reference.Lat, state.Reference.Lng = pins[0].Lat, pins[0].Lng
	}
	if len(pins) == 0 {
		return c.JSON(fiber.Map{"clarification": "I have no satellite data near that place. Try a nearby place.", "context": state})
	}
	// Warmer/colder compares against the reference, or the mean of a theme's sites.
	refTemp, refOK := 0.0, len(refTemps) > 0
	for _, temp := range refTemps {
		refTemp += temp / float64(len(refTemps))
	}
	if state.Temperature.Comparison != "" && !refOK {
		return c.JSON(fiber.Map{"clarification": "I don't have annual temperature data for that reference. Try another place.", "context": state})
	}

	matches, err := s.engine.FindFilteredTopMatches(pins, searchResultCount, func(index int, _, _ float64) bool {
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
		if theme != nil && match.BestPinIndex < len(pins) {
			item["similar_to"] = pins[match.BestPinIndex].Label
		}
		if refOK && (state.Temperature.Comparison != "" || state.Temperature.MinC != nil || state.Temperature.MaxC != nil) {
			item["temperature_difference_c"] = math.Round((temperature-refTemp)*10) / 10
		}
		out = append(out, item)
	}
	// The context never carries coordinates (clients can't inject them), so the
	// reference points are returned separately for drawing them on the map.
	references := make([]fiber.Map, len(pins))
	for i, pin := range pins {
		references[i] = fiber.Map{"name": pin.Label, "lat": pin.Lat, "lng": pin.Lng}
	}
	response := fiber.Map{"matches": out, "references": references, "context": state}
	refLabel := ""
	if theme != nil {
		response["theme"] = fiber.Map{"id": theme.ID, "name": theme.Name}
		refLabel = "the " + theme.Name + " reference sites"
	} else {
		response["reference"] = references[0]
		refLabel = state.Reference.Name
	}
	response["explanation"] = explainSearch(state, refLabel, parsed.Unsupported)
	return c.JSON(response)
}

func parseSearchPrompt(ctx context.Context, prompt string, state *searchContext, themes []searchTheme) (parsedSearch, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return parsedSearch{}, fmt.Errorf("OPENAI_API_KEY is not configured")
	}
	input, _ := json.Marshal(fiber.Map{"prompt": prompt, "current_state": state})
	themeIDs := []string{""}
	themeList := make([]string, 0, len(themes))
	for _, theme := range themes {
		themeIDs = append(themeIDs, theme.ID)
		themeList = append(themeList, theme.ID+" ("+theme.Name+")")
	}
	body := fiber.Map{
		"model": "gpt-6-luna", "max_completion_tokens": 300,
		"messages": []fiber.Map{
			{"role": "system", "content": "You turn a place-search prompt into filters for a satellite-similarity search. Every search ranks places by how closely their Google AlphaEarth satellite embeddings match a reference, so \"like\", \"similar to\", \"resembling\" or \"looks like\" X always means X is the reference: never ask what similarity means and never list it as unsupported. The reference is either reference_place, a specific named place as the user names it (\"Atacama Desert\", \"the Sundarbans\"), or theme, when the prompt asks for a kind of place instead: an ecosystem, landform, crop or land use such as \"mangroves\", \"glacier country\", \"rice-growing land\" or \"coffee country\". Set theme to the closest id from this list and leave reference_place empty; if no theme fits, put the request in unsupported. Themes: " + strings.Join(themeList, ", ") + ". Other fields: included/excluded country names or continents; annual mean temperature bounds in Celsius; warmer/colder than the reference. current_state holds earlier turns: keep its values unless the prompt changes them, and leave reference_place and theme empty to keep its reference or theme. Put other constraints you cannot express with these fields (rainfall, soil, population, ...) in unsupported. Use clarification only when neither the prompt nor current_state gives a reference place or theme. Never return coordinates, scores, or explanations. Do not invent values. Return strict JSON matching the schema."},
			{"role": "user", "content": string(input)},
		},
		"response_format": fiber.Map{"type": "json_schema", "json_schema": fiber.Map{
			"name": "search_filters", "strict": true,
			"schema": fiber.Map{"type": "object", "additionalProperties": false, "required": []string{"reference_place", "theme", "countries", "continents", "temperature", "clarification", "unsupported"}, "properties": fiber.Map{
				"reference_place": fiber.Map{"type": "string"},
				"theme":           fiber.Map{"type": "string", "enum": themeIDs},
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

func mergeSearchContext(current *searchContext, parsed parsedSearch, themes []searchTheme) (*searchContext, error) {
	state := &searchContext{}
	if current != nil {
		*state = *current
	}
	// A new theme or place replaces the other; otherwise the current reference
	// is geocoded again, since coordinates never round-trip through the client.
	switch {
	case parsed.Theme != "":
		if findTheme(themes, parsed.Theme) == nil {
			return nil, fmt.Errorf("unknown search theme")
		}
		state.Theme, state.Reference = parsed.Theme, nil
	case parsed.ReferencePlace != "" || state.Reference != nil:
		name := parsed.ReferencePlace
		if name == "" {
			name = state.Reference.Name
		}
		place, err := geocodeReference(name)
		if err != nil {
			return nil, err
		}
		state.Reference, state.Theme = place, ""
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
	// Popular references ("Tuscany") repeat; each GeoNames lookup costs a credit.
	cacheKey := strings.ToLower(strings.TrimSpace(query))
	if cached, ok := referenceCache.Load(cacheKey); ok {
		ref := cached.(searchReference)
		return &ref, nil
	}
	username := os.Getenv("GEONAMES_USERNAME")
	if username == "" {
		return nil, fmt.Errorf("GEONAMES_USERNAME is not configured")
	}
	u := "https://secure.geonames.org/searchJSON?q=" + url.QueryEscape(query) + "&maxRows=5&style=FULL&username=" + url.QueryEscape(username)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		return nil, errGeocoderUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errGeocoderUnavailable
	}
	var data geonamesResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&data); err != nil {
		return nil, err
	}
	if data.Status != nil {
		log.Printf("WARN: GeoNames search failed: %s", data.Status.Message)
		return nil, errGeocoderUnavailable
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
	ref := searchReference{Name: geonamesLabel(*selected), Lat: latValue, Lng: lngValue}
	referenceCache.Store(cacheKey, ref)
	return &ref, nil
}

type ambiguousPlaceError struct{ names []string }

func (e *ambiguousPlaceError) Error() string { return "ambiguous place" }

// chooseGeonamesResult picks the place a search refers to: the first exact name
// match, else GeoNames' top-ranked result. It wins unless another result with the
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

	// Prefer an exact name match ("Alentejo" over "Viana do Alentejo Municipality").
	top := results[0]
	for _, result := range results {
		if strings.EqualFold(strings.TrimSpace(result.Name), strings.TrimSpace(query)) {
			top = result
			break
		}
	}
	rivals := []geonamesResult{top}
	for _, result := range results {
		if result != top && strings.EqualFold(strings.TrimSpace(result.Name), strings.TrimSpace(top.Name)) &&
			result.FeatureClass == top.FeatureClass && comparablePopulation(result.Population, top.Population) &&
			geonamesDistanceKm(result, top) > distinctPlaceKm {
			rivals = append(rivals, result)
		}
	}
	if len(rivals) > 1 {
		return nil, &ambiguousPlaceError{names: geonamesLabels(rivals)}
	}
	return &top, nil
}

// Same-named results closer than this are one place listed several times
// (the Sundarbans as a region, a district and a park), not an ambiguity.
const distinctPlaceKm = 100

func geonamesDistanceKm(a, b geonamesResult) float64 {
	var latA, lngA, latB, lngB float64
	if _, err := fmt.Sscan(a.Lat, &latA); err != nil {
		return math.Inf(1)
	}
	if _, err := fmt.Sscan(a.Lng, &lngA); err != nil {
		return math.Inf(1)
	}
	if _, err := fmt.Sscan(b.Lat, &latB); err != nil {
		return math.Inf(1)
	}
	if _, err := fmt.Sscan(b.Lng, &lngB); err != nil {
		return math.Inf(1)
	}
	toRad := math.Pi / 180
	dLat, dLng := (latB-latA)*toRad, (lngB-lngA)*toRad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(latA*toRad)*math.Cos(latB*toRad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * 6371 * math.Asin(math.Min(1, math.Sqrt(h)))
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
	if s.gazetteer != nil {
		return s.nameMatchesOffline(matches)
	}
	names := make([]string, len(matches))
	username := os.Getenv("GEONAMES_USERNAME")
	if username == "" {
		return names
	}
	ctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	for i, match := range matches {
		row, col, ok := s.grid.LatLngToRowCol(match.Lat, match.Lng)
		if ok {
			country, _, _ := s.searchData.At(row*int(s.grid.Width) + col)
			names[i] = fmt.Sprintf("%.2f°, %.2f°, %s", match.Lat, match.Lng, country.Name)
		}
		// Results repeat across searches; cached names save GeoNames credits (1,000/hour).
		cacheKey := fmt.Sprintf("%.2f,%.2f", match.Lat, match.Lng)
		if name, ok := placeNameCache.Load(cacheKey); ok {
			names[i] = name.(string)
			continue
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
				placeNameCache.Store(cacheKey, names[i])
			}
		}(i, match)
	}
	wg.Wait()
	return names
}

func explainSearch(state *searchContext, refLabel string, ignored []string) string {
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
			parts = append(parts, state.Temperature.Comparison+" than "+refLabel+" annual mean")
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
// means AlphaEarth embedding similarity, so once there is a reference place or
// theme the search runs; anything the model couldn't map to a filter is listed in the
// explanation rather than blocking. Ambiguous place names are caught later by
// the geocoder.
func clarificationFor(parsed parsedSearch, current *searchContext) (string, bool) {
	if parsed.ReferencePlace != "" || parsed.Theme != "" || current != nil && (current.Reference != nil || current.Theme != "") {
		return "", false
	}
	if parsed.Clarification != "" {
		return parsed.Clarification, true
	}
	return "Which place should the results look like?", true
}

// Offline naming: results within nameNearKm get the town's name; farther ones
// say how far the nearest town is ("120 km from Tamanrasset, Algeria").
const (
	nameNearKm   = 25
	nameSearchKm = 150
)

func (s *Server) nameMatchesOffline(matches []similarity.TopMatch) []string {
	names := make([]string, len(matches))
	for i, match := range matches {
		if place, km, ok := s.gazetteer.Nearest(match.Lat, match.Lng, nameSearchKm); ok {
			names[i] = place.Label()
			if km > nameNearKm {
				names[i] = fmt.Sprintf("%.0f km from %s", km, names[i])
			}
			continue
		}
		name := ""
		if row, col, ok := s.grid.LatLngToRowCol(match.Lat, match.Lng); ok {
			country, _, _ := s.searchData.At(row*int(s.grid.Width) + col)
			name = country.Name
		}
		names[i] = strings.TrimSuffix(fmt.Sprintf("%.2f°, %.2f°, %s", match.Lat, match.Lng, name), ", ")
	}
	return names
}
