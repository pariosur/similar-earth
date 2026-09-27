package api

import "sort"

// searchTheme is a kind of place ("mangroves", "coffee country") that natural
// search looks for using the curated reference pins of a gallery map, instead
// of a single named reference place.
type searchTheme struct {
	ID   string
	Name string
	Pins []ReferencePin
}

// minThemePins keeps out gallery maps with too few sites to define a kind of place.
const minThemePins = 5

// searchThemes returns the gallery maps usable as search themes, sorted by ID.
func (s *Server) searchThemes() []searchTheme {
	themes := make([]searchTheme, 0, len(s.layerMeta))
	for id, meta := range s.layerMeta {
		if len(meta.Pins) >= minThemePins {
			themes = append(themes, searchTheme{ID: id, Name: meta.Name, Pins: meta.Pins})
		}
	}
	sort.Slice(themes, func(i, j int) bool { return themes[i].ID < themes[j].ID })
	return themes
}

func findTheme(themes []searchTheme, id string) *searchTheme {
	for i := range themes {
		if themes[i].ID == id {
			return &themes[i]
		}
	}
	return nil
}
