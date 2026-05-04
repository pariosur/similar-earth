package similarity

import "github.com/google/uuid"

// Pin represents a geographic reference point selected by the user.
type Pin struct {
	Lat   float64 `json:"lat"`
	Lng   float64 `json:"lng"`
	Label string  `json:"label,omitempty"`
}

// Query holds the user's similarity query: one or more reference pins
// whose embeddings are compared against the entire grid.
type Query struct {
	ID   uuid.UUID
	Pins []Pin
	// RefEmbeddings holds the grid embedding looked up for each pin.
	// Populated by Engine.Compute before the similarity scan.
	RefEmbeddings [][]int8
}

// QueryResult contains the similarity scores for every grid pixel.
type QueryResult struct {
	QueryID       uuid.UUID
	Scores        []float32 // one per grid pixel (row-major), nil if lazy-loaded
	BestPinIndex  []uint8   // which pin was the best match per pixel
	Width         int
	Height        int
	ComputeMs     int64
	RefEmbeddings [][]int8 // reference embeddings for COG refinement
	scoresPath    string   // path to scores file for lazy loading
}
