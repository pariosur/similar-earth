import { useState, useCallback, useRef } from 'react'
import { useQueryStore } from '../stores/queryStore'
import type { MapInfo } from '../api/client'

/**
 * Shared map selection logic used by MapCard (gallery) and MapChip (collapsed sheet).
 * Handles state updates, URL params, and async tile computation for non-featured maps.
 */
export function useSelectMap() {
  const [loading, setLoading] = useState(false)
  const pollRef = useRef<ReturnType<typeof setInterval>>(undefined)

  const selectMap = useCallback(async (map: MapInfo) => {
    const store = useQueryStore.getState()

    // Already active and ready — no-op
    if (store.activeMapId === map.id && (store.queryStatus === 'ready' || store.queryStatus === 'computing')) {
      return
    }

    const pins = (map.pins || []).map((p) => ({
      lat: p.lat,
      lng: p.lng,
      label: p.label,
    }))

    if (map.has_tiles && map.slug) {
      useQueryStore.setState({
        pins,
        activeMapId: map.id,
        activeMapSlug: map.slug,
        activeMapName: map.title,
        activeQueryId: null,
        tileUrl: `/tiles/${map.slug}/{z}/{x}/{y}.png`,
        queryStatus: 'ready',
        createMode: false,
        addPinMode: false,
        hdState: 'off',
        hdTileUrl: null,
        selectedPinIndex: null,
        selectedDiscoveryIndex: null,
      })
      const url = new URL(window.location.href)
      url.searchParams.set('s', map.slug)
      url.searchParams.delete('map')
      url.searchParams.delete('query')
      window.history.replaceState({}, '', url.toString())
    } else {
      useQueryStore.setState({
        pins,
        activeMapId: map.id,
        activeMapSlug: map.slug || null,
        activeMapName: map.title,
        activeQueryId: null,
        tileUrl: null,
        queryStatus: 'computing',
        createMode: false,
        addPinMode: false,
        hdState: 'off',
        hdTileUrl: null,
        selectedPinIndex: null,
        selectedDiscoveryIndex: null,
      })
      setLoading(true)
      if (pollRef.current) clearInterval(pollRef.current)
      try {
        const { postQuery, getQueryStatus } = await import('../api/client')
        const res = await postQuery(pins)
        pollRef.current = setInterval(async () => {
          const status = await getQueryStatus(res.id)
          if (status.status === 'completed') {
            clearInterval(pollRef.current)
            setLoading(false)
            useQueryStore.setState({
              activeMapId: map.id,
              activeQueryId: res.id,
              tileUrl: res.tile_url,
              queryStatus: 'ready',
            })
            const url = new URL(window.location.href)
            url.searchParams.set('map', map.id)
            url.searchParams.delete('s')
            url.searchParams.delete('query')
            window.history.replaceState({}, '', url.toString())
          }
        }, 1500)
      } catch {
        setLoading(false)
        useQueryStore.setState({ queryStatus: 'failed' })
      }
    }
  }, [])

  return { selectMap, loading }
}
