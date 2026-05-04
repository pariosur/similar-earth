import { useEffect, useState } from 'react'
import { useQueryStore } from '../../stores/queryStore'
import { getPoint } from '../../api/client'
import type { PointResponse } from '../../api/types'

export function PointInspection() {
  const activeQueryId = useQueryStore((s) => s.activeQueryId)
  const inspectedPoint = useQueryStore((s) => s.inspectedPoint)
  const setInspectedPoint = useQueryStore((s) => s.setInspectedPoint)
  const [data, setData] = useState<PointResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(false)

  useEffect(() => {
    if (!activeQueryId || !inspectedPoint) {
      setData(null)
      return
    }

    let cancelled = false
    setLoading(true)
    setError(false)

    getPoint(activeQueryId, inspectedPoint.lat, inspectedPoint.lng)
      .then((res) => {
        if (!cancelled) setData(res)
      })
      .catch(() => {
        if (!cancelled) { setData(null); setError(true) }
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => { cancelled = true }
  }, [activeQueryId, inspectedPoint])

  if (!inspectedPoint) return null

  return (
    <div className="border-t border-fg-08 px-8 py-5">
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-[11px] font-bold uppercase tracking-[0.2em] text-fg-40 flex items-center gap-2">
          <span className="material-symbols-outlined text-sm text-gold">pin_drop</span>
          Point Details
        </h3>
        <button
          onClick={() => setInspectedPoint(null)}
          className="text-fg-25 hover:text-fg-50 transition-colors"
        >
          <span className="material-symbols-outlined text-lg">close</span>
        </button>
      </div>

      <div className="text-[11px] text-fg-30 font-mono mb-4">
        {inspectedPoint.lat.toFixed(4)}, {inspectedPoint.lng.toFixed(4)}
      </div>

      {loading && (
        <div className="text-[11px] text-fg-25 animate-pulse-slow uppercase tracking-widest">Loading...</div>
      )}

      {error && !loading && (
        <p className="text-xs text-crimson flex items-center gap-1.5">
          <span className="material-symbols-outlined text-sm">error</span>
          Could not load point data
        </p>
      )}

      {data && !loading && (
        <div className="space-y-3">
          {/* Similarity score */}
          <div className="bg-fg-03 border border-fg-05 p-4">
            <div className="text-[11px] text-fg-30 uppercase tracking-widest mb-1 font-bold">Similarity</div>
            <div className="text-2xl font-black text-gold tracking-tight">
              <span title="Satellite similarity score. Compares land patterns, not crop suitability or climate.">{(data.similarity * 100).toFixed(1)}%</span>
            </div>
            {data.best_pin_index !== undefined && (
              <div className="text-[11px] text-fg-30 mt-1">
                Best match: Pin #{data.best_pin_index + 1}
                {data.best_pin_label && ` (${data.best_pin_label})`}
              </div>
            )}
          </div>

          {/* Terrain */}
          {data.terrain && (
            <div className="bg-fg-03 border border-fg-05 p-4">
              <div className="text-[11px] text-fg-30 uppercase tracking-widest mb-3 font-bold">Terrain</div>
              <div className="grid grid-cols-3 gap-3 text-xs">
                <div>
                  <div className="text-[11px] text-fg-25 uppercase tracking-wider">Elevation</div>
                  <div className="text-fg-80 font-semibold font-mono mt-0.5">{data.terrain.elevation_m.toFixed(0)}m</div>
                </div>
                <div>
                  <div className="text-[11px] text-fg-25 uppercase tracking-wider">Slope</div>
                  <div className="text-fg-80 font-semibold font-mono mt-0.5">{data.terrain.slope_deg.toFixed(1)}&deg;</div>
                </div>
                <div>
                  <div className="text-[11px] text-fg-25 uppercase tracking-wider">Aspect</div>
                  <div className="text-fg-80 font-semibold font-mono mt-0.5">{data.terrain.aspect_deg.toFixed(0)}&deg;</div>
                </div>
              </div>
            </div>
          )}

          {/* Biophysical */}
          {data.biophysical && (
            <div className="bg-fg-03 border border-fg-05 p-4">
              <div className="text-[11px] text-fg-30 uppercase tracking-widest mb-3 font-bold">Climate</div>
              <div className="grid grid-cols-2 gap-3 text-xs">
                <div>
                  <div className="text-[11px] text-fg-25 uppercase tracking-wider">Rainfall</div>
                  <div className="text-fg-80 font-semibold font-mono mt-0.5">{data.biophysical.annual_rainfall_mm.toFixed(0)} mm/yr</div>
                </div>
                <div>
                  <div className="text-[11px] text-fg-25 uppercase tracking-wider">Temperature</div>
                  <div className="text-fg-80 font-semibold font-mono mt-0.5">{data.biophysical.mean_temp_c.toFixed(1)}&deg;C</div>
                </div>
              </div>
            </div>
          )}

          {/* Land cover */}
          {data.landcover && (
            <div className="bg-fg-03 border border-fg-05 p-4">
              <div className="text-[11px] text-fg-30 uppercase tracking-widest mb-1 font-bold">Land Cover</div>
              <div className="text-sm text-fg-80 font-semibold">{data.landcover.class_name}</div>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
