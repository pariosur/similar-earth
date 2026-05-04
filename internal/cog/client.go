package cog

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const tileSize = 256

// Client calls the Python /fetch-cog-tile endpoint for 10m embeddings.
// Concurrency is limited via a semaphore to avoid overwhelming the Python service.
type Client struct {
	baseURL    string
	httpClient *http.Client
	sem        chan struct{} // concurrency limiter
}

// NewClient creates a COG client pointing at the Python service.
// maxConcurrent limits how many simultaneous requests to the Python service.
func NewClient(baseURL string, maxConcurrent int) *Client {
	if maxConcurrent <= 0 {
		maxConcurrent = 4
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		sem: make(chan struct{}, maxConcurrent),
	}
}

type cogTileRequest struct {
	Z             int      `json:"z"`
	X             int      `json:"x"`
	Y             int      `json:"y"`
	Year          int      `json:"year"`
	RefEmbeddings [][]int8 `json:"ref_embeddings"`
}

// FetchTileScores calls the Python /fetch-cog-tile endpoint.
// Returns 256*256 float32 scores, or nil if no data is available (204).
func (c *Client) FetchTileScores(ctx context.Context, z, x, y, year int, refEmbeddings [][]int8) ([]float32, error) {
	// Acquire semaphore slot (blocks if at max concurrency).
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	payload, err := json.Marshal(cogTileRequest{
		Z: z, X: x, Y: y,
		Year:          year,
		RefEmbeddings: refEmbeddings,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal COG request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/fetch-cog-tile", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create COG request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("COG request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("COG service returned %d: %s", resp.StatusCode, string(body))
	}

	expectedBytes := tileSize * tileSize * 4
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read COG response: %w", err)
	}
	if len(data) != expectedBytes {
		return nil, fmt.Errorf("unexpected COG response size: got %d, want %d", len(data), expectedBytes)
	}

	scores := make([]float32, tileSize*tileSize)
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, scores); err != nil {
		return nil, fmt.Errorf("decode COG scores: %w", err)
	}

	return scores, nil
}
