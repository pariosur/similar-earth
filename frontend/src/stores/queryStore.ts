import { create } from 'zustand'
import type { Pin } from '../api/types'
import type { LayerInfo } from '../api/client'

const MAX_PINS = 50

export type HdState = 'off' | 'zoomed' | 'computing' | 'loaded'

export interface QueryState {
  // Layer catalog (loaded from API)
  layers: LayerInfo[]

  // Selection state
  pins: Pin[]
  activeMapId: string | null
  activeMapSlug: string | null
  activeMapName: string | null
  activeQueryId: string | null
  tileUrl: string | null
  queryStatus: 'idle' | 'computing' | 'ready' | 'failed'
  inspectedPoint: { lat: number; lng: number } | null
  selectedPinIndex: number | null
  selectedDiscoveryIndex: number | null
  addPinMode: boolean
  createMode: boolean

  // HD (10m) state
  hdState: HdState
  hdTileUrl: string | null

  setLayers: (layers: LayerInfo[]) => void
  addPin: (lat: number, lng: number) => void
  removePin: (index: number) => void
  clearPins: () => void
  selectLayer: (layerId: string) => void
  setQuery: (id: string, tileUrl: string) => void
  setQueryStatus: (status: QueryState['queryStatus']) => void
  setInspectedPoint: (point: { lat: number; lng: number } | null) => void
  setSelectedPin: (index: number | null) => void
  setSelectedDiscovery: (index: number | null) => void
  clearSelection: () => void
  setAddPinMode: (mode: boolean) => void
  setCreateMode: (mode: boolean) => void
  setHdState: (state: HdState) => void
  setHdTileUrl: (url: string | null) => void
  reset: () => void
}

export const useQueryStore = create<QueryState>((set, get) => ({
  layers: [],
  pins: [],
  activeMapId: null,
  activeMapSlug: null,
  activeMapName: null,
  activeQueryId: null,
  tileUrl: null,
  queryStatus: 'idle',
  inspectedPoint: null,
  selectedPinIndex: null,
  selectedDiscoveryIndex: null,
  addPinMode: false,
  createMode: false,
  hdState: 'off',
  hdTileUrl: null,

  setLayers: (layers) => set({ layers }),

  addPin: (lat, lng) => {
    set((state) => {
      if (state.pins.length >= MAX_PINS) return state
      // Prevent duplicate pins too close together (~1km)
      const tooClose = state.pins.some((p) =>
        Math.abs(p.lat - lat) < 0.01 && Math.abs(p.lng - lng) < 0.01
      )
      if (tooClose) return state
      return { pins: [...state.pins, { lat, lng }] }
    })
    // Background reverse-geocode to add a friendly label (match by coords, not index)
    fetch(`https://nominatim.openstreetmap.org/reverse?format=json&lat=${lat}&lon=${lng}&zoom=10&accept-language=en`, { headers: { 'User-Agent': 'SimilarEarth/1.0' } })
      .then((r) => r.json())
      .then((data) => {
        const addr = data.address || {}
        const local = addr.city || addr.town || addr.village || addr.municipality || addr.county || ''
        const country = addr.country || ''
        const label = local && country ? `${local}, ${country}` : country || data.display_name?.split(',').slice(0, 2).join(',').trim() || ''
        if (label) {
          set((state) => ({
            pins: state.pins.map((p) => p.lat === lat && p.lng === lng && !p.label ? { ...p, label } : p),
          }))
        }
      })
      .catch(() => {})
  },

  removePin: (index) =>
    set((state) => ({
      pins: state.pins.filter((_, i) => i !== index),
    })),

  clearPins: () =>
    set({
      pins: [],
      activeMapId: null,
      activeMapSlug: null,
      activeMapName: null,
      activeQueryId: null,
      tileUrl: null,
      queryStatus: 'idle',
      inspectedPoint: null,
      selectedPinIndex: null,
      selectedDiscoveryIndex: null,
      hdState: 'off',
      hdTileUrl: null,
    }),

  selectLayer: (layerId) => {
    const layer = get().layers.find((l) => l.id === layerId)
    if (!layer) return
    const pins = (layer.pins || []).map((p) => ({
      lat: p.lat,
      lng: p.lng,
      label: p.label,
    }))
    set({
      pins,
      activeQueryId: null,
      tileUrl: `/tiles/${layerId}/{z}/{x}/{y}.png`,
      queryStatus: 'ready',
      inspectedPoint: null,
      selectedPinIndex: null,
      selectedDiscoveryIndex: null,
      hdState: 'off',
      hdTileUrl: null,
    })
  },

  setQuery: (id, tileUrl) =>
    set({ activeQueryId: id, tileUrl, queryStatus: 'ready', activeMapSlug: null, activeMapId: null, activeMapName: null, selectedPinIndex: null, selectedDiscoveryIndex: null }),

  setQueryStatus: (status) => set({ queryStatus: status }),

  setInspectedPoint: (point) => set({ inspectedPoint: point }),

  setSelectedPin: (index) => set({ selectedPinIndex: index, selectedDiscoveryIndex: null }),
  setSelectedDiscovery: (index) => set({ selectedDiscoveryIndex: index, selectedPinIndex: null }),
  clearSelection: () => set({ selectedPinIndex: null, selectedDiscoveryIndex: null }),

  setAddPinMode: (mode) => set({ addPinMode: mode }),

  setCreateMode: (mode) => {
    if (mode) {
      // Entering create mode: clear everything, enable pin dropping
      set({
        createMode: true,
        addPinMode: true,
        pins: [],
        activeQueryId: null,
        tileUrl: null,
        queryStatus: 'idle',
        hdState: 'off',
        hdTileUrl: null,
      })
    } else {
      set({ createMode: false, addPinMode: false })
    }
  },

  setHdState: (hdState) => set({ hdState }),

  setHdTileUrl: (hdTileUrl) => set({ hdTileUrl }),

  reset: () =>
    set({
      pins: [],
      activeMapId: null,
      activeMapSlug: null,
      activeMapName: null,
      activeQueryId: null,
      tileUrl: null,
      queryStatus: 'idle',
      inspectedPoint: null,
      selectedPinIndex: null,
      selectedDiscoveryIndex: null,
      addPinMode: false,
      createMode: false,
      hdState: 'off',
      hdTileUrl: null,
    }),
}))
