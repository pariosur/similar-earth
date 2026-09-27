import { useSearchStore } from '../../stores/searchStore'

/** Top-of-map summary for Find places, in place of the gallery's ContextChip. */
export function SearchContextChip() {
  const matches = useSearchStore((s) => s.matches)
  const referenceName = useSearchStore((s) => s.reference?.name || s.context?.reference?.name)
  const explored = useSearchStore((s) => s.explored)
  const exploring = useSearchStore((s) => s.exploring)
  const update = useSearchStore((s) => s.update)

  if (!matches.length) return null

  return (
    <div className={`absolute top-[var(--context-chip-top)] left-1/2 -translate-x-1/2 w-max z-[var(--z-chip)] glass-panel flex items-center gap-1.5 pl-3.5 ${explored ? 'pr-1.5' : 'pr-3.5'} py-1 max-w-[calc(100%-2rem)] ${exploring !== null ? 'animate-pulse-slow' : ''}`}>
      <span className="text-[10px] font-bold uppercase tracking-wider shrink-0" style={{ color: 'var(--fg-40)' }}>
        {explored ? 'Heatmap for' : 'Places like'}
      </span>
      <span className="text-[11px] font-bold truncate" style={{ color: 'var(--accent-primary)' }}>
        {explored ? `#${explored.index + 1} ${explored.name}` : referenceName || 'your search'}
      </span>
      <span className="text-[10px] shrink-0" style={{ color: 'var(--fg-25)' }}>&middot;</span>
      <span className="text-[10px] font-bold uppercase tracking-wider shrink-0" style={{ color: 'var(--fg-40)' }}>
        {exploring !== null ? 'scanning…' : `${matches.length} matches`}
      </span>
      {explored && (
        <button
          onClick={() => update({ explored: null })}
          className="flex items-center px-1.5 py-0.5"
          style={{ color: 'var(--fg-30)', background: 'none', border: 'none', cursor: 'pointer' }}
          title="Hide heatmap"
        >
          <span className="material-symbols-outlined text-[16px]">close</span>
        </button>
      )}
    </div>
  )
}
