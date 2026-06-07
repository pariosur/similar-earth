import { useThemeStore } from '../../stores/themeStore'
import { useIsMobile } from '../../hooks/useIsMobile'

/**
 * Always-on Map / Satellite basemap switcher.
 * Independent of any active similarity map — also useful while placing pins in Create mode.
 * Desktop: top-right. Mobile: bottom-left (clears the centered search + bottom sheet).
 */
export function BasemapToggle() {
  const basemap = useThemeStore((s) => s.basemap)
  const setBasemap = useThemeStore((s) => s.setBasemap)
  const isMobile = useIsMobile()

  const pos = isMobile
    ? 'bottom-[var(--mobile-bottom-offset)] left-3'
    : 'top-4 right-4'

  return (
    <div className={`absolute ${pos} z-[var(--z-chip)] glass-panel flex overflow-hidden`}>
      <BasemapButton active={basemap === 'map'} onClick={() => setBasemap('map')} icon="map" label="Map" value="map" />
      <div className="w-px self-stretch" style={{ background: 'var(--fg-10)' }} />
      <BasemapButton active={basemap === 'satellite'} onClick={() => setBasemap('satellite')} icon="satellite_alt" label="Satellite" value="satellite" />
    </div>
  )
}

function BasemapButton({
  active,
  onClick,
  icon,
  label,
  value,
}: {
  active: boolean
  onClick: () => void
  icon: string
  label: string
  value: string
}) {
  return (
    <button
      onClick={onClick}
      data-basemap={value}
      className="flex items-center gap-1.5 font-bold uppercase tracking-wider transition-all"
      style={{
        background: active ? 'var(--nav-bg)' : 'transparent',
        color: active ? 'var(--fg)' : 'var(--fg-50)',
        border: 'none',
        padding: '7px 14px',
        fontSize: 11,
        letterSpacing: '0.1em',
        cursor: 'pointer',
        fontFamily: "'Plus Jakarta Sans', sans-serif",
      }}
    >
      <span className="material-symbols-outlined" style={{ fontSize: 15 }}>{icon}</span>
      {label}
    </button>
  )
}
