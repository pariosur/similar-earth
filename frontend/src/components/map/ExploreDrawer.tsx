import { useState, useEffect, useRef } from 'react'
import { useQueryStore } from '../../stores/queryStore'
import { useDiscoveries } from '../../hooks/useDiscoveries'
import { useIsMobile } from '../../hooks/useIsMobile'

type Mode = 'discoveries' | 'pins'

interface ExploreDrawerProps {
  onFlyTo?: (lat: number, lng: number) => void
}

/**
 * Desktop bottom drawer for navigating discoveries + reference pins.
 * Horizontal scrollable cards below the map.
 * Hidden on mobile (chip strips in the bottom sheet handle it there).
 */
export function ExploreDrawer({ onFlyTo }: ExploreDrawerProps) {
  const { discoveries, loading } = useDiscoveries()
  const queryStatus = useQueryStore((s) => s.queryStatus)
  const pins = useQueryStore((s) => s.pins)
  const tileUrl = useQueryStore((s) => s.tileUrl)
  const selectedDiscoveryIndex = useQueryStore((s) => s.selectedDiscoveryIndex)
  const selectedPinIndex = useQueryStore((s) => s.selectedPinIndex)
  const setSelectedDiscovery = useQueryStore((s) => s.setSelectedDiscovery)
  const setSelectedPin = useQueryStore((s) => s.setSelectedPin)
  const isMobile = useIsMobile()
  const [mode, setMode] = useState<Mode>('pins')
  const [expanded, setExpanded] = useState(true)
  const scrollRef = useRef<HTMLDivElement>(null)

  // Auto-scroll to selected item
  useEffect(() => {
    if (selectedDiscoveryIndex !== null) {
      setMode('discoveries')
      setTimeout(() => {
        const el = scrollRef.current?.querySelector(`[data-d="${selectedDiscoveryIndex}"]`)
        el?.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'center' })
      }, 50)
    }
  }, [selectedDiscoveryIndex])

  useEffect(() => {
    if (selectedPinIndex !== null) {
      setMode('pins')
      setTimeout(() => {
        const el = scrollRef.current?.querySelector(`[data-p="${selectedPinIndex}"]`)
        el?.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'center' })
      }, 50)
    }
  }, [selectedPinIndex])

  // Hide on mobile (chip strips handle it) or when no map is active
  if (isMobile || !tileUrl || queryStatus !== 'ready') return null

  return (
    <div className="always-dark border-t border-fg-08 bg-dark-800/95 backdrop-blur-md shrink-0">
      {/* Header bar — always visible */}
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center justify-between px-4 py-2 hover:bg-fg-03 transition-colors"
      >
        <div className="flex items-center gap-4">
          <span className="material-symbols-outlined text-sm text-gold">explore</span>
          {/* Tabs inline */}
          <button
            onClick={(e) => { e.stopPropagation(); setMode('pins'); setExpanded(true) }}
            className="text-[10px] font-bold uppercase tracking-wider py-0.5 transition-colors"
            style={{
              color: mode === 'pins' ? 'var(--accent-primary)' : 'var(--fg-30)',
              borderBottom: mode === 'pins' ? '1px solid var(--accent-primary)' : '1px solid transparent',
            }}
          >
            {useQueryStore.getState().createMode ? 'Your Pins' : 'Reference Pins'} ({pins.length})
          </button>
          <button
            onClick={(e) => { e.stopPropagation(); setMode('discoveries'); setExpanded(true) }}
            className="text-[10px] font-bold uppercase tracking-wider py-0.5 transition-colors"
            style={{
              color: mode === 'discoveries' ? 'var(--crimson)' : 'var(--fg-30)',
              borderBottom: mode === 'discoveries' ? '1px solid var(--crimson)' : '1px solid transparent',
            }}
          >
            Most Similar ({discoveries.length})
          </button>
        </div>
        <span
          className="material-symbols-outlined text-sm text-fg-25"
          style={{ transform: expanded ? 'rotate(180deg)' : 'none', transition: 'transform 0.15s' }}
        >
          expand_less
        </span>
      </button>

      {/* Scrollable cards */}
      {expanded && (
        <div
          ref={scrollRef}
          className="flex items-stretch gap-2 overflow-x-auto px-4 pb-3 pt-1"
          style={{ scrollbarWidth: 'none', WebkitOverflowScrolling: 'touch' }}
        >
          {loading && (
            <div className="flex items-center gap-2 px-4 py-3 shrink-0">
              <svg className="animate-spin h-3 w-3" viewBox="0 0 24 24" fill="none" style={{ color: 'var(--accent-primary)' }}>
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
              </svg>
              <span className="text-[10px] text-fg-40 uppercase tracking-wider font-bold animate-pulse-slow">Finding matches...</span>
            </div>
          )}

          {mode === 'discoveries' && !loading && discoveries.map((d, i) => {
            const isSelected = selectedDiscoveryIndex === i
            return (
              <button
                key={`${d.lat}-${d.lng}`}
                data-d={i}
                onClick={() => {
                  setSelectedDiscovery(isSelected ? null : i)
                  onFlyTo?.(d.lat, d.lng)
                }}
                className={`shrink-0 text-left px-3 py-2 transition-all border ${
                  isSelected ? 'border-crimson bg-navy-light/40' : 'border-fg-08 hover:border-fg-20 hover:bg-fg-03'
                }`}
                style={{ minWidth: 180, maxWidth: 220 }}
              >
                <div className="flex items-center gap-2 mb-1">
                  <span
                    className="w-4 h-4 rounded-full text-[8px] font-bold flex items-center justify-center shrink-0"
                    style={{ background: 'var(--crimson)', color: '#fff' }}
                  >
                    {i + 1}
                  </span>
                  <span className="text-[11px] font-medium text-fg-80 truncate">{d.name}</span>
                  <span className="text-[10px] font-bold ml-auto shrink-0" style={{
                    color: d.score > 0.8 ? '#B04632' : d.score > 0.65 ? '#E3B448' : '#ffce5f'
                  }}>
                    <span title="Satellite similarity score">{Math.round(d.score * 100)}%</span>
                  </span>
                </div>
                {d.similar_to && (
                  <div className="text-[9px] text-fg-30 truncate">Similar to {d.similar_to}</div>
                )}
              </button>
            )
          })}

          {mode === 'pins' && pins.map((pin, i) => {
            const isSelected = selectedPinIndex === i
            const matchCount = discoveries.filter((d) => d.best_pin_index === i).length
            return (
              <button
                key={`${pin.lat}-${pin.lng}-${i}`}
                data-p={i}
                onClick={() => {
                  setSelectedPin(isSelected ? null : i)
                  onFlyTo?.(pin.lat, pin.lng)
                }}
                className={`shrink-0 text-left px-3 py-2 transition-all border ${
                  isSelected ? 'border-gold bg-navy-light/40' : 'border-fg-08 hover:border-fg-20 hover:bg-fg-03'
                }`}
                style={{ minWidth: 180, maxWidth: 220 }}
              >
                <div className="flex items-center gap-2 mb-1">
                  <span
                    className="w-4 h-4 text-[8px] font-bold flex items-center justify-center shrink-0"
                    style={{ background: 'var(--pin-bg)', color: 'var(--pin-text)', border: '1px solid var(--pin-border)' }}
                  >
                    {i + 1}
                  </span>
                  <span className="text-[11px] font-medium text-fg-80 truncate">
                    {pin.label || `${pin.lat.toFixed(4)}, ${pin.lng.toFixed(4)}`}
                  </span>
                </div>
                {matchCount > 0 && (
                  <div className="text-[9px] text-fg-30">{matchCount} {matchCount === 1 ? 'match' : 'matches'}</div>
                )}
              </button>
            )
          })}

        </div>
      )}
    </div>
  )
}
