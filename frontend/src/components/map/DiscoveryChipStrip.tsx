import { useEffect, useRef } from 'react'
import { useDiscoveries } from '../../hooks/useDiscoveries'
import { useQueryStore } from '../../stores/queryStore'

interface DiscoveryChipStripProps {
  onFlyTo?: (lat: number, lng: number) => void
  limit?: number
}

export function DiscoveryChipStrip({ onFlyTo, limit = 10 }: DiscoveryChipStripProps) {
  const { discoveries, loading } = useDiscoveries()
  const selectedDiscoveryIndex = useQueryStore((s) => s.selectedDiscoveryIndex)
  const setSelectedDiscovery = useQueryStore((s) => s.setSelectedDiscovery)
  const scrollRef = useRef<HTMLDivElement>(null)

  // Scroll selected chip into view
  useEffect(() => {
    if (selectedDiscoveryIndex === null || !scrollRef.current) return
    const el = scrollRef.current.querySelector(`[data-discovery-idx="${selectedDiscoveryIndex}"]`) as HTMLElement | null
    if (el) el.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'center' })
  }, [selectedDiscoveryIndex])

  if (loading) {
    return (
      <div className="flex items-center gap-2 px-4 py-1.5 overflow-hidden" style={{ minHeight: 32 }}>
        <svg className="animate-spin h-3 w-3 shrink-0" viewBox="0 0 24 24" fill="none" style={{ color: 'var(--accent-primary)' }}>
          <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
          <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
        </svg>
        <span className="text-[10px] uppercase tracking-wider font-bold animate-pulse-slow" style={{ color: 'var(--fg-40)' }}>
          Finding top matches...
        </span>
      </div>
    )
  }

  if (discoveries.length === 0) return null

  const shown = discoveries.slice(0, limit)

  return (
    <div
      ref={scrollRef}
      className="flex items-center gap-1.5 overflow-x-auto px-4 py-1.5"
      style={{
        scrollbarWidth: 'none',
        WebkitOverflowScrolling: 'touch',
      }}
    >
      {shown.map((d, i) => {
        const isActive = selectedDiscoveryIndex === i
        return (
          <button
            key={`${d.lat},${d.lng}`}
            data-discovery-idx={i}
            onClick={() => {
              setSelectedDiscovery(i)
              onFlyTo?.(d.lat, d.lng)
            }}
            className={`flex items-center gap-1 px-2.5 py-1 shrink-0 transition-all text-[10px] font-medium ${
              isActive ? 'text-white' : 'text-fg-60 hover:text-fg'
            }`}
            style={{
              background: isActive ? 'var(--crimson)' : 'var(--fg-05)',
              border: '1px solid',
              borderColor: isActive ? 'var(--crimson)' : 'var(--fg-08)',
              fontFamily: "'Plus Jakarta Sans', sans-serif",
            }}
          >
            <span className="material-symbols-outlined text-[12px] shrink-0">location_on</span>
            <span className="whitespace-nowrap">{d.name}</span>
          </button>
        )
      })}
    </div>
  )
}
