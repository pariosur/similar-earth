import { postQuery, getQueryStatus, searchPlaces, type NaturalSearchMatch, type NaturalSearchContext } from '../../api/client'
import { useQueryStore } from '../../stores/queryStore'

export interface NaturalSearchState {
  prompt: string
  matches: NaturalSearchMatch[]
  clarification: string
  explanation: string
  error: string
  loading: boolean
  exploring: number | null
  context?: NaturalSearchContext
}

const examples = [
  'Places like Tuscany, but warmer and outside Europe',
  'Places like Atacama Desert, only in South America',
  'Find warmer places like Alentejo, outside Europe',
]

export function NaturalSearch({
  state,
  setState,
  onFlyTo,
  onBack,
}: {
  state: NaturalSearchState
  setState: React.Dispatch<React.SetStateAction<NaturalSearchState>>
  onFlyTo?: (lat: number, lng: number) => void
  onBack: () => void
}) {
  const update = (patch: Partial<NaturalSearchState>) => setState((current) => ({ ...current, ...patch }))

  const resetSearch = () => update({
    prompt: '', matches: [], clarification: '', explanation: '', context: undefined, error: '',
  })

  const search = async (text = state.prompt) => {
    if (!text.trim() || state.loading) return
    update({ prompt: text, loading: true, error: '', matches: [], clarification: '', explanation: '' })
    try {
      const result = await searchPlaces(text, state.context)
      update({
        matches: result.matches || [],
        clarification: result.clarification || result.unsupported?.join(' ') || '',
        explanation: result.explanation || '',
        context: result.context,
      })
    } catch (e) {
      update({ error: e instanceof Error ? e.message : 'Search failed. Try again.' })
    } finally {
      update({ loading: false })
    }
  }

  const explore = async (match: NaturalSearchMatch, index: number) => {
    update({ exploring: index, error: '' })
    try {
      const response = await postQuery([{ lat: match.lat, lng: match.lng, label: match.name }])
      const deadline = Date.now() + 120_000
      while (Date.now() < deadline) {
        const status = await getQueryStatus(response.id)
        if (status.status === 'completed') {
          useQueryStore.getState().setQuery(response.id, response.tile_url)
          useQueryStore.setState({ pins: [{ lat: match.lat, lng: match.lng, label: match.name }], hdState: 'off', hdTileUrl: null })
          onFlyTo?.(match.lat, match.lng)
          onBack()
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

  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <p className="text-[10px] font-bold uppercase tracking-[0.18em] text-gold">Search Earth</p>
          <h2 className="text-sm font-semibold text-fg mt-1">Describe a place to find</h2>
        </div>
        <div className="flex items-center gap-3">
          {state.context && <button onClick={resetSearch} className="text-[10px] font-bold uppercase tracking-wider text-fg-50 hover:text-gold">New search</button>}
          <button onClick={onBack} className="text-[10px] font-bold uppercase tracking-wider text-fg-50 hover:text-gold">Gallery</button>
        </div>
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
          <span className="text-[10px] text-fg-40">5–10 matches · 2 km similarity</span>
          <button disabled={state.loading || !state.prompt.trim()} className="bg-gold px-3 py-2 text-[10px] font-bold uppercase tracking-wider text-dark-900 disabled:opacity-50">
            {state.loading ? 'Searching…' : 'Find places'}
          </button>
        </div>
      </form>
      <p className="text-[11px] text-fg-40">Searching and viewing results leave your current map unchanged. Explore starts a separate 2 km similarity map, without 10 m refinement.</p>
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
          <button onClick={resetSearch} className="text-[10px] text-fg-40 hover:text-fg">Clear</button>
        </div>
        {state.explanation && <p className="text-xs text-fg-50">{state.explanation}</p>}
        <div className="space-y-2">
          {state.matches.map((match, i) => <article key={`${match.lat},${match.lng}`} className="border border-fg-08 bg-fg-03 p-3">
            <div className="flex justify-between gap-2"><h3 className="text-xs font-semibold text-fg">{match.name}</h3><span className="shrink-0 text-xs text-gold">{Math.round(match.score * 100)}%</span></div>
            {match.temperature_difference_c != null && <p className="mt-1 text-[11px] text-fg-50">{match.temperature_difference_c > 0 ? '+' : ''}{match.temperature_difference_c.toFixed(1)}°C vs reference</p>}
            <div className="mt-3 flex gap-2">
              <button onClick={() => onFlyTo?.(match.lat, match.lng)} className="border border-fg-15 px-2 py-1.5 text-[10px] text-fg-70 hover:text-gold">View on map</button>
              <button disabled={state.exploring !== null} onClick={() => void explore(match, i)} className="border border-gold/40 bg-gold/10 px-2 py-1.5 text-[10px] text-gold disabled:opacity-50">{state.exploring === i ? 'Scanning…' : 'Explore similar places'}</button>
            </div>
          </article>)}
        </div>
      </>}
    </section>
  )
}
