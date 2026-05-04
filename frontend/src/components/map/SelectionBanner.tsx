import { useQueryStore } from '../../stores/queryStore'
import { useDiscoveries } from '../../hooks/useDiscoveries'

export function SelectionBanner() {
  const selectedPinIndex = useQueryStore((s) => s.selectedPinIndex)
  const selectedDiscoveryIndex = useQueryStore((s) => s.selectedDiscoveryIndex)
  const pins = useQueryStore((s) => s.pins)
  const clearSelection = useQueryStore((s) => s.clearSelection)
  const { discoveries } = useDiscoveries()

  if (selectedPinIndex === null && selectedDiscoveryIndex === null) return null

  let content: React.ReactNode = null

  if (selectedDiscoveryIndex !== null) {
    const d = discoveries[selectedDiscoveryIndex]
    if (!d) return null
    content = (
      <>
        <span className="text-[10px] font-bold uppercase tracking-wider" style={{ color: 'var(--crimson)' }}>
          Most Similar #{selectedDiscoveryIndex + 1}
        </span>
        <span className="text-[11px] font-bold" style={{ color: 'var(--fg-80)' }}>{d.name}</span>
        {d.similar_to && (
          <>
            <span className="text-[10px]" style={{ color: 'var(--fg-25)' }}>·</span>
            <span className="text-[10px]" style={{ color: 'var(--fg-50)' }}>
              similar to <span style={{ color: 'var(--accent-primary)' }}>{d.similar_to}</span>
            </span>
          </>
        )}
      </>
    )
  } else if (selectedPinIndex !== null) {
    const pin = pins[selectedPinIndex]
    if (!pin) return null
    const matchCount = discoveries.filter((d) => d.best_pin_index === selectedPinIndex).length
    content = (
      <>
        <span className="text-[10px] font-bold uppercase tracking-wider" style={{ color: 'var(--accent-primary)' }}>
          Your Pin #{selectedPinIndex + 1}
        </span>
        <span className="text-[11px] font-bold" style={{ color: 'var(--fg-80)' }}>{pin.label || `${pin.lat.toFixed(2)}, ${pin.lng.toFixed(2)}`}</span>
        {matchCount > 0 && (
          <>
            <span className="text-[10px]" style={{ color: 'var(--fg-25)' }}>·</span>
            <span className="text-[10px]" style={{ color: 'var(--fg-50)' }}>
              {matchCount} {matchCount === 1 ? 'match' : 'matches'} found
            </span>
          </>
        )}
      </>
    )
  }

  return (
    <div
      className="absolute top-[7rem] left-1/2 -translate-x-1/2 z-[var(--z-chip)] glass-panel flex items-center gap-1.5 pl-3.5 pr-1.5 py-1 max-w-[calc(100vw-2rem)]"
    >
      {content}
      <button
        onClick={clearSelection}
        className="flex items-center justify-center px-1.5 py-0.5"
        style={{ color: 'var(--fg-30)', background: 'none', border: 'none', cursor: 'pointer' }}
        title="Clear selection (Esc)"
      >
        <span className="material-symbols-outlined text-[16px]">close</span>
      </button>
    </div>
  )
}
