export interface Pin {
  lat: number
  lng: number
  label?: string
}

export interface QueryResponse {
  id: string
  status: 'computing' | 'ready' | 'failed'
  tile_url: string
  compute_ms: number
  pin_count: number
}

export interface PointResponse {
  similarity: number
  best_pin_index: number
  best_pin_label?: string
  terrain?: {
    elevation_m: number
    slope_deg: number
    aspect_deg: number
  }
  landcover?: {
    class_name: string
    class_id: number
  }
  biophysical?: {
    annual_rainfall_mm: number
    mean_temp_c: number
    soil_moisture: number
  }
}
