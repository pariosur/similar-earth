package api

// QueryRequest is the JSON body for POST /api/query.
type QueryRequest struct {
	Pins   []PinDTO `json:"pins"`
	Intent string   `json:"intent,omitempty"`
}

// PinDTO represents a geographic reference pin.
type PinDTO struct {
	Lat   float64 `json:"lat"`
	Lng   float64 `json:"lng"`
	Label string  `json:"label,omitempty"`
}

// QueryResponse is returned after a similarity computation completes.
type QueryResponse struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	TileURL   string `json:"tile_url"`
	ComputeMs int64  `json:"compute_ms"`
	PinCount  int    `json:"pin_count"`
}

// PointResponse is returned for GET /api/query/:id/point.
type PointResponse struct {
	Similarity   float64          `json:"similarity"`
	BestPinIndex int              `json:"best_pin_index"`
	BestPinLabel string           `json:"best_pin_label,omitempty"`
	Terrain      *TerrainData     `json:"terrain,omitempty"`
	Landcover    *LandcoverData   `json:"landcover,omitempty"`
	Biophysical  *BiophysicalData `json:"biophysical,omitempty"`
}

// TerrainData holds elevation, slope and aspect for a point.
type TerrainData struct {
	ElevationM float64 `json:"elevation_m"`
	SlopeDeg   float64 `json:"slope_deg"`
	AspectDeg  float64 `json:"aspect_deg"`
}

// LandcoverData holds land cover classification for a point.
type LandcoverData struct {
	ClassName string `json:"class_name"`
	ClassID   int    `json:"class_id"`
}

// BiophysicalData holds climate/soil metrics for a point.
type BiophysicalData struct {
	AnnualRainfallMm float64 `json:"annual_rainfall_mm"`
	MeanTempC        float64 `json:"mean_temp_c"`
	SoilMoisture     float64 `json:"soil_moisture"`
}

// HealthResponse is returned for GET /api/health.
type HealthResponse struct {
	Status     string `json:"status"`
	GridLoaded bool   `json:"grid_loaded"`
	GridPixels int    `json:"grid_pixels"`
	LandPixels int    `json:"land_pixels"`
	UptimeS    int64  `json:"uptime_s"`
}
