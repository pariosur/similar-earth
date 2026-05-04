import type { Pin, QueryResponse, PointResponse } from './types'

const BASE = '/api'

export async function postQuery(pins: Pin[]): Promise<QueryResponse> {
  const res = await fetch(`${BASE}/query`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ pins }),
  })
  if (!res.ok) {
    throw new Error(`Query failed: ${res.status} ${res.statusText}`)
  }
  return res.json() as Promise<QueryResponse>
}

export async function getQueryStatus(
  queryId: string,
): Promise<{ id: string; status: string }> {
  const res = await fetch(`${BASE}/query/${queryId}/status`)
  if (!res.ok) {
    throw new Error(`Status check failed: ${res.status}`)
  }
  return res.json()
}

export async function getPoint(
  queryId: string,
  lat: number,
  lng: number,
): Promise<PointResponse> {
  const res = await fetch(
    `${BASE}/query/${queryId}/point?lat=${lat}&lng=${lng}`,
  )
  if (!res.ok) {
    throw new Error(`Point query failed: ${res.status} ${res.statusText}`)
  }
  return res.json() as Promise<PointResponse>
}

export interface ReferencePin {
  lat: number
  lng: number
  label: string
}

export interface LayerInfo {
  id: string
  name: string
  description?: string
  category: string
  pin_count: number
  pins: ReferencePin[]
  tile_url: string
  featured: boolean
  has_tiles: boolean
}

export async function getLayers(): Promise<LayerInfo[]> {
  const res = await fetch(`${BASE}/layers`)
  if (!res.ok) {
    throw new Error(`Layers fetch failed: ${res.status}`)
  }
  const data = await res.json()
  return data.layers as LayerInfo[]
}

// ============================================================================
// Maps
// ============================================================================

export interface MapInfo {
  id: string
  slug: string
  title: string
  description: string
  category: string
  pins: { lat: number; lng: number; label?: string }[]
  pin_count: number
  author: string
  stars: number
  views: number
  is_featured: boolean
  has_tiles: boolean
  created_at: string
}

export async function createMap(data: {
  title: string
  description: string
  category: string
  pins: { lat: number; lng: number; label?: string }[]
  author?: string
}): Promise<MapInfo> {
  const res = await fetch(`${BASE}/maps`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  })
  if (!res.ok) throw new Error(`Create map failed: ${res.status}`)
  return res.json()
}

export async function listMaps(sort = 'newest', limit = 50): Promise<MapInfo[]> {
  const res = await fetch(`${BASE}/maps?sort=${sort}&limit=${limit}`)
  if (!res.ok) throw new Error(`List maps failed: ${res.status}`)
  const data = await res.json()
  return data.maps || []
}

export async function getMap(id: string): Promise<MapInfo> {
  const res = await fetch(`${BASE}/maps/${id}`)
  if (!res.ok) throw new Error(`Get map failed: ${res.status}`)
  return res.json()
}

export async function starMap(id: string): Promise<void> {
  const res = await fetch(`${BASE}/maps/${id}/star`, { method: 'POST' })
  if (!res.ok) throw new Error(`Star map failed: ${res.status}`)
}

export interface TopMatch {
  lat: number
  lng: number
  score: number
  best_pin_index: number
}

export interface TopMatchesResponse {
  matches: TopMatch[]
  pin_labels: string[]
  match_names?: string[]
}

export async function getTopMatches(source: { layerId?: string; queryId?: string }, count = 20): Promise<TopMatchesResponse> {
  let url: string
  if (source.layerId) {
    url = `${BASE}/layers/${source.layerId}/top?count=${count}`
  } else if (source.queryId) {
    url = `${BASE}/query/${source.queryId}/top?count=${count}`
  } else {
    return { matches: [], pin_labels: [] }
  }
  const res = await fetch(url)
  if (!res.ok) return { matches: [], pin_labels: [] }
  const data = await res.json()
  return { matches: data.matches || [], pin_labels: data.pin_labels || [], match_names: data.match_names }
}

