import { useRef, useEffect, useCallback, useState } from 'react'
import maplibregl from 'maplibre-gl'
import { useQueryStore } from '../stores/queryStore'
import { useThemeStore, type ResolvedTheme, type Basemap } from '../stores/themeStore'

const DARK_STYLE = 'https://basemaps.cartocdn.com/gl/dark-matter-gl-style/style.json'
const LIGHT_STYLE = 'https://basemaps.cartocdn.com/gl/positron-gl-style/style.json'

// Esri World Imagery — free, no API key, high-res global coverage to ~z19.
const ESRI_SATELLITE = 'https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}'
const ESRI_ATTRIBUTION = 'Imagery © Esri, Maxar, Earthstar Geographics'
const SAT_SOURCE_ID = 'satellite-basemap'
const SAT_LAYER_ID = 'satellite-basemap-layer'

// Satellite mode always uses the dark vector style so labels stay legible (white halos) over imagery.
function computeVectorStyle(theme: ResolvedTheme, basemap: Basemap): string {
  if (basemap === 'satellite') return DARK_STYLE
  return theme === 'dark' ? DARK_STYLE : LIGHT_STYLE
}

/**
 * Toggle the Esri satellite raster in-place: add it as the bottom layer and hide the
 * opaque base fills so imagery shows through, keeping labels + lines on top. Reversible.
 *
 * Must only be called once the style DOCUMENT is loaded (from a 'load' or 'style.load'
 * handler, or after the initial load). It does NOT depend on map.isStyleLoaded() — that
 * stays false for seconds while sprite/glyphs/tiles load, yet addLayer/addSource already
 * work. Gating on isStyleLoaded() was what stopped the satellite from appearing on toggle.
 */
function applyBasemap(map: maplibregl.Map, basemap: Basemap) {
  const hasSat = !!map.getLayer(SAT_LAYER_ID)

  if (basemap === 'satellite') {
    if (!hasSat) {
      const layers = map.getStyle().layers || []
      const firstNonBg = layers.find((l) => l.type !== 'background')
      if (!map.getSource(SAT_SOURCE_ID)) {
        map.addSource(SAT_SOURCE_ID, {
          type: 'raster',
          tiles: [ESRI_SATELLITE],
          tileSize: 256,
          maxzoom: 19,
          attribution: ESRI_ATTRIBUTION,
        })
      }
      map.addLayer({ id: SAT_LAYER_ID, type: 'raster', source: SAT_SOURCE_ID }, firstNonBg?.id)
    }
    // Hide opaque base layers so imagery is visible; keep symbols (labels) + lines.
    for (const l of map.getStyle().layers || []) {
      if (l.id === SAT_LAYER_ID) continue
      if (l.type === 'background' || l.type === 'fill' || l.type === 'fill-extrusion') {
        try { map.setLayoutProperty(l.id, 'visibility', 'none') } catch {}
      }
    }
  } else {
    if (hasSat) {
      try { map.removeLayer(SAT_LAYER_ID) } catch {}
      try { map.removeSource(SAT_SOURCE_ID) } catch {}
    }
    for (const l of map.getStyle().layers || []) {
      if (l.type === 'background' || l.type === 'fill' || l.type === 'fill-extrusion') {
        try { map.setLayoutProperty(l.id, 'visibility', 'visible') } catch {}
      }
    }
  }
}

interface UseMapInitOptions {
  onFlyToReady?: (fn: (lat: number, lng: number) => void) => void
}

export function useMapInit({ onFlyToReady }: UseMapInitOptions) {
  const mapContainerRef = useRef<HTMLDivElement>(null)
  const mapRef = useRef<maplibregl.Map | null>(null)
  const [mapReady, setMapReady] = useState(false)
  const [zoom, setZoom] = useState(2.5)

  const resolvedTheme = useThemeStore((s) => s.resolved)
  const basemap = useThemeStore((s) => s.basemap)
  // Tracks the vector style currently loaded so we only call setStyle when it actually changes.
  const currentStyleRef = useRef<string>(computeVectorStyle(resolvedTheme, basemap))
  // True once the style document is loaded (initial 'load' or post-setStyle 'style.load'),
  // i.e. when it's safe to add/remove layers. Reset to false during a setStyle swap.
  const styleReadyRef = useRef(false)
  const addPin = useQueryStore((s) => s.addPin)
  const addPinMode = useQueryStore((s) => s.addPinMode)
  const createMode = useQueryStore((s) => s.createMode)
  const setAddPinMode = useQueryStore((s) => s.setAddPinMode)
  const activeQueryId = useQueryStore((s) => s.activeQueryId)
  const setInspectedPoint = useQueryStore((s) => s.setInspectedPoint)

  const handleFlyTo = useCallback((lat: number, lng: number, z = 10) => {
    mapRef.current?.flyTo({ center: [lng, lat], zoom: z, duration: 2000 })
  }, [])

  // Expose flyTo to parent
  useEffect(() => {
    onFlyToReady?.((lat, lng) => {
      mapRef.current?.flyTo({ center: [lng, lat], zoom: 8, duration: 2000 })
    })
  }, [onFlyToReady])

  // Map click handler
  const handleClick = useCallback(
    (e: maplibregl.MapMouseEvent) => {
      if (createMode || addPinMode) {
        addPin(e.lngLat.lat, e.lngLat.lng)
        if (!createMode) setAddPinMode(false)
        return
      }
      // If something was selected, just clear it. Otherwise inspect the point.
      const { selectedPinIndex, selectedDiscoveryIndex } = useQueryStore.getState()
      if (selectedPinIndex !== null || selectedDiscoveryIndex !== null) {
        useQueryStore.getState().clearSelection()
        return
      }
      if (activeQueryId) {
        setInspectedPoint({ lat: e.lngLat.lat, lng: e.lngLat.lng })
      }
    },
    [createMode, addPinMode, addPin, setAddPinMode, activeQueryId, setInspectedPoint],
  )

  // Create map
  useEffect(() => {
    if (!mapContainerRef.current || mapRef.current) return

    const initialBasemap = useThemeStore.getState().basemap
    const map = new maplibregl.Map({
      container: mapContainerRef.current,
      style: computeVectorStyle(resolvedTheme, initialBasemap),
      center: [-75, 10],
      zoom: 2.5,
    })

    map.addControl(new maplibregl.NavigationControl(), 'bottom-right')

    map.on('load', () => {
      styleReadyRef.current = true
      applyBasemap(map, useThemeStore.getState().basemap)
      setMapReady(true)
    });
    (window as any).__map = map
    map.on('zoom', () => setZoom(map.getZoom()))

    mapRef.current = map

    return () => {
      map.remove()
      mapRef.current = null
      setMapReady(false)
    }
  }, [])

  // Register click handler
  useEffect(() => {
    const map = mapRef.current
    if (!map) return
    map.on('click', handleClick)
    return () => { map.off('click', handleClick) }
  }, [handleClick])

  // Escape key clears selection
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        useQueryStore.getState().clearSelection()
        useQueryStore.getState().setInspectedPoint(null)
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [])

  // Cursor mode
  useEffect(() => {
    const container = mapContainerRef.current
    if (!container) return
    container.style.cursor = (createMode || addPinMode) ? 'crosshair' : ''
  }, [createMode, addPinMode])

  // React to theme / basemap changes.
  // - If the underlying vector style changes (e.g. light<->dark, or light->satellite),
  //   setStyle wipes all custom layers, so we flip mapReady to remount the overlay
  //   components (HeatmapLayer/markers) — they re-add themselves from store state.
  //   This also fixes the heatmap silently vanishing on a theme toggle.
  // - If the vector style is unchanged (e.g. dark<->satellite), toggle the satellite
  //   raster in place — instant, overlays untouched.
  useEffect(() => {
    const map = mapRef.current
    if (!map) return
    const target = computeVectorStyle(resolvedTheme, basemap)

    if (target !== currentStyleRef.current) {
      currentStyleRef.current = target
      styleReadyRef.current = false
      setMapReady(false)
      // diff:false forces a full style reload so 'style.load' fires reliably (a diffed
      // setStyle may not emit it), and gives us a clean slate to re-add overlays onto.
      map.setStyle(target, { diff: false })
      map.once('style.load', () => {
        styleReadyRef.current = true
        applyBasemap(map, useThemeStore.getState().basemap)
        setMapReady(true)
      })
    } else if (styleReadyRef.current) {
      // Same vector style — toggle the satellite raster in place (instant, overlays intact).
      applyBasemap(map, basemap)
    }
  }, [resolvedTheme, basemap])

  return { mapContainerRef, mapRef, mapReady, zoom, handleFlyTo }
}
