import { useEffect, useRef } from 'react'
import { useQueryStore } from '../../stores/queryStore'

interface PinChipStripProps {
  onFlyTo?: (lat: number, lng: number) => void
}

export function PinChipStrip({ onFlyTo }: PinChipStripProps) {
  const pins = useQueryStore((s) => s.pins)
  const selectedPinIndex = useQueryStore((s) => s.selectedPinIndex)
  const setSelectedPin = useQueryStore((s) => s.setSelectedPin)
  const scrollRef = useRef<HTMLDivElement>(null)

  // Scroll selected chip into view
  useEffect(() => {
    if (selectedPinIndex === null || !scrollRef.current) return
    const el = scrollRef.current.querySelector(`[data-pin-idx="${selectedPinIndex}"]`) as HTMLElement | null
    if (el) el.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'center' })
  }, [selectedPinIndex])

  if (pins.length === 0) return null

  return (
    <div
      ref={scrollRef}
      className="flex items-center gap-1.5 overflow-x-auto px-4 py-1.5"
      style={{
        scrollbarWidth: 'none',
        WebkitOverflowScrolling: 'touch',
      }}
    >
      {pins.map((pin, i) => {
        const isActive = selectedPinIndex === i
        const label = pin.label || `${pin.lat.toFixed(2)}, ${pin.lng.toFixed(2)}`
        return (
          <button
            key={`${pin.lat},${pin.lng},${i}`}
            data-pin-idx={i}
            onClick={() => {
              setSelectedPin(i)
              onFlyTo?.(pin.lat, pin.lng)
            }}
            className={`flex items-center gap-1 px-2.5 py-1 shrink-0 transition-all text-[10px] font-medium ${
              isActive ? 'text-navy' : 'text-fg-60 hover:text-fg'
            }`}
            style={{
              background: isActive ? 'var(--accent-primary)' : 'var(--fg-05)',
              border: '1px solid',
              borderColor: isActive ? 'var(--accent-primary)' : 'var(--fg-08)',
              fontFamily: "'Plus Jakarta Sans', sans-serif",
            }}
          >
            <span className="text-[10px] font-bold shrink-0">#{i + 1}</span>
            <span className="whitespace-nowrap">{label}</span>
          </button>
        )
      })}
    </div>
  )
}
