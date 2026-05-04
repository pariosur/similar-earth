import { useRef, useEffect, useCallback, useState } from 'react'
import maplibregl from 'maplibre-gl'
import { useQueryStore } from '../stores/queryStore'
import { useThemeStore } from '../stores/themeStore'

const DARK_STYLE = 'https://basemaps.cartocdn.com/gl/dark-matter-gl-style/style.json'
const LIGHT_STYLE = 'https://basemaps.cartocdn.com/gl/positron-gl-style/style.json'

interface UseMapInitOptions {
  onFlyToReady?: (fn: (lat: number, lng: number) => void) => void
}

export function useMapInit({ onFlyToReady }: UseMapInitOptions) {
  const mapContainerRef = useRef<HTMLDivElement>(null)
  const mapRef = useRef<maplibregl.Map | null>(null)
  const [mapReady, setMapReady] = useState(false)
  const [zoom, setZoom] = useState(2.5)

  const resolvedTheme = useThemeStore((s) => s.resolved)
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

    const map = new maplibregl.Map({
      container: mapContainerRef.current,
      style: resolvedTheme === 'dark' ? DARK_STYLE : LIGHT_STYLE,
      center: [-75, 10],
      zoom: 2.5,
    })

    map.addControl(new maplibregl.NavigationControl(), 'bottom-right')

    map.on('load', () => setMapReady(true));
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

  // Switch basemap on theme change
  useEffect(() => {
    const map = mapRef.current
    if (!map) return
    const newStyle = resolvedTheme === 'dark' ? DARK_STYLE : LIGHT_STYLE
    map.setStyle(newStyle)
    map.once('style.load', () => setMapReady(true))
  }, [resolvedTheme])

  return { mapContainerRef, mapRef, mapReady, zoom, handleFlyTo }
}
