import { useState, useRef, useEffect, useCallback } from 'react'
import { useQueryStore } from '../../stores/queryStore'

interface SearchResult {
  display_name: string
  lat: string
  lon: string
}

interface SearchBarProps {
  onFlyTo: (lat: number, lng: number, zoom?: number) => void
}

export function SearchBar({ onFlyTo }: SearchBarProps) {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchResult[]>([])
  const [open, setOpen] = useState(false)
  const [loading, setLoading] = useState(false)
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined)
  const containerRef = useRef<HTMLDivElement>(null)

  // Close dropdown on outside click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  const search = useCallback(async (q: string) => {
    // Check if it's coordinates (e.g. "5.77, -75.77" or "5.77 -75.77")
    const coordMatch = q.match(/^\s*(-?\d+\.?\d*)[,\s]+(-?\d+\.?\d*)\s*$/)
    if (coordMatch) {
      const lat = parseFloat(coordMatch[1])
      const lng = parseFloat(coordMatch[2])
      if (lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180) {
        onFlyTo(lat, lng, 12)
        setOpen(false)
        setQuery(`${lat.toFixed(4)}, ${lng.toFixed(4)}`)
        return
      }
    }

    if (q.length < 3) {
      setResults([])
      return
    }

    setLoading(true)
    try {
      const res = await fetch(
        `https://nominatim.openstreetmap.org/search?format=json&limit=5&q=${encodeURIComponent(q)}`,
        { headers: { 'Accept-Language': 'en' } }
      )
      const data: SearchResult[] = await res.json()
      setResults(data)
      setOpen(data.length > 0)
    } catch {
      setResults([])
    } finally {
      setLoading(false)
    }
  }, [onFlyTo])

  const handleInput = (value: string) => {
    setQuery(value)
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => search(value), 400)
  }

  const handleSelect = (r: SearchResult) => {
    const lat = parseFloat(r.lat)
    const lng = parseFloat(r.lon)
    const createMode = useQueryStore.getState().createMode
    if (createMode) {
      // In create mode: add as pin + fly to it
      useQueryStore.getState().addPin(lat, lng)
      onFlyTo(lat, lng, 10)
      setQuery('')
    } else {
      onFlyTo(lat, lng, 10)
      setQuery(r.display_name.split(',').slice(0, 2).join(','))
    }
    setOpen(false)
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      if (results.length > 0) {
        handleSelect(results[0])
      } else {
        search(query)
      }
    }
    if (e.key === 'Escape') {
      setOpen(false)
    }
  }

  return (
    <div ref={containerRef} className="absolute top-[var(--search-top)] left-1/2 -translate-x-1/2 z-[var(--z-panel)] w-[380px] max-w-[calc(100vw-2rem)]">
      <div style={{
        display: 'flex',
        alignItems: 'center',
        background: 'var(--ctrl-bg)',
        border: '1px solid var(--fg-10)',
        padding: '0 12px',
        backdropFilter: 'blur(8px)',
      }}>
        <span className="material-symbols-outlined" style={{ fontSize: 16, color: 'var(--fg-40)' }}>search</span>
        <input
          type="text"
          value={query}
          onChange={(e) => handleInput(e.target.value)}
          onKeyDown={handleKeyDown}
          onFocus={() => results.length > 0 && setOpen(true)}
          placeholder={useQueryStore.getState().createMode ? 'Search for a place to add as pin...' : 'Search for a place or paste coordinates...'}
          style={{
            flex: 1,
            background: 'transparent',
            border: 'none',
            outline: 'none',
            color: 'var(--fg)',
            fontSize: 13,
            padding: '10px 8px',
            fontFamily: "'Plus Jakarta Sans', sans-serif",
            letterSpacing: '0.01em',
          }}
        />
        {loading && (
          <div style={{
            width: 14, height: 14,
            border: '2px solid var(--fg-20)',
            borderTopColor: 'var(--accent-primary)',
            borderRadius: '50%',
            animation: 'spin 0.6s linear infinite',
          }} />
        )}
        {!loading && query && (
          <button
            onClick={() => { setQuery(''); setResults([]); setOpen(false) }}
            style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--fg-30)', display: 'flex', alignItems: 'center' }}
            title="Clear search"
          >
            <span className="material-symbols-outlined" style={{ fontSize: 16 }}>close</span>
          </button>
        )}
      </div>

      {open && results.length > 0 && (
        <div style={{
          background: 'var(--ctrl-bg)',
          border: '1px solid var(--fg-10)',
          borderTop: 'none',
          maxHeight: 200,
          overflowY: 'auto',
          backdropFilter: 'blur(8px)',
        }}>
          {results.map((r, i) => (
            <button
              key={i}
              onClick={() => handleSelect(r)}
              style={{
                display: 'block',
                width: '100%',
                textAlign: 'left',
                padding: '12px',
                background: 'transparent',
                border: 'none',
                color: 'var(--fg-80)',
                fontSize: 12,
                cursor: 'pointer',
                borderBottom: i < results.length - 1 ? '1px solid var(--fg-05)' : 'none',
                fontFamily: "'Plus Jakarta Sans', sans-serif",
              }}
              onMouseEnter={(e) => (e.currentTarget.style.background = 'var(--fg-06)')}
              onMouseLeave={(e) => (e.currentTarget.style.background = 'transparent')}
            >
              <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                {useQueryStore.getState().createMode && (
                  <span className="material-symbols-outlined" style={{ fontSize: 14, color: 'var(--accent-primary)' }}>add_location_alt</span>
                )}
                {r.display_name.length > 55 ? r.display_name.slice(0, 55) + '...' : r.display_name}
              </span>
            </button>
          ))}
        </div>
      )}

      <style>{`@keyframes spin { to { transform: rotate(360deg); } }`}</style>
    </div>
  )
}
