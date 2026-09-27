import { create } from 'zustand'
import type { NaturalSearchContext, NaturalSearchMatch, NaturalSearchReference, NaturalSearchTheme } from '../api/client'

// Which experience the map is showing. "search" hides the gallery heatmap and
// shows the Find places results; "gallery" is the Maps / Create experience.
export type MapMode = 'search' | 'gallery'

export interface ExploredHeatmap {
  index: number
  name: string
  queryId: string
  tileUrl: string
}

interface SearchState {
  mapMode: MapMode
  prompt: string
  matches: NaturalSearchMatch[]
  reference: NaturalSearchReference | null
  // Theme searches ("mangroves") have a theme and several reference sites
  theme: NaturalSearchTheme | null
  references: NaturalSearchReference[]
  context?: NaturalSearchContext
  clarification: string
  explanation: string
  error: string
  loading: boolean
  // Result index currently being scanned for a similarity heatmap
  exploring: number | null
  explored: ExploredHeatmap | null
  // Result highlighted by hovering a card or marker
  hovered: number | null
  // Result picked by clicking its marker (sidebar scrolls to it)
  selected: number | null

  setMapMode: (mode: MapMode) => void
  update: (patch: Partial<SearchState>) => void
  reset: () => void
}

// Shared links (?map=, ?s=, ?query=) open a gallery map, so start there.
function initialMapMode(): MapMode {
  if (typeof window === 'undefined') return 'search'
  const params = new URLSearchParams(window.location.search)
  return params.has('map') || params.has('s') || params.has('query') ? 'gallery' : 'search'
}

// Map links (?s=, ?map=, ?query=) open the gallery, so they must not linger in
// the address bar while Find places is shown: a reload would land on the old map.
const MAP_PARAMS = ['s', 'map', 'query']
let stashedMapParams = ''

function syncUrlWithMode(mode: MapMode) {
  const url = new URL(window.location.href)
  if (mode === 'search') {
    const kept = new URLSearchParams()
    for (const param of MAP_PARAMS) {
      const value = url.searchParams.get(param)
      if (value !== null) {
        kept.set(param, value)
        url.searchParams.delete(param)
      }
    }
    if (kept.toString()) stashedMapParams = kept.toString()
  } else if (stashedMapParams) {
    for (const [param, value] of new URLSearchParams(stashedMapParams)) {
      if (!url.searchParams.has(param)) url.searchParams.set(param, value)
    }
    stashedMapParams = ''
  }
  if (url.href !== window.location.href) window.history.replaceState(window.history.state, '', url)
}

const emptyResults = {
  matches: [],
  reference: null,
  theme: null,
  references: [],
  context: undefined,
  clarification: '',
  explanation: '',
  error: '',
  explored: null,
  hovered: null,
  selected: null,
}

export const useSearchStore = create<SearchState>((set) => ({
  mapMode: initialMapMode(),
  prompt: '',
  loading: false,
  exploring: null,
  ...emptyResults,

  setMapMode: (mapMode) => {
    syncUrlWithMode(mapMode)
    set({ mapMode })
  },
  update: (patch) => set(patch),
  reset: () => set({ prompt: '', ...emptyResults }),
}))
