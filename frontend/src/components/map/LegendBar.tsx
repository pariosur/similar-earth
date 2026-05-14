import { useQueryStore, type HdState } from '../../stores/queryStore'
import { useIsMobile } from '../../hooks/useIsMobile'

interface LegendBarProps {
  onPillClick: (target: 'global' | 'hd') => void
}

export function LegendBar({ onPillClick }: LegendBarProps) {
  const tileUrl = useQueryStore((s) => s.tileUrl)
  const hdState = useQueryStore((s) => s.hdState)
  const isMobile = useIsMobile()

  if (!tileUrl) return null

  const isGlobalActive = hdState === 'off'
  const isHdActive = hdState === 'zoomed' || hdState === 'computing' || hdState === 'loaded'

  return (
    <div className={`absolute left-1/2 -translate-x-1/2 flex flex-col items-center ${isMobile ? 'bottom-[var(--mobile-bottom-offset)] gap-1.5 z-[var(--z-legend)]' : 'bottom-6 gap-2 z-[var(--z-chip)]'}`}>
      {isMobile ? (
        <MobileBar
          isGlobalActive={isGlobalActive}
          isHdActive={isHdActive}
          hdState={hdState}
          onPillClick={onPillClick}
        />
      ) : (
        <DesktopBar
          isGlobalActive={isGlobalActive}
          isHdActive={isHdActive}
          hdState={hdState}
          onPillClick={onPillClick}
        />
      )}
    </div>
  )
}

interface BarProps {
  isGlobalActive: boolean
  isHdActive: boolean
  hdState: HdState
  onPillClick: (target: 'global' | 'hd') => void
}

function MobileBar({ isGlobalActive, hdState, onPillClick }: BarProps) {
  // Full-width CTA when HD is ready to trigger
  if (hdState === 'zoomed') {
    return (
      <button
        onClick={() => onPillClick('hd')}
        className="flex items-center justify-center gap-2 w-[280px] py-2.5 font-bold uppercase tracking-wider text-[11px]"
        style={{
          background: 'rgba(74, 222, 128, 0.15)',
          backdropFilter: 'blur(8px)',
          border: '1px solid rgba(74, 222, 128, 0.3)',
          color: '#4ade80',
          fontFamily: "'Plus Jakarta Sans', sans-serif",
          animation: 'pulse-green 2s ease-in-out infinite',
        }}
      >
        <span className="material-symbols-outlined text-base">location_searching</span>
        Scan at 10m resolution
      </button>
    )
  }

  // Scanning in progress
  if (hdState === 'computing') {
    return (
      <div
        className="glass-panel flex items-center justify-center gap-2 w-[280px] py-2.5 text-[11px] font-bold uppercase tracking-wider"
        style={{ color: 'var(--accent-primary)' }}
      >
        <span className="material-symbols-outlined text-base animate-spin">hourglass_top</span>
        Scanning...
      </div>
    )
  }

  // Normal compact bar (off or loaded)
  // Compact bar on mobile: only resolution toggle (legend lives in the bottom sheet)
  return (
    <div className="glass-panel flex items-center overflow-hidden" style={{ gap: 0 }}>
      <MobileResButton active={isGlobalActive} onClick={() => onPillClick('global')} label="2KM" sub="Global" />
      <div className="w-px h-5" style={{ background: 'var(--fg-10)' }} />
      <MobileResButton active={hdState === 'loaded'} onClick={() => onPillClick('hd')} label="10M" sub="Detail" />
    </div>
  )
}

function DesktopBar({ isGlobalActive, hdState, onPillClick }: BarProps) {
  // Resolution toggle only — similarity legend lives in MapLegend now
  return (
    <div className="flex overflow-hidden" style={{ background: 'var(--fg-08)', backdropFilter: 'blur(8px)', border: '1px solid var(--fg-10)' }}>
      <ResolutionButton active={isGlobalActive} onClick={() => onPillClick('global')}>
        <span className="material-symbols-outlined text-sm">language</span>
        <span>Global <span style={{ opacity: 0.6, fontSize: 10 }}>2km</span></span>
      </ResolutionButton>
      <ResolutionButton
        active={hdState === 'loaded' || hdState === 'computing'}
        onClick={() => onPillClick('hd')}
        disabled={hdState === 'computing'}
        hdReady={hdState === 'zoomed'}
        title={hdState === 'zoomed' ? 'Click to scan at 10m resolution' : hdState === 'computing' ? 'Scanning at 10m...' : hdState === 'loaded' ? '10m detail active' : 'Zoom in to enable 10m detail scan'}
        borderLeft
      >
        {hdState === 'computing' ? (
          <span className="inline-block animate-spin">
            <span className="material-symbols-outlined text-sm">hourglass_top</span>
          </span>
        ) : hdState === 'zoomed' ? (
          <span className="material-symbols-outlined text-sm" style={{ color: '#4ade80' }}>location_searching</span>
        ) : (
          <span className="material-symbols-outlined text-sm">location_searching</span>
        )}
        {hdState === 'computing' ? 'Scanning...' : hdState === 'zoomed' ? (
          <span>Detail <span style={{ opacity: 0.8, fontSize: 10 }}>Click to scan</span></span>
        ) : hdState === 'loaded' ? (
          <span>Detail <span style={{ opacity: 0.6, fontSize: 10 }}>10m active</span></span>
        ) : (
          <span>Detail <span style={{ opacity: 0.4, fontSize: 10 }}>Zoom in</span></span>
        )}
      </ResolutionButton>
    </div>
  )
}

interface ResolutionButtonProps {
  active: boolean
  onClick: () => void
  disabled?: boolean
  borderLeft?: boolean
  compact?: boolean
  hdReady?: boolean
  title?: string
  children: React.ReactNode
}

function ResolutionButton({ active, onClick, disabled, borderLeft, compact, hdReady, title, children }: ResolutionButtonProps) {
  let bg = active ? 'var(--nav-bg)' : 'transparent'
  let color = active ? 'var(--fg)' : 'var(--fg-50)'

  if (hdReady) {
    bg = 'rgba(74, 222, 128, 0.15)'
    color = '#4ade80'
  }

  return (
    <button
      onClick={onClick}
      disabled={disabled}
      title={title}
      className="flex items-center gap-1.5 font-bold uppercase tracking-wider transition-all"
      style={{
        background: bg,
        color,
        border: 'none',
        borderLeft: borderLeft ? '1px solid var(--fg-10)' : undefined,
        padding: compact ? '6px 10px' : '7px 16px',
        fontSize: compact ? 10 : 11,
        letterSpacing: '0.1em',
        cursor: disabled ? 'wait' : 'pointer',
        fontFamily: "'Plus Jakarta Sans', sans-serif",
        animation: hdReady ? 'pulse-green 2s ease-in-out infinite' : undefined,
      }}
    >
      {children}
    </button>
  )
}

function MobileResButton({ active, onClick, label, sub }: { active: boolean; onClick: () => void; label: string; sub: string }) {
  return (
    <button
      onClick={onClick}
      className="flex flex-col items-center py-1 px-2.5 transition-all"
      style={{
        background: active ? 'var(--nav-bg)' : 'transparent',
        color: active ? 'var(--fg)' : 'var(--fg-50)',
        border: 'none',
        fontFamily: "'Plus Jakarta Sans', sans-serif",
      }}
    >
      <span className="text-[10px] font-bold uppercase tracking-wider leading-none">{label}</span>
      <span className="text-[7px] uppercase tracking-wider leading-none mt-0.5" style={{ opacity: 0.5 }}>{sub}</span>
    </button>
  )
}
