package gee

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client talks to the Python GEE microservice.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a GEE client pointing at the given base URL.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// --- request / response types matching the Python service ---

type pointRequest struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// TerrainData mirrors the Python TerrainResponse.
type TerrainData struct {
	Elevation float64 `json:"elevation"`
	Slope     float64 `json:"slope"`
	Aspect    float64 `json:"aspect"`
}

// LandcoverData mirrors the Python LandcoverResponse.
type LandcoverData struct {
	DominantClass    string             `json:"dominant_class"`
	ClassPercentages map[string]float64 `json:"class_percentages"`
}

// LandDataResponse mirrors the Python LandDataResponse (flat structure).
type LandDataResponse struct {
	// Climate
	TempMeanC        float64 `json:"temp_mean_c"`
	TempMinC         float64 `json:"temp_min_c"`
	TempMaxC         float64 `json:"temp_max_c"`
	AnnualRainfallMm float64 `json:"annual_rainfall_mm"`
	WaterBalanceMm   float64 `json:"water_balance_mm"`
	SunshineHours    float64 `json:"sunshine_hours_daily"`
	HumidityPct      float64 `json:"humidity_percent"`

	// Terrain
	ElevationM   float64 `json:"elevation_m"`
	SlopeDegrees float64 `json:"slope_degrees"`
	Aspect       string  `json:"aspect"`

	// Soil
	SoilPH             float64 `json:"soil_ph"`
	SoilType           string  `json:"soil_type"`
	SoilDrainage       string  `json:"soil_drainage"`
	SoilMoistureStatus string  `json:"soil_moisture_status"`

	// Risk
	PoolingRisk     string `json:"pooling_risk"`
	HeatStressRisk  string `json:"heat_stress_risk"`
	IrrigationNeeded bool   `json:"irrigation_needed"`

	// Summary
	Suitability string   `json:"suitability"`
	Concerns    []string `json:"concerns"`
}

// FetchTerrain gets elevation, slope, aspect for a point.
func (c *Client) FetchTerrain(ctx context.Context, lat, lng float64) (*TerrainData, error) {
	var result TerrainData
	if err := c.post(ctx, "/fetch-terrain", pointRequest{Latitude: lat, Longitude: lng}, &result); err != nil {
		return nil, fmt.Errorf("fetch terrain: %w", err)
	}
	return &result, nil
}

// FetchLandcover gets land cover classification for a point.
func (c *Client) FetchLandcover(ctx context.Context, lat, lng float64) (*LandcoverData, error) {
	var result LandcoverData
	if err := c.post(ctx, "/fetch-landcover", pointRequest{Latitude: lat, Longitude: lng}, &result); err != nil {
		return nil, fmt.Errorf("fetch landcover: %w", err)
	}
	return &result, nil
}

// landDataRequest includes a crop field.
type landDataRequest struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Crop      string  `json:"crop,omitempty"`
}

// FetchLandData gets comprehensive biophysical data for a point.
func (c *Client) FetchLandData(ctx context.Context, lat, lng float64) (*LandDataResponse, error) {
	var result LandDataResponse
	req := landDataRequest{Latitude: lat, Longitude: lng, Crop: "hass avocado"}
	if err := c.post(ctx, "/fetch-land-data", req, &result); err != nil {
		return nil, fmt.Errorf("fetch land data: %w", err)
	}
	return &result, nil
}

// post is a helper that POSTs JSON and decodes the response.
func (c *Client) post(ctx context.Context, path string, body any, dest any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GEE service returned %d: %s", resp.StatusCode, string(b))
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
