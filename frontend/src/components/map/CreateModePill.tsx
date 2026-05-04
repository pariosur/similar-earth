import { useQueryStore } from '../../stores/queryStore'
import { useIsMobile } from '../../hooks/useIsMobile'

interface CreateModePillProps {
  onDone: () => void
  sidebarOpen: boolean
}

/**
 * Floating pill shown during mobile create mode (Phase 1: pin placement).
 * The sheet is collapsed, giving the user full-screen map access.
 * This pill shows pin count and a "Done" button to proceed to the form.
 */
export function CreateModePill({ onDone, sidebarOpen }: CreateModePillProps) {
  const createMode = useQueryStore((s) => s.createMode)
  const pins = useQueryStore((s) => s.pins)
  const clearPins = useQueryStore((s) => s.clearPins)
  const setCreateMode = useQueryStore((s) => s.setCreateMode)
  const isMobile = useIsMobile()

  // Only show when: mobile + create mode + sheet is collapsed (Phase 1)
  if (!isMobile || !createMode || sidebarOpen) return null

  return (
    <div
      className="fixed left-1/2 -translate-x-1/2 z-[var(--z-panel)] glass-panel always-dark flex items-center gap-3 px-4 py-2.5"
      style={{ bottom: 'calc(var(--mobile-sheet-collapsed) + 1rem)' }}
    >
      {/* Crosshair hint */}
      <span className="material-symbols-outlined text-base text-gold">add_location_alt</span>

      {/* Pin count + guidance */}
      <span className="text-[12px] font-bold text-fg">
        {pins.length === 0
          ? 'Tap to drop pins'
          : pins.length < 3
            ? `${pins.length} pin${pins.length !== 1 ? 's' : ''} · add more`
            : `${pins.length} pins`
        }
      </span>

      {/* Undo last pin */}
      {pins.length > 0 && (
        <button
          onClick={() => useQueryStore.getState().removePin(pins.length - 1)}
          className="text-fg-30 hover:text-fg-60 transition-colors"
          title="Undo last pin"
        >
          <span className="material-symbols-outlined text-base">undo</span>
        </button>
      )}

      {/* Cancel */}
      <button
        onClick={() => {
          clearPins()
          setCreateMode(false)
        }}
        className="text-[10px] font-bold uppercase tracking-wider px-2 py-1.5 transition-colors text-fg-40 hover:text-crimson"
        style={{ fontFamily: "'Plus Jakarta Sans', sans-serif" }}
      >
        Cancel
      </button>

      {/* Done — proceed to form */}
      {pins.length >= 1 && (
        <button
          onClick={onDone}
          className="text-[10px] font-bold uppercase tracking-wider px-3 py-1.5 transition-colors"
          style={{
            background: 'var(--accent-primary)',
            color: 'var(--nav-active)',
            fontFamily: "'Plus Jakarta Sans', sans-serif",
          }}
        >
          Done
        </button>
      )}
    </div>
  )
}
