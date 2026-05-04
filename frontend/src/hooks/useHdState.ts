import { useEffect, useCallback, useRef } from 'react'
import maplibregl from 'maplibre-gl'
import { useQueryStore } from '../stores/queryStore'

const HD_ZOOM = 10

export function useHdState(mapRef: React.RefObject<maplibregl.Map | null>, zoom: number) {
  const tileUrl = useQueryStore((s) => s.tileUrl)
  const activeMapSlug = useQueryStore((s) => s.activeMapSlug)
  const activeQueryId = useQueryStore((s) => s.activeQueryId)
  const hdState = useQueryStore((s) => s.hdState)
  const hdTileUrl = useQueryStore((s) => s.hdTileUrl)
  const setHdState = useQueryStore((s) => s.setHdState)
  const setHdTileUrl = useQueryStore((s) => s.setHdTileUrl)

  // Remember the HD URL so we can restore it after zoom out/in
  const lastHdUrl = useRef<string | null>(null)

  // Track when HD tiles have been loaded
  useEffect(() => {
    if (hdTileUrl) lastHdUrl.current = hdTileUrl
  }, [hdTileUrl])

  // Clear memory when switching maps
  useEffect(() => {
    lastHdUrl.current = null
  }, [activeMapSlug, activeQueryId])

  const handlePillClick = useCallback((target: 'global' | 'hd') => {
    const map = mapRef.current
    if (!map || !tileUrl) return

    if (target === 'hd') {
      if (hdState === 'off') {
        map.flyTo({ zoom: HD_ZOOM, duration: 1500 })
        setHdState('zoomed')
      } else if (hdState === 'zoomed') {
        let hdUrl: string | null = null
        if (activeMapSlug) {
          hdUrl = `/api/layers/${activeMapSlug}/tiles/{z}/{x}/{y}.png`
        } else if (activeQueryId) {
          hdUrl = `/api/tiles/${activeQueryId}/{z}/{x}/{y}.png`
        }
        if (hdUrl) {
          setHdState('computing')
          setHdTileUrl(hdUrl)
        }
      }
    } else {
      if (hdState === 'loaded' || hdState === 'zoomed') {
        lastHdUrl.current = null // User explicitly chose 2KM — don't auto-restore
        setHdState('off')
        setHdTileUrl(null)
        map.flyTo({ zoom: 3, duration: 1500 })
      }
    }
  }, [hdState, tileUrl, activeMapSlug, activeQueryId, setHdState, setHdTileUrl, mapRef])

  // Sync HD state with zoom level
  useEffect(() => {
    if (!tileUrl) return

    if (zoom >= HD_ZOOM && hdState === 'off') {
      // If we previously loaded HD for this map, auto-restore
      if (lastHdUrl.current) {
        setHdTileUrl(lastHdUrl.current)
        setHdState('computing')
      } else {
        setHdState('zoomed')
      }
    } else if (zoom < HD_ZOOM - 1 && (hdState === 'zoomed' || hdState === 'loaded')) {
      setHdState('off')
      setHdTileUrl(null)
    }
  }, [zoom, hdState, tileUrl, setHdState, setHdTileUrl])

  return { handlePillClick }
}
