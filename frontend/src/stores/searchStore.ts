import { create } from 'zustand'
import type { NaturalSearchContext, NaturalSearchMatch, NaturalSearchReference } from '../api/client'

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

const emptyResults = {
  matches: [],
  reference: null,
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

  setMapMode: (mapMode) => set({ mapMode }),
  update: (patch) => set(patch),
  reset: () => set({ prompt: '', ...emptyResults }),
}))
