import { useEffect, useRef } from 'react'
import type maplibregl from 'maplibre-gl'
import { useQueryStore } from '../../stores/queryStore'
import { useSearchStore } from '../../stores/searchStore'
import { useThemeStore, type Basemap, type ResolvedTheme } from '../../stores/themeStore'
import { setImageryDimmed } from '../../hooks/useMapInit'

const SOURCE_ID = 'similarity-tiles'
const LAYER_ID = 'similarity-layer'
const HD_SOURCE_ID = 'similarity-tiles-hd'
const HD_LAYER_ID = 'similarity-layer-hd'

// Tiles are rendered for a dark basemap. On the light basemap the pale yellow low end
// washes out, so deepen and saturate it there. (Satellite dims the imagery instead.)
function colorPaint(theme: ResolvedTheme, basemap: Basemap) {
  const light = basemap === 'map' && theme === 'light'
  return {
    'raster-brightness-max': light ? 0.78 : 1,
    'raster-saturation': light ? 0.45 : 0,
    'raster-contrast': light ? 0.15 : 0,
  }
}

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
  const opacity = useThemeStore((s) => s.heatmapOpacity)
  const theme = useThemeStore((s) => s.resolved)
  const basemap = useThemeStore((s) => s.basemap)
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
    }, firstLabel?.id)
  }, [hdTileUrl, map])

  // Opacity + per-basemap treatment; runs after the layers above are (re)added
  useEffect(() => {
    const paint = { ...colorPaint(theme, basemap), 'raster-opacity': opacity }
    for (const id of [LAYER_ID, HD_LAYER_ID]) {
      if (!map.getLayer(id)) continue
      for (const [prop, value] of Object.entries(paint)) {
        map.setPaintProperty(id, prop, value)
      }
    }
    setImageryDimmed(map, !!tileUrl)
  }, [tileUrl, hdTileUrl, opacity, theme, basemap, map])

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
