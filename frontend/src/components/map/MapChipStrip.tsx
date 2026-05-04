import { useEffect, useState, useRef } from 'react'
import { listMaps, type MapInfo } from '../../api/client'
import { useQueryStore } from '../../stores/queryStore'
import { useSelectMap } from '../../hooks/useSelectMap'

const CATEGORY_ICONS: Record<string, string> = {
  'Agriculture': 'agriculture',
  'Energy': 'bolt',
  'Natural Ecosystems': 'forest',
  'Climate Risk': 'local_fire_department',
  'Other': 'more_horiz',
}

export function MapChipStrip() {
  const [maps, setMaps] = useState<MapInfo[]>([])
  const activeMapId = useQueryStore((s) => s.activeMapId)
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    listMaps('stars', 50)
      .then((all) => setMaps(all.filter((m) => m.is_featured)))
      .catch(() => setMaps([]))
  }, [])

  // Scroll active chip into view
  useEffect(() => {
    if (!activeMapId || !scrollRef.current) return
    const el = scrollRef.current.querySelector(`[data-map-id="${activeMapId}"]`) as HTMLElement | null
    if (el) el.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'center' })
  }, [activeMapId])

  if (maps.length === 0) return null

  return (
    <div
      ref={scrollRef}
      className="flex items-center gap-1.5 overflow-x-auto px-4 py-2"
      style={{
        scrollbarWidth: 'none',
        WebkitOverflowScrolling: 'touch',
      }}
    >
      {maps.map((map) => (
        <MapChip key={map.id} map={map} />
      ))}
    </div>
  )
}

function MapChip({ map }: { map: MapInfo }) {
  const activeMapId = useQueryStore((s) => s.activeMapId)
  const isActive = activeMapId === map.id
  const { selectMap, loading } = useSelectMap()
  const icon = CATEGORY_ICONS[map.category] || 'map'

  return (
    <button
      data-map-id={map.id}
      onClick={() => selectMap(map)}
      className={`flex items-center gap-1.5 px-3 py-1.5 shrink-0 transition-all text-[11px] font-bold uppercase tracking-wider ${
        isActive
          ? 'bg-gold text-navy'
          : 'bg-fg-05 text-fg-60 hover:bg-fg-08 hover:text-fg'
      }`}
      style={{
        border: '1px solid',
        borderColor: isActive ? 'var(--accent-primary)' : 'var(--fg-08)',
        fontFamily: "'Plus Jakarta Sans', sans-serif",
      }}
    >
      {loading ? (
        <svg className="animate-spin h-3 w-3 shrink-0" viewBox="0 0 24 24" fill="none">
          <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
          <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
        </svg>
      ) : (
        <span className="material-symbols-outlined text-sm shrink-0">{icon}</span>
      )}
      <span className="whitespace-nowrap">{map.title}</span>
      <span className="text-[8px] opacity-50">{map.pin_count}</span>
    </button>
  )
}
