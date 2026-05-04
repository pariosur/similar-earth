import { useState, useCallback } from 'react'
import { useQueryStore } from '../../stores/queryStore'
import { useDiscoveries } from '../../hooks/useDiscoveries'
import { useIsMobile } from '../../hooks/useIsMobile'

/**
 * On-map legend explaining marker types + similarity gradient + opacity control.
 * Desktop-only (mobile has its own compact legend in the bottom sheet).
 */
export function MapLegend() {
  const pins = useQueryStore((s) => s.pins)
  const tileUrl = useQueryStore((s) => s.tileUrl)
  const isMobile = useIsMobile()
  const { discoveries } = useDiscoveries()
  const [opacity, setOpacity] = useState(70)

  const handleOpacity = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const val = parseInt(e.target.value)
    setOpacity(val)
    // Update MapLibre layer opacity
    const mapEl = document.querySelector('.maplibregl-map') as any
    const map = mapEl?.__maplibregl_map || (window as any).__map
    if (!map) return
    const layers = map.getStyle()?.layers || []
    for (const layer of layers) {
      if (layer.id?.startsWith('similarity-layer') && layer.type === 'raster') {
        try { map.setPaintProperty(layer.id, 'raster-opacity', val / 100) } catch {}
      }
    }
  }, [])

  if (!tileUrl || isMobile) return null

  return (
    <div className="absolute bottom-6 left-6 z-[var(--z-chip)] glass-panel px-3 py-2.5 flex flex-col gap-1.5">
      {/* Marker key */}
      <div className="flex items-center gap-4">
        <div className="flex items-center gap-1.5">
          <span className="inline-block w-3 h-3" style={{ background: 'var(--pin-bg)', border: '1.5px solid var(--pin-border)' }} />
          <span className="text-[10px] font-bold uppercase tracking-wider" style={{ color: 'var(--fg-60)' }}>
            Reference ({pins.length})
          </span>
        </div>
        <div className="flex items-center gap-1.5">
          <span className="inline-block w-2.5 h-2.5 rounded-full" style={{ background: 'var(--crimson)', border: '1.5px solid #ffffff' }} />
          <span className="text-[10px] font-bold uppercase tracking-wider" style={{ color: 'var(--fg-60)' }}>
            Similar ({discoveries.length})
          </span>
        </div>
      </div>

      {/* Similarity gradient */}
      <div className="flex items-center gap-1.5 pt-1 border-t" style={{ borderColor: 'var(--fg-08)' }}>
        <span className="text-[9px] font-bold uppercase tracking-wider" style={{ color: 'var(--fg-30)' }}>Low</span>
        <div className="w-[100px] h-[5px]" style={{ background: 'linear-gradient(to right, var(--accent-primary), var(--crimson))' }} />
        <span className="text-[9px] font-bold uppercase tracking-wider" style={{ color: 'var(--fg-30)' }}>High</span>
        <span className="text-[9px] uppercase tracking-wider ml-1" style={{ color: 'var(--fg-25)' }}>Similarity</span>
      </div>

      {/* Opacity slider */}
      <div className="flex items-center gap-2 pt-1 border-t" style={{ borderColor: 'var(--fg-08)' }}>
        <span className="text-[9px] uppercase tracking-wider font-bold" style={{ color: 'var(--fg-25)' }}>Opacity</span>
        <input
          type="range"
          min="0"
          max="100"
          value={opacity}
          onChange={handleOpacity}
          className="flex-1 h-1 accent-gold cursor-pointer"
          style={{ accentColor: 'var(--accent-primary)' }}
        />
        <span className="text-[9px] font-mono" style={{ color: 'var(--fg-30)' }}>{opacity}%</span>
      </div>

      {/* Data source */}
      <div className="pt-1 border-t" style={{ borderColor: 'var(--fg-08)' }}>
        <span className="text-[8px] tracking-wider" style={{ color: 'var(--fg-20)' }}>
          <a href="https://developers.google.com/earth-engine/datasets/catalog/GOOGLE_SATELLITE_EMBEDDING_V1_ANNUAL" target="_blank" rel="noopener noreferrer" className="hover:underline" style={{ color: 'var(--fg-25)' }}>Google AlphaEarth</a> · 2km / 10m · 2025
        </span>
      </div>
    </div>
  )
}
