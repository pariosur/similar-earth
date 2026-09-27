import { useEffect, useRef } from 'react'
import type maplibregl from 'maplibre-gl'
import { useQueryStore } from '../../stores/queryStore'
import { useSearchStore } from '../../stores/searchStore'

const SOURCE_ID = 'similarity-tiles'
const LAYER_ID = 'similarity-layer'
const HD_SOURCE_ID = 'similarity-tiles-hd'
const HD_LAYER_ID = 'similarity-layer-hd'

interface HeatmapLayerProps {
  map: maplibregl.Map
}

export function HeatmapLayer({ map }: HeatmapLayerProps) {
  // Find places shows only its own Explore heatmap (2 km, no HD); the gallery
  // map's layers come back when switching to Maps.
  const isSearch = useSearchStore((s) => s.mapMode) === 'search'
  const searchTileUrl = useSearchStore((s) => s.explored?.tileUrl ?? null)
  const galleryTileUrl = useQueryStore((s) => s.tileUrl)
  const galleryHdTileUrl = useQueryStore((s) => s.hdTileUrl)
  const tileUrl = isSearch ? searchTileUrl : galleryTileUrl
  const hdTileUrl = isSearch ? null : galleryHdTileUrl
  const hdState = useQueryStore((s) => s.hdState)
  const setHdState = useQueryStore((s) => s.setHdState)
  const prevTileUrl = useRef<string | null>(null)
  const prevHdTileUrl = useRef<string | null>(null)

  // Base layer — instant swap
  useEffect(() => {
    if (tileUrl === prevTileUrl.current) return
    prevTileUrl.current = tileUrl

    // Clean up existing layers
    try {
      if (map.getLayer(LAYER_ID)) map.removeLayer(LAYER_ID)
      if (map.getSource(SOURCE_ID)) map.removeSource(SOURCE_ID)
      if (map.getLayer(HD_LAYER_ID)) map.removeLayer(HD_LAYER_ID)
      if (map.getSource(HD_SOURCE_ID)) map.removeSource(HD_SOURCE_ID)
    } catch {}
    prevHdTileUrl.current = null

    if (!tileUrl) return

    const isStatic = tileUrl.startsWith('/tiles/')

    map.addSource(SOURCE_ID, {
      type: 'raster',
      tiles: [tileUrl],
      tileSize: 256,
      ...(isStatic ? { minzoom: 2, maxzoom: 8 } : {}),
    })

    // Insert below label layers so place names stay visible
    const firstLabel = map.getStyle().layers?.find((l: any) => l.type === 'symbol')
    map.addLayer({
      id: LAYER_ID,
      type: 'raster',
      source: SOURCE_ID,
      paint: { 'raster-opacity': 0.7 },
    }, firstLabel?.id)
  }, [tileUrl, map])

  // HD layer
  useEffect(() => {
    if (hdTileUrl === prevHdTileUrl.current) return
    prevHdTileUrl.current = hdTileUrl

    try {
      if (map.getLayer(HD_LAYER_ID)) map.removeLayer(HD_LAYER_ID)
      if (map.getSource(HD_SOURCE_ID)) map.removeSource(HD_SOURCE_ID)
    } catch {}

    if (!hdTileUrl) return

    const firstLabel = map.getStyle().layers?.find((l: any) => l.type === 'symbol')
    map.addSource(HD_SOURCE_ID, {
      type: 'raster',
      tiles: [hdTileUrl],
      tileSize: 256,
      minzoom: 9,
      maxzoom: 14,
    })

    map.addLayer({
      id: HD_LAYER_ID,
      type: 'raster',
      source: HD_SOURCE_ID,
      minzoom: 9,
      paint: { 'raster-opacity': 0.85 },
    }, firstLabel?.id)
  }, [hdTileUrl, map])

  // Track HD tile loading → loaded transition
  useEffect(() => {
    if (hdState !== 'computing') return

    const onIdle = () => {
      if (hdTileUrl && map.getSource(HD_SOURCE_ID)) {
        setHdState('loaded')
      }
    }
    map.on('idle', onIdle)
    return () => { map.off('idle', onIdle) }
  }, [hdState, hdTileUrl, map, setHdState])

  return null
}
