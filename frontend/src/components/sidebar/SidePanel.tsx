import { useState } from 'react'
import { MapGallery } from './MapGallery'
import { CreateMap } from './CreateMap'
import { PointInspection } from './PointInspection'
import { MapChipStrip } from '../map/MapChipStrip'
import { DiscoveryChipStrip } from '../map/DiscoveryChipStrip'
import { PinChipStrip } from '../map/PinChipStrip'
import { useDiscoveries } from '../../hooks/useDiscoveries'
import { useQueryStore } from '../../stores/queryStore'
import { useThemeStore } from '../../stores/themeStore'
import { useIsMobile } from '../../hooks/useIsMobile'

type Tab = 'browse' | 'create' | 'about'

interface SidePanelProps {
  open: boolean
  onToggle: () => void
  onFlyTo?: (lat: number, lng: number) => void
  onCollapse?: () => void
}

export function SidePanel({ open, onToggle, onFlyTo, onCollapse }: SidePanelProps) {
  const [tab, setTab] = useState<Tab>('browse')
  const themePreference = useThemeStore((s) => s.preference)
  const setTheme = useThemeStore((s) => s.setTheme)
  const isMobile = useIsMobile()

  const cycleTheme = () => {
    setTheme(themePreference === 'dark' ? 'light' : 'dark')
  }
  const themeIcon = themePreference === 'dark' ? 'dark_mode' : 'light_mode'
  const themeTitle = `Theme: ${themePreference} (click to toggle)`

  // Desktop: hide completely when closed
  // Mobile: always render (collapsed bar when closed)
  if (!open && !isMobile) return null

  return (
    <div
      className={`
        always-dark z-[var(--z-panel)] bg-dark-800/95 backdrop-blur-md flex flex-col overflow-hidden
        transition-all duration-200 ease-out
        ${isMobile
          ? `fixed inset-x-0 bottom-0 border-t border-fg-08 ${open ? 'max-h-[var(--mobile-sheet-expanded)]' : 'max-h-[var(--mobile-sheet-collapsed)]'}`
          : 'w-[380px] shrink-0 border-r border-fg-08 h-full'
        }
      `}
    >
      {/* Collapsed mobile: chip strip + compact legend */}
      {isMobile && !open && (
        <MobileCollapsedHeader onExpand={onToggle} onFlyTo={onFlyTo} />
      )}

      {/* Expanded/desktop header */}
      {(!isMobile || open) && (
      <div className={`${isMobile ? 'px-4 pt-3 pb-2' : 'px-8 pt-6 pb-4'} border-b border-fg-08 shrink-0`}>
        {/* Row 1: Title + controls */}
        <div className="flex items-start justify-between">
          <div>
            <h1 className={`${isMobile ? 'text-base' : 'text-xl'} font-black uppercase tracking-[0.15em] text-fg`}>
              Similar Earth
            </h1>
            <p className="text-[11px] uppercase tracking-[0.2em] text-fg-60 mt-0.5">
              Global similarity maps
            </p>
          </div>
          <div className="flex items-center gap-1 shrink-0 mt-1">
            <button
              onClick={cycleTheme}
              className="w-7 h-7 flex items-center justify-center text-fg-30 hover:text-fg-60 transition-colors"
              title={themeTitle}
            >
              <span className="material-symbols-outlined text-base">{themeIcon}</span>
            </button>
            <button
              onClick={() => setTab(tab === 'about' ? 'browse' : 'about')}
              className={`px-2 h-7 flex items-center transition-colors text-[10px] font-bold uppercase tracking-wider ${
                tab === 'about' ? 'text-gold' : 'text-fg-30 hover:text-fg-60'
              }`}
            >
              How it works
            </button>
            <button
              onClick={onToggle}
              className="w-7 h-7 flex items-center justify-center text-fg-30 hover:text-fg-60 transition-colors"
              title={isMobile ? (open ? 'Collapse' : 'Expand') : 'Close sidebar (Cmd+B)'}
            >
              <span className="material-symbols-outlined text-base">
                {isMobile ? 'expand_more' : 'chevron_left'}
              </span>
            </button>
          </div>
        </div>
        {/* Row 2: Description — full width */}
        <p className="text-sm text-fg-80 mt-3 leading-relaxed">
          Comparing every 10m of Earth using satellite data to find places that look like the ones you pin.
        </p>

        {/* Tabs — only show when open */}
        {open && tab !== 'about' && (
          <nav className={`flex ${isMobile ? 'mt-3' : 'mt-5'} border-b border-fg-08 ${isMobile ? '-mx-4 px-4' : '-mx-8 px-8'}`}>
            <button
              onClick={() => {
                setTab('browse')
                if (isMobile) useQueryStore.getState().setCreateMode(false)
              }}
              className={`flex items-center gap-2 py-3 text-[11px] font-bold uppercase tracking-[0.15em] transition-all border-b-2 mr-6 ${
                tab === 'browse'
                  ? 'text-gold border-gold'
                  : 'text-fg-40 border-transparent hover:text-fg-60'
              }`}
            >
              <span className="material-symbols-outlined text-sm">explore</span>
              Maps
            </button>
            <button
              onClick={() => {
                setTab('create')
                // On mobile: enter create mode and collapse sheet for pin placement
                if (isMobile) {
                  useQueryStore.getState().setCreateMode(true)
                  onCollapse?.()
                }
              }}
              className={`flex items-center gap-2 py-3 text-[11px] font-bold uppercase tracking-[0.15em] transition-all border-b-2 ${
                tab === 'create'
                  ? 'text-gold border-gold'
                  : 'text-fg-40 border-transparent hover:text-fg-60'
              }`}
            >
              <span className="material-symbols-outlined text-sm">add_box</span>
              Create
            </button>
          </nav>
        )}
      </div>
      )}

      {/* Tab content — only show when open */}
      {open && (
        <>
          <div className={`flex-1 overflow-y-auto ${isMobile ? 'px-4 py-4' : 'px-8 py-6'}`}>
            {tab === 'about' ? <HowItWorks onClose={() => setTab('browse')} onCreateClick={() => { setTab('create'); if (isMobile) { useQueryStore.getState().setCreateMode(true); onCollapse?.() } }} /> :
             tab === 'browse' ? <MapGallery /> :
             <CreateMap onPublished={() => setTab('browse')} />}
          </div>

          {/* Point inspection */}
          <PointInspection />

          {/* Attribution */}
          <div className={`${isMobile ? 'px-4' : 'px-8'} py-3 border-t border-fg-08 shrink-0`}>
            <p className="text-[10px] text-fg-30 leading-relaxed flex items-center gap-1">
              Built by <a href="https://github.com/pariosur" target="_blank" rel="noopener noreferrer" className="text-fg-30 hover:text-gold">Pablo Rios</a>
              <span className="text-fg-20">·</span>
              <a href="https://github.com/pariosur/similar-earth" target="_blank" rel="noopener noreferrer" className="text-fg-30 hover:text-gold flex items-center gap-0.5">
                <svg viewBox="0 0 16 16" width="12" height="12" fill="currentColor"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"/></svg>
                Open Source
              </a>
            </p>
          </div>
        </>
      )}
    </div>
  )
}

function HowItWorks({ onClose, onCreateClick }: { onClose: () => void; onCreateClick?: () => void }) {
  const activeMapName = useQueryStore((s) => s.activeMapName)
  const pins = useQueryStore((s) => s.pins)
  const createMode = useQueryStore((s) => s.createMode)

  return (
    <div className="space-y-6">
      <h2 className="text-[11px] font-bold uppercase tracking-[0.2em] text-fg-40">How It Works</h2>

      <p className="text-xs text-fg-50 leading-relaxed">
        {activeMapName
          ? `This map compares every land area on Earth against ${pins.length} ${activeMapName} reference pins using satellite data.`
          : createMode
            ? `This map will compare Earth against your ${pins.length} selected reference pins.`
            : 'Each map shows places on Earth that look similar to a set of reference pins, based on satellite data.'}
      </p>

      <div className="flex gap-4">
        <div className="w-6 h-6 bg-navy-light text-gold flex items-center justify-center shrink-0 text-[11px] font-bold">1</div>
        <div>
          <h3 className="text-sm font-semibold text-fg">Pin places you know</h3>
          <p className="text-xs text-fg-40 mt-1 leading-relaxed">Farms, beaches, forests, solar panels, anything.</p>
        </div>
      </div>

      <div className="flex gap-4">
        <div className="w-6 h-6 bg-navy-light text-gold flex items-center justify-center shrink-0 text-[11px] font-bold">2</div>
        <div>
          <h3 className="text-sm font-semibold text-fg">We scan the planet</h3>
          <p className="text-xs text-fg-40 mt-1 leading-relaxed">Your pins get compared against every land pixel on Earth using satellite embeddings.</p>
        </div>
      </div>

      <div className="flex gap-4">
        <div className="w-6 h-6 bg-navy-light text-gold flex items-center justify-center shrink-0 text-[11px] font-bold">3</div>
        <div>
          <h3 className="text-sm font-semibold text-fg">See what matches</h3>
          <p className="text-xs text-fg-40 mt-1 leading-relaxed">A place lights up if it looks like any of your pins. Gold = moderate, red = strong match. Zoom in for 10m detail.</p>
        </div>
      </div>

      <div className="border-t border-fg-08 pt-5">
        <h3 className="text-[11px] font-bold uppercase tracking-[0.2em] text-fg-40 mb-3">Use Cases</h3>
        <div className="grid grid-cols-2 gap-1.5">
          {[
            { icon: 'eco', label: 'Crop suitability' },
            { icon: 'solar_power', label: 'Solar siting' },
            { icon: 'forest', label: 'Natural ecosystems' },
            { icon: 'local_fire_department', label: 'Climate risk' },
            { icon: 'beach_access', label: 'Landscape matching' },
            { icon: 'add_circle', label: 'You name it' },
          ].map((item) => (
            <div key={item.label} className="flex items-center gap-2 text-xs text-fg-50 bg-fg-03 px-3 py-2">
              <span className="material-symbols-outlined text-sm text-fg-30">{item.icon}</span>
              {item.label}
            </div>
          ))}
        </div>
      </div>

      <div className="border-t border-fg-08 pt-5">
        <h3 className="text-[11px] font-bold uppercase tracking-[0.2em] text-fg-40 mb-3">Create Your Own</h3>
        <p className="text-xs text-fg-30 leading-relaxed">
          Drop pins on any locations and scan the planet for similar places. Publish your map to the community — maps with great reference pins can be promoted to Featured.
        </p>
        {onCreateClick && (
          <button
            onClick={onCreateClick}
            className="mt-3 w-full py-2.5 bg-fg-05 border border-fg-08 text-gold text-[11px] font-bold uppercase tracking-[0.2em] hover:bg-navy-light transition-colors flex items-center justify-center gap-2"
          >
            <span className="material-symbols-outlined text-sm">add_box</span>
            Create a Map
          </button>
        )}
      </div>

      <div className="border-t border-fg-08 pt-5">
        <p className="text-xs text-fg-30 leading-relaxed">
          Built on <a href="https://developers.google.com/earth-engine/datasets/catalog/GOOGLE_SATELLITE_EMBEDDING_V1_ANNUAL" target="_blank" rel="noopener noreferrer" className="text-gold hover:underline">Google AlphaEarth</a> satellite embeddings (2025). 10m resolution. Open source.
        </p>
      </div>

      <button
        onClick={onClose}
        className="w-full py-3 bg-navy-light text-gold text-[11px] font-bold uppercase tracking-[0.2em] hover:bg-navy transition-colors"
      >
        Start Exploring
      </button>
    </div>
  )
}

function MobileCollapsedHeader({ onExpand, onFlyTo }: { onExpand: () => void; onFlyTo?: (lat: number, lng: number) => void }) {
  const activeMapName = useQueryStore((s) => s.activeMapName)
  const pins = useQueryStore((s) => s.pins)
  const { discoveries } = useDiscoveries()
  const [chipMode, setChipMode] = useState<'discoveries' | 'pins'>('pins')

  return (
    <div className="flex flex-col shrink-0" style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}>
      {/* Top row: intro question + expand button */}
      <div className="flex items-center justify-between px-4 pt-3 pb-1 shrink-0 gap-2">
        <span className="text-[12px] font-bold text-fg truncate">
          {activeMapName ? (
            <>Where else on Earth looks like <span className="text-gold">{activeMapName}</span>?</>
          ) : (
            'Where on Earth looks like this?'
          )}
        </span>
        <button
          onClick={onExpand}
          className="w-8 h-8 flex items-center justify-center text-fg-30 hover:text-fg-60 transition-colors shrink-0 -mr-1"
          title="Browse maps"
        >
          <span className="material-symbols-outlined text-lg">expand_less</span>
        </button>
      </div>

      {/* Map chips (always shown) */}
      <MapChipStrip />

      {/* Pins/Discoveries toggle + corresponding chip strip */}
      {activeMapName && (
        <>
          <div className="border-t border-fg-08 flex items-center gap-3 px-4 pt-2 pb-1">
            <button
              onClick={() => setChipMode('pins')}
              className={`text-[9px] font-bold uppercase tracking-wider py-1 transition-colors`}
              style={{
                color: chipMode === 'pins' ? 'var(--accent-primary)' : 'var(--fg-30)',
                borderBottom: chipMode === 'pins' ? '1px solid var(--accent-primary)' : '1px solid transparent',
              }}
            >
              {useQueryStore.getState().createMode ? 'Your Pins' : 'Reference Pins'} ({pins.length})
            </button>
            <button
              onClick={() => setChipMode('discoveries')}
              className={`text-[9px] font-bold uppercase tracking-wider py-1 transition-colors`}
              style={{
                color: chipMode === 'discoveries' ? 'var(--crimson)' : 'var(--fg-30)',
                borderBottom: chipMode === 'discoveries' ? '1px solid var(--crimson)' : '1px solid transparent',
              }}
            >
              Most Similar ({discoveries.length})
            </button>
          </div>
          {chipMode === 'discoveries'
            ? <DiscoveryChipStrip onFlyTo={onFlyTo} limit={10} />
            : <PinChipStrip onFlyTo={onFlyTo} />
          }
        </>
      )}
    </div>
  )
}
