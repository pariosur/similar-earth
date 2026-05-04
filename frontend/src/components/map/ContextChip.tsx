import { useState } from 'react'
import { useQueryStore } from '../../stores/queryStore'
import { useIsMobile } from '../../hooks/useIsMobile'

export function ContextChip() {
  const activeMapName = useQueryStore((s) => s.activeMapName)
  const pins = useQueryStore((s) => s.pins)
  const tileUrl = useQueryStore((s) => s.tileUrl)
  const queryStatus = useQueryStore((s) => s.queryStatus)
  const isMobile = useIsMobile()
  const [shared, setShared] = useState(false)

  if (!tileUrl && queryStatus !== 'computing') return null
  const isComputing = queryStatus === 'computing'

  async function handleShare() {
    const url = window.location.href
    const title = `Similar Earth — ${activeMapName || 'Custom query'}`
    if (isMobile && navigator.share) {
      try { await navigator.share({ title, url }) } catch {}
    } else {
      await navigator.clipboard.writeText(url)
      setShared(true)
      setTimeout(() => setShared(false), 2000)
    }
  }

  return (
    <div className={`absolute top-[var(--context-chip-top)] left-1/2 -translate-x-1/2 z-[var(--z-chip)] glass-panel flex items-center gap-1.5 pl-3.5 pr-1.5 py-1 ${isComputing ? 'animate-pulse-slow' : ''}`}>
      {queryStatus === 'computing' && (
        <svg className="animate-spin h-3 w-3 shrink-0" viewBox="0 0 24 24" fill="none" style={{ color: 'var(--accent-primary)' }}>
          <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
          <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
        </svg>
      )}
      <span className="text-[10px] font-bold uppercase tracking-wider" style={{ color: 'var(--fg-40)' }}>
        {queryStatus === 'computing' ? 'Scanning for' : 'Showing similarity for'}
      </span>
      <span className="text-[11px] font-bold" style={{ color: 'var(--accent-primary)' }}>
        {activeMapName || 'Custom query'}
      </span>
      <span className="text-[10px]" style={{ color: 'var(--fg-25)' }}>&middot;</span>
      <span className="text-[10px] font-bold uppercase tracking-wider" style={{ color: 'var(--fg-40)' }}>
        {pins.length > 0 ? `from ${pins.length} pin${pins.length !== 1 ? 's' : ''}` : 'loading...'}
      </span>
      <button
        onClick={handleShare}
        className="flex items-center gap-1 px-1.5 py-0.5 transition-colors"
        style={{ color: shared ? 'var(--accent-primary)' : 'var(--fg-30)', background: 'none', border: 'none', cursor: 'pointer' }}
        title="Share this map"
      >
        <span className="material-symbols-outlined text-[16px]">
          {shared ? 'check' : isMobile ? 'share' : 'content_copy'}
        </span>
        {shared && <span className="text-[10px] font-bold">Copied!</span>}
      </button>
    </div>
  )
}
