import { create } from 'zustand'

export type ThemePreference = 'light' | 'dark' | 'system'
export type ResolvedTheme = 'light' | 'dark'
export type Basemap = 'map' | 'satellite'

interface ThemeState {
  preference: ThemePreference
  resolved: ResolvedTheme
  basemap: Basemap
  setTheme: (pref: ThemePreference) => void
  setBasemap: (basemap: Basemap) => void
}

function getSystemTheme(): ResolvedTheme {
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

function resolve(pref: ThemePreference): ResolvedTheme {
  return pref === 'system' ? getSystemTheme() : pref
}

function apply(resolved: ResolvedTheme) {
  const root = document.documentElement
  if (resolved === 'dark') {
    root.classList.add('dark')
  } else {
    root.classList.remove('dark')
  }
  const meta = document.querySelector('meta[name="theme-color"]')
  if (meta) {
    meta.setAttribute('content', resolved === 'dark' ? '#002542' : '#e2eaed')
  }
}

const stored = (localStorage.getItem('theme') as ThemePreference) || 'dark'
const storedBasemap = (localStorage.getItem('basemap') as Basemap) || 'map'

export const useThemeStore = create<ThemeState>((set) => {
  const initial = resolve(stored)
  apply(initial)

  // Listen for system theme changes
  const mq = window.matchMedia('(prefers-color-scheme: dark)')
  mq.addEventListener('change', () => {
    const state = useThemeStore.getState()
    if (state.preference === 'system') {
      const resolved = getSystemTheme()
      apply(resolved)
      set({ resolved })
    }
  })

  return {
    preference: stored,
    resolved: initial,
    basemap: storedBasemap,
    setTheme: (pref) => {
      localStorage.setItem('theme', pref)
      const resolved = resolve(pref)
      apply(resolved)
      set({ preference: pref, resolved })
    },
    setBasemap: (basemap) => {
      localStorage.setItem('basemap', basemap)
      set({ basemap })
    },
  }
})
