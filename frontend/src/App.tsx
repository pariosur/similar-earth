import { useEffect, useState, useCallback, useRef } from 'react'
import { MapContainer } from './components/map/MapContainer'
import { SidePanel } from './components/sidebar/SidePanel'
import { ExploreDrawer } from './components/map/ExploreDrawer'
import { CreateModePill } from './components/map/CreateModePill'
import { LoadingOverlay } from './components/ui/LoadingOverlay'
import { useIsMobile } from './hooks/useIsMobile'
import { useInitializeMap } from './hooks/useInitializeMap'
import { useQueryStore } from './stores/queryStore'

export function App() {
  const isMobile = useIsMobile()
  const createMode = useQueryStore((s) => s.createMode)
  const activeMapName = useQueryStore((s) => s.activeMapName)

  // Dynamic page title + OG meta
  useEffect(() => {
    const title = activeMapName
      ? `${activeMapName} — Similar Earth`
      : 'Similar Earth — Global Similarity Maps'
    const desc = activeMapName
      ? `Where else on Earth looks like ${activeMapName}? Satellite similarity search.`
      : 'Where else on Earth looks like this? Search the entire planet by satellite similarity.'
    document.title = title
    document.querySelector('meta[property="og:title"]')?.setAttribute('content', title)
    document.querySelector('meta[property="og:description"]')?.setAttribute('content', desc)
    document.querySelector('meta[name="twitter:title"]')?.setAttribute('content', title)
    document.querySelector('meta[name="twitter:description"]')?.setAttribute('content', desc)
  }, [activeMapName])
  const [sidebarOpen, setSidebarOpen] = useState(() => window.innerWidth >= 768)
  const flyToRef = useRef<(lat: number, lng: number) => void>(() => {})

  useInitializeMap()

  // Cmd+B / Ctrl+B to toggle sidebar
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'b') {
        e.preventDefault()
        setSidebarOpen((prev) => !prev)
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [])

  const toggleSidebar = useCallback(() => setSidebarOpen((prev) => !prev), [])

  const handleFlyTo = useCallback((lat: number, lng: number) => {
    flyToRef.current(lat, lng)
  }, [])

  // Mobile create mode: "Done" → open sheet with form
  const handleCreateDone = useCallback(() => {
    setSidebarOpen(true)
  }, [])

  return (
    <div className="relative w-screen h-dvh overflow-hidden flex">
      {/* Sidebar — full height left column */}
      <SidePanel
        open={sidebarOpen}
        onToggle={toggleSidebar}
        onFlyTo={handleFlyTo}
        onCollapse={() => setSidebarOpen(false)}
      />

      {/* Map + bottom drawer — right column */}
      <div className="flex-1 flex flex-col min-w-0">
        <div className="relative flex-1 min-h-0">
          <MapContainer onFlyToReady={(fn) => { flyToRef.current = fn }} />
          {/* Tap map to collapse expanded bottom sheet on mobile — exempt during create mode */}
          {isMobile && sidebarOpen && !createMode && (
            <div
              className="absolute inset-0 z-[var(--z-legend)]"
              onClick={() => setSidebarOpen(false)}
            />
          )}
          <CreateModePill onDone={handleCreateDone} sidebarOpen={sidebarOpen} />
          {!sidebarOpen && !isMobile && (
            <button
              onClick={toggleSidebar}
              className="absolute top-[var(--search-top)] left-[var(--search-top)] z-[var(--z-panel)] w-10 h-10 bg-navy-light/90 backdrop-blur-md border border-fg-10 flex items-center justify-center text-fg-60 hover:text-fg hover:bg-navy-light transition-colors"
              title="Open sidebar (Cmd+B)"
            >
              <span className="material-symbols-outlined text-xl">menu</span>
            </button>
          )}
          <LoadingOverlay />
        </div>
        <ExploreDrawer onFlyTo={handleFlyTo} />
      </div>
    </div>
  )
}
