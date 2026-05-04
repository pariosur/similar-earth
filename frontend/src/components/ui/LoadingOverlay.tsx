import { useQueryStore } from '../../stores/queryStore'

export function LoadingOverlay() {
  const queryStatus = useQueryStore((s) => s.queryStatus)
  const setQueryStatus = useQueryStore((s) => s.setQueryStatus)

  if (queryStatus === 'computing') {
    return (
      <div className="absolute inset-0 z-50 flex items-center justify-center bg-dark-900/70 backdrop-blur-sm">
        <div className="bg-dark-800/95 border border-fg-08 px-10 py-8 flex flex-col items-center gap-5 shadow-2xl">
          <svg
            className="animate-spin h-8 w-8 text-gold"
            viewBox="0 0 24 24"
            fill="none"
          >
            <circle
              className="opacity-25"
              cx="12"
              cy="12"
              r="10"
              stroke="currentColor"
              strokeWidth="4"
            />
            <path
              className="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"
            />
          </svg>
          <div className="text-center">
            <p className="text-xs font-bold uppercase tracking-[0.2em] text-fg-80">
              Scanning the Planet
            </p>
            <p className="text-xs text-fg-30 mt-2 animate-pulse-slow uppercase tracking-widest">
              Analyzing 37.5M land pixels
            </p>
          </div>
        </div>
      </div>
    )
  }

  if (queryStatus === 'failed') {
    return (
      <div className="absolute inset-0 z-50 flex items-center justify-center bg-dark-900/70 backdrop-blur-sm">
        <div className="bg-dark-800/95 border border-fg-08 px-10 py-8 flex flex-col items-center gap-5 shadow-2xl">
          <span className="material-symbols-outlined text-3xl text-crimson">error</span>
          <div className="text-center">
            <p className="text-xs font-bold uppercase tracking-[0.2em] text-fg-80">
              Computation Failed
            </p>
            <p className="text-xs text-fg-30 mt-2">
              Could not compute similarity. Please try again.
            </p>
          </div>
          <button
            onClick={() => setQueryStatus('idle')}
            className="text-[11px] px-6 py-2.5 bg-navy-light text-gold font-bold uppercase tracking-[0.15em] hover:bg-navy transition-colors"
          >
            Dismiss
          </button>
        </div>
      </div>
    )
  }

  return null
}
