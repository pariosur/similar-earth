import { useEffect, useRef } from 'react'
import { postQuery, getQueryStatus, searchPlaces, type NaturalSearchMatch } from '../../api/client'
import { useSearchStore } from '../../stores/searchStore'

const examples = [
  'Places like Tuscany, but warmer and outside Europe',
  'Places like Atacama Desert, only in South America',
  'Find warmer places like Alentejo, outside Europe',
]

export function NaturalSearch({
  onFlyTo,
  onResults,
}: {
  onFlyTo?: (lat: number, lng: number) => void
  // Called when a search returns matches (mobile collapses the sheet to show the map)
  onResults?: () => void
}) {
  const state = useSearchStore()
  const { update } = state
  const listRef = useRef<HTMLDivElement>(null)

  // Scroll to the card whose map marker was clicked
  useEffect(() => {
    if (state.selected === null) return
    listRef.current?.querySelector(`[data-result="${state.selected}"]`)?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
  }, [state.selected])

  const search = async (text = state.prompt) => {
    if (!text.trim() || state.loading) return
    const context = state.context
    update({ prompt: text, loading: true, error: '', matches: [], reference: null, theme: null, references: [], clarification: '', explanation: '', explored: null, hovered: null, selected: null })
    try {
      const result = await searchPlaces(text, context)
      const matches = result.matches || []
      update({
        matches,
        reference: result.reference || null,
        theme: result.theme || null,
        references: result.references || (result.reference ? [result.reference] : []),
        clarification: result.clarification || result.unsupported?.join(' ') || '',
        explanation: result.explanation || '',
        context: result.context,
      })
      if (matches.length) onResults?.()
    } catch (e) {
      update({ error: e instanceof Error ? e.message : 'Search failed. Try again.' })
    } finally {
      update({ loading: false })
    }
  }

  // Toggle a 2 km similarity heatmap for one result, shown in this view.
  const explore = async (match: NaturalSearchMatch, index: number) => {
    if (state.explored?.index === index) {
      update({ explored: null })
      return
    }
    update({ exploring: index, error: '' })
    try {
      const response = await postQuery([{ lat: match.lat, lng: match.lng, label: match.name }])
      const deadline = Date.now() + 120_000
      while (Date.now() < deadline) {
        const status = await getQueryStatus(response.id)
        if (status.status === 'completed') {
          update({ explored: { index, name: match.name, queryId: response.id, tileUrl: response.tile_url } })
          return
        }
        if (status.status === 'failed') throw new Error('Similarity scan failed. Try again.')
        await new Promise((resolve) => window.setTimeout(resolve, 1500))
      }
      throw new Error('Similarity scan is taking too long. Try again later.')
    } catch (e) {
      update({ error: e instanceof Error ? e.message : 'Could not explore this place.' })
    } finally {
      update({ exploring: null })
    }
  }

  const viewOnMap = (match: NaturalSearchMatch, index: number) => {
    update({ selected: index })
    onFlyTo?.(match.lat, match.lng)
  }

  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <p className="text-[10px] font-bold uppercase tracking-[0.18em] text-gold">Search Earth</p>
          <h2 className="text-sm font-semibold text-fg mt-1">Describe a place to find</h2>
        </div>
        {state.context && <button onClick={state.reset} className="text-[10px] font-bold uppercase tracking-wider text-fg-50 hover:text-gold">New search</button>}
      </div>
      <form onSubmit={(e) => { e.preventDefault(); void search() }} className="space-y-2">
        <textarea
          value={state.prompt}
          onChange={(e) => update({ prompt: e.target.value })}
          maxLength={500}
          rows={3}
          aria-label="Describe places to find"
          placeholder="Places like Tuscany, but warmer and outside Europe…"
          className="w-full resize-y bg-fg-05 border border-fg-10 p-3 text-sm text-fg placeholder-fg-40 outline-none focus:border-gold/50"
        />
        <div className="flex items-center justify-between">
          <span className="text-[10px] text-fg-40">Up to 20 matches · 2 km similarity</span>
          <button disabled={state.loading || !state.prompt.trim()} className="bg-gold px-3 py-2 text-[10px] font-bold uppercase tracking-wider text-dark-900 disabled:opacity-50">
            {state.loading ? 'Searching…' : 'Find places'}
          </button>
        </div>
      </form>
      <p className="text-[11px] text-fg-40">Matches are numbered on the map. Show heatmap scans the planet for places like one result (2 km).</p>
      <p className="text-[10px] text-fg-30">Place names: GeoNames. Annual mean temperature (1991–2020): Copernicus Climate Change Service.</p>
      {!state.matches.length && !state.clarification && (
        <div className="space-y-2">
          <p className="text-[10px] font-bold uppercase tracking-wider text-fg-40">Try an example</p>
          {examples.map((example) => <button key={example} onClick={() => { update({ prompt: example }); void search(example) }} className="block w-full border border-fg-08 px-3 py-2 text-left text-xs text-fg-70 hover:border-gold/40">{example}</button>)}
        </div>
      )}
      {state.loading && <p role="status" className="text-xs text-fg-50">Searching places and applying your filters…</p>}
      {state.error && <p role="alert" className="border border-crimson/30 bg-crimson/5 p-3 text-xs text-crimson">{state.error}</p>}
      {state.clarification && <p role="status" className="border border-gold/20 bg-gold/5 p-3 text-xs text-fg-70">{state.clarification}</p>}
      {!!state.matches.length && <>
        <div className="flex items-center justify-between border-t border-fg-08 pt-3">
          <p className="text-[10px] font-bold uppercase tracking-wider text-fg-40">{state.matches.length} matches</p>
          <button onClick={state.reset} className="text-[10px] text-fg-40 hover:text-fg">Clear</button>
        </div>
        {state.explanation && <p className="text-xs text-fg-50">{state.explanation}</p>}
        {state.theme && (
          <div className="flex w-full items-center gap-2 border border-gold/20 bg-gold/5 px-3 py-2 text-xs text-fg-70">
            <span className="material-symbols-outlined text-sm text-gold">star</span>
            <span className="text-[10px] font-bold uppercase tracking-wider text-fg-40">Theme</span>
            <span className="truncate font-semibold text-fg">{state.theme.name}</span>
            <span className="ml-auto shrink-0 text-[10px] text-fg-50">{state.references.length} reference sites</span>
          </div>
        )}
        {state.reference && (
          <button
            onClick={() => onFlyTo?.(state.reference!.lat, state.reference!.lng)}
            className="flex w-full items-center gap-2 border border-gold/20 bg-gold/5 px-3 py-2 text-left text-xs text-fg-70 hover:border-gold/40"
          >
            <span className="material-symbols-outlined text-sm text-gold">star</span>
            <span className="text-[10px] font-bold uppercase tracking-wider text-fg-40">Reference</span>
            <span className="truncate font-semibold text-fg">{state.reference.name}</span>
          </button>
        )}
        <div ref={listRef} className="space-y-2">
          {state.matches.map((match, i) => {
            const active = state.hovered === i || state.selected === i
            const explored = state.explored?.index === i
            return <article
              key={`${match.lat},${match.lng}`}
              data-result={i}
              onMouseEnter={() => update({ hovered: i })}
              onMouseLeave={() => update({ hovered: null })}
              className={`border p-3 transition-colors ${active || explored ? 'border-gold/50 bg-gold/5' : 'border-fg-08 bg-fg-03'}`}
            >
              <div className="flex justify-between gap-2">
                <h3 className="flex items-start gap-2 text-xs font-semibold text-fg">
                  <span className="search-result-badge">{i + 1}</span>
                  <span>{match.name}</span>
                </h3>
                <span className="shrink-0 text-xs text-gold">{Math.round(match.score * 100)}%</span>
              </div>
              {match.similar_to && <p className="mt-1 pl-7 text-[11px] text-fg-50">Closest to {match.similar_to}</p>}
              {match.temperature_difference_c != null && <p className="mt-1 pl-7 text-[11px] text-fg-50">{match.temperature_difference_c > 0 ? '+' : ''}{match.temperature_difference_c.toFixed(1)}°C vs reference</p>}
              <div className="mt-3 flex gap-2 pl-7">
                <button onClick={() => viewOnMap(match, i)} className="border border-fg-15 px-2 py-1.5 text-[10px] text-fg-70 hover:text-gold">View on map</button>
                <button
                  disabled={state.exploring !== null}
                  onClick={() => void explore(match, i)}
                  className="border border-gold/40 bg-gold/10 px-2 py-1.5 text-[10px] text-gold disabled:opacity-50"
                >
                  {state.exploring === i ? 'Scanning…' : explored ? 'Hide heatmap' : 'Show heatmap'}
                </button>
              </div>
            </article>
          })}
        </div>
      </>}
    </section>
  )
}
