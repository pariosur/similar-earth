import { useEffect, useState } from 'react'
import { getLayers, listMaps, getMap, postQuery, getQueryStatus, type MapInfo } from '../api/client'
import { useQueryStore } from '../stores/queryStore'

function extractPins(map: MapInfo) {
  return (map.pins || []).map((p) => ({ lat: p.lat, lng: p.lng, label: p.label }))
}

function loadFeaturedMap(map: MapInfo) {
  useQueryStore.setState({
    pins: extractPins(map),
    activeMapId: map.id,
    activeMapSlug: map.slug,
    activeMapName: map.title,
    tileUrl: `/tiles/${map.slug}/{z}/{x}/{y}.png`,
    queryStatus: 'ready',
  })
}

export function useInitializeMap() {
  const setLayers = useQueryStore((s) => s.setLayers)
  const [initialized, setInitialized] = useState(false)

  useEffect(() => {
    async function init() {
      const layers = await getLayers()
      setLayers(layers)

      const params = new URLSearchParams(window.location.search)
      const mapId = params.get('map')
      const slug = params.get('s')
      const queryId = params.get('query')

      if (queryId) {
        // Restore a custom query (from Build Map)
        try {
          const status = await getQueryStatus(queryId)
          if (status.status === 'completed') {
            useQueryStore.setState({
              activeQueryId: queryId,
              tileUrl: `/api/tiles/${queryId}/{z}/{x}/{y}.png`,
              queryStatus: 'ready',
            })
          }
        } catch (e) {
          console.error('Failed to restore custom query:', e)
        }
      } else if (mapId) {
        try {
          const map = await getMap(mapId)
          if (map.has_tiles && map.slug) {
            loadFeaturedMap(map)
          } else {
            useQueryStore.setState({
              pins: extractPins(map),
              activeMapId: map.id,
              activeMapSlug: map.slug || null,
              activeMapName: map.title,
              queryStatus: 'computing',
            })
            const res = await postQuery(extractPins(map))
            const poll = setInterval(async () => {
              const status = await getQueryStatus(res.id)
              if (status.status === 'completed') {
                clearInterval(poll)
                useQueryStore.setState({
                  activeQueryId: res.id,
                  tileUrl: res.tile_url,
                  queryStatus: 'ready',
                })
              }
            }, 1500)
          }
        } catch (e) {
          console.error('Failed to load shared map:', e)
        }
      } else if (slug) {
        try {
          const maps = await listMaps('newest', 200)
          const map = maps.find((m) => m.slug === slug)
          if (map?.has_tiles) loadFeaturedMap(map)
        } catch (e) {
          console.error('Failed to load map by slug:', e)
        }
      } else {
        // No URL params — auto-load Hass Avocado as the default map
        try {
          const maps = await listMaps('newest', 200)
          const defaultMap = maps.find((m) => m.slug === 'avocado')
          if (defaultMap?.has_tiles) loadFeaturedMap(defaultMap)
        } catch (e) {
          console.error('Failed to load default map:', e)
        }
      }

      setInitialized(true)
      ;(window as any).__showApp?.()
    }

    init().catch((e) => {
      console.error('Init failed:', e)
      setInitialized(true)
      ;(window as any).__showApp?.()
    })
  }, [setLayers])

  return initialized
}
