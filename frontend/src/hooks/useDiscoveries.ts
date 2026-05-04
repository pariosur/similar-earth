import { useEffect, useState } from 'react'
import { getTopMatches } from '../api/client'
import { useQueryStore } from '../stores/queryStore'

export interface Discovery {
  lat: number
  lng: number
  score: number
  name: string
  best_pin_index: number
  similar_to?: string
}

// Shared cache across all components (markers, chips, sidebar list)
const cache = new Map<string, Discovery[]>()

// In-flight promises — dedupe concurrent fetches for the same key
const inflight = new Map<string, Promise<Discovery[]>>()

const DISCOVERY_LIMIT = 10

async function fetchDiscoveries(key: string, source: { layerId?: string; queryId?: string }): Promise<Discovery[]> {
  if (cache.has(key)) return cache.get(key)!
  if (inflight.has(key)) return inflight.get(key)!

  const promise = getTopMatches(source, DISCOVERY_LIMIT).then((resp) => {
    const items: Discovery[] = resp.matches.map((m, i) => ({
      lat: m.lat,
      lng: m.lng,
      score: m.score,
      name: resp.match_names?.[i] || `${m.lat.toFixed(2)}, ${m.lng.toFixed(2)}`,
      best_pin_index: m.best_pin_index,
      similar_to: resp.pin_labels[m.best_pin_index] || undefined,
    }))
    cache.set(key, items)
    inflight.delete(key)
    return items
  }).catch((err) => {
    inflight.delete(key)
    throw err
  })

  inflight.set(key, promise)
  return promise
}

/**
 * Single source of truth for top similarity matches.
 * Shared cache + in-flight dedupe — only one API call per map across all components.
 */
export function useDiscoveries() {
  const activeMapSlug = useQueryStore((s) => s.activeMapSlug)
  const activeQueryId = useQueryStore((s) => s.activeQueryId)
  const queryStatus = useQueryStore((s) => s.queryStatus)
  const [discoveries, setDiscoveries] = useState<Discovery[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(false)

  useEffect(() => {
    if (queryStatus !== 'ready') {
      setDiscoveries([])
      setLoading(false)
      setError(false)
      return
    }
    const key = activeMapSlug || activeQueryId || null
    if (!key) return

    if (cache.has(key)) {
      setDiscoveries(cache.get(key)!)
      setLoading(false)
      setError(false)
      return
    }

    setLoading(true)
    setError(false)
    let cancelled = false

    fetchDiscoveries(
      key,
      activeMapSlug ? { layerId: activeMapSlug } : { queryId: activeQueryId! }
    )
      .then((items) => {
        if (!cancelled) {
          setDiscoveries(items)
          setLoading(false)
        }
      })
      .catch(() => {
        if (!cancelled) {
          setDiscoveries([])
          setLoading(false)
          setError(true)
        }
      })

    return () => { cancelled = true }
  }, [activeMapSlug, activeQueryId, queryStatus])

  return { discoveries, loading, error }
}
