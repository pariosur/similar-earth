import { useEffect, useState, useMemo, useCallback } from 'react'
import { listMaps, starMap, type MapInfo } from '../../api/client'
import { useQueryStore } from '../../stores/queryStore'
import { useSelectMap } from '../../hooks/useSelectMap'

const CATEGORY_ICONS: Record<string, string> = {
  'Agriculture': 'agriculture',
  'Energy': 'bolt',
  'Natural Ecosystems': 'forest',
  'Climate Risk': 'local_fire_department',
  'Other': 'more_horiz',
}

const CATEGORY_ORDER = ['Agriculture', 'Energy', 'Natural Ecosystems', 'Climate Risk', 'Other']

export function MapGallery() {
  const [maps, setMaps] = useState<MapInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [search, setSearch] = useState('')
  const [sort, setSort] = useState<'newest' | 'stars' | 'views'>('stars')
  const [expandedCats, setExpandedCats] = useState<Set<string>>(new Set())
  const activeMapId = useQueryStore((s) => s.activeMapId)

  const pins = useQueryStore((s) => s.pins)

  const toggleCat = useCallback((cat: string) => {
    setExpandedCats((prev) => {
      const next = new Set(prev)
      if (next.has(cat)) next.delete(cat)
      else next.add(cat)
      return next
    })
  }, [])

  function loadMaps() {
    setLoading(true)
    setError(false)
    listMaps(sort, 200)
      .then(setMaps)
      .catch(() => setError(true))
      .finally(() => setLoading(false))
  }

  useEffect(() => { loadMaps() }, [sort])

  // Auto-expand category containing active map
  useEffect(() => {
    if (!activeMapId || maps.length === 0) return
    const activeMap = maps.find((m) => m.id === activeMapId)
    if (activeMap?.category) {
      setExpandedCats((prev) => {
        if (prev.has(activeMap.category)) return prev
        return new Set(prev).add(activeMap.category)
      })
    }
  }, [activeMapId, maps])

  // Split maps into featured (curated) and community (user-created)
  const { featuredCategories, communityMaps } = useMemo(() => {
    const filtered = search
      ? maps.filter((m) => m.title.toLowerCase().includes(search.toLowerCase()) || m.category.toLowerCase().includes(search.toLowerCase()))
      : maps

    const featured = filtered.filter((m) => m.is_featured)
    const community = filtered.filter((m) => !m.is_featured)

    // Group featured maps by category
    const cats: Record<string, MapInfo[]> = {}
    for (const m of featured) {
      const cat = CATEGORY_ORDER.includes(m.category) ? m.category : 'Other'
      if (!cats[cat]) cats[cat] = []
      cats[cat].push(m)
    }

    // Sort maps within each category
    for (const cat of Object.keys(cats)) {
      cats[cat].sort((a, b) => {
        if (sort === 'stars') return b.stars - a.stars || b.views - a.views
        if (sort === 'views') return b.views - a.views || b.stars - a.stars
        return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
      })
    }

    // Sort categories by defined order
    const featuredCategories = Object.entries(cats).sort(([a], [b]) => {
      const ai = CATEGORY_ORDER.indexOf(a)
      const bi = CATEGORY_ORDER.indexOf(b)
      return ai - bi
    })

    // Sort community maps
    community.sort((a, b) => {
      if (sort === 'stars') return b.stars - a.stars || b.views - a.views
      if (sort === 'views') return b.views - a.views || b.stars - a.stars
      return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
    })

    return { featuredCategories, communityMaps: community }
  }, [maps, search, sort])

  if (loading) {
    return (
      <div className="space-y-3 py-4">
        {[1, 2, 3, 4].map((i) => (
          <div key={i} className="flex items-center gap-3 px-1 animate-pulse">
            <div className="w-5 h-5 bg-fg-08 shrink-0" />
            <div className="flex-1 space-y-1.5">
              <div className="h-3 bg-fg-08 rounded w-3/4" />
              <div className="h-2 bg-fg-05 rounded w-1/2" />
            </div>
          </div>
        ))}
      </div>
    )
  }

  // Find active map name
  const activeMap = maps.find((m) => m.id === activeMapId)

  return (
    <div className="space-y-4">
      {/* Intro / context card */}
      <div className="bg-navy-light/30 border-l-2 border-gold px-4 py-4">
        {activeMap ? (
          <>
            <h2 className="text-sm font-bold text-fg">
              Where else on Earth looks like <span className="text-gold">{activeMap.title}</span>?
            </h2>
            <p className="text-xs text-fg-60 mt-1.5 leading-relaxed">
              {pins.length > 0 ? `Showing similarity from ${pins.length} reference pins using satellite data.` : 'Loading reference pins...'}
            </p>
            {activeMap.description && (
              <p className="text-xs text-fg-60 mt-1 leading-relaxed">{activeMap.description}</p>
            )}
            {activeMap.category === 'Agriculture' && (
              <p className="text-[11px] text-fg-60 mt-1 italic">Satellite-similar areas, not guaranteed crop-suitable.</p>
            )}
            {activeMap.category === 'Energy' && (
              <p className="text-[11px] text-fg-60 mt-1 italic">Visually similar to known solar sites, not confirmed suitable project locations.</p>
            )}
            {activeMap.category === 'Climate Risk' && (
              <p className="text-[11px] text-fg-60 mt-1 italic">Areas with similar satellite patterns, not confirmed risk assessments.</p>
            )}
            <button
              onClick={() => {
                navigator.clipboard.writeText(window.location.href)
              }}
              className="mt-2 text-[9px] text-fg-30 hover:text-gold flex items-center gap-1 uppercase tracking-wider font-bold transition-colors"
            >
              <span className="material-symbols-outlined text-[14px]">content_copy</span>
              Copy share link
            </button>
          </>
        ) : (
          <>
            <h2 className="text-sm font-bold text-fg">Where on Earth looks like this?</h2>
            <p className="text-xs text-fg-60 mt-1.5 leading-relaxed">
              Pick a map below to see which places on Earth share similar satellite signatures. Or create your own from any set of locations.
            </p>
          </>
        )}
      </div>

      {/* Search + Sort */}
      <div className="flex gap-2">
        <div className="relative flex-1">
          <span className="material-symbols-outlined text-fg-30 text-sm absolute left-2.5 top-1/2 -translate-y-1/2">search</span>
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search maps..."
            className="w-full text-xs pl-8 pr-3 py-2 bg-fg-05 border border-fg-08 text-fg-80 placeholder-fg-40 outline-none focus:border-gold/40"
          />
        </div>
        <div className="flex bg-fg-05 border border-fg-08 p-0.5">
          {(['stars', 'views', 'newest'] as const).map((s) => (
            <button
              key={s}
              onClick={() => setSort(s)}
              className={`text-[11px] px-2 py-1 font-bold uppercase tracking-wider transition-colors ${
                sort === s ? 'bg-navy-light text-gold' : 'text-fg-50 hover:text-fg-60'
              }`}
              title={s === 'stars' ? 'Sort by stars' : s === 'views' ? 'Sort by views' : 'Sort by newest'}
            >
              {s === 'stars' ? '\u2606' : s === 'views' ? '\u{1F441}' : '\u{1F195}'}
            </button>
          ))}
        </div>
      </div>

      {/* Featured Maps */}
      {featuredCategories.length > 0 && (
        <>
          <div>
            <h2 className="text-[11px] font-bold uppercase tracking-[0.2em] text-fg-30">
              Featured Maps <span className="text-fg-40 font-normal">{featuredCategories.reduce((sum, [, maps]) => sum + maps.length, 0)}</span>
            </h2>
            <p className="text-xs text-fg-60 mt-1">
              Each map highlights regions worldwide that match a set of reference pins. Pick a category to explore.
            </p>
          </div>

          <div className="space-y-1">
            {featuredCategories.map(([category, catMaps]) => {
              const isExpanded = expandedCats.has(category)
              return (
                <div key={category}>
                  <button
                    onClick={() => toggleCat(category)}
                    className="flex items-center gap-3 w-full py-3 hover:bg-fg-03 transition-colors group"
                  >
                    <span className="material-symbols-outlined text-lg text-fg-40 group-hover:text-fg-50 transition-opacity">
                      {CATEGORY_ICONS[category] || 'map'}
                    </span>
                    <span className="text-sm text-fg-80 font-medium flex-1 text-left">
                      {category} <span className="text-xs text-fg-50 font-normal">{catMaps.length}</span>
                    </span>
                    <span
                      className="material-symbols-outlined text-sm text-fg-40"
                      style={{ transform: isExpanded ? 'rotate(90deg)' : 'none', transition: 'transform 0.15s' }}
                    >
                      chevron_right
                    </span>
                  </button>
                  {isExpanded && (
                    <div className="space-y-px ml-1 mb-3 border-l-2 border-gold/20 pl-4">
                      {catMaps.map((m) => (
                        <MapCard key={m.id} map={m} />
                      ))}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        </>
      )}

      {/* Community Maps */}
      {communityMaps.length > 0 && (
        <>
          <div className="border-t border-fg-08 pt-4">
            <h2 className="text-[11px] font-bold uppercase tracking-[0.2em] text-fg-30">Community Maps</h2>
            <p className="text-xs text-fg-60 mt-1">Created by users. Maps with quality pins may be promoted to Featured.</p>
          </div>

          <div className="space-y-px">
            {communityMaps.map((m) => (
              <MapCard key={m.id} map={m} />
            ))}
          </div>
        </>
      )}

      {error && (
        <div className="text-center py-8">
          <span className="material-symbols-outlined text-2xl text-crimson mb-2 block">cloud_off</span>
          <p className="text-xs text-fg-60 mb-3">Failed to load maps</p>
          <button
            onClick={loadMaps}
            className="text-[11px] px-4 py-2 bg-navy-light text-gold font-bold uppercase tracking-widest hover:bg-navy transition-colors"
          >
            Retry
          </button>
        </div>
      )}

      {!error && featuredCategories.length === 0 && communityMaps.length === 0 && (
        <div className="text-center py-8">
          <span className="material-symbols-outlined text-2xl text-fg-10 mb-2 block">explore</span>
          <p className="text-xs text-fg-50 uppercase tracking-widest">
            {search ? `No results for "${search}"` : 'No maps yet'}
          </p>
        </div>
      )}
    </div>
  )
}

function MapCard({ map }: { map: MapInfo }) {
  const [starred, setStarred] = useState(false)
  const activeMapId = useQueryStore((s) => s.activeMapId)
  const isActive = activeMapId === map.id
  const { selectMap, loading } = useSelectMap()
  const handleClick = () => selectMap(map)

  async function handleStar(e: React.MouseEvent) {
    e.stopPropagation()
    if (starred) return
    try {
      await starMap(map.id)
      setStarred(true)
    } catch (err) {
      console.error('Star failed:', err)
    }
  }

  return (
    <button
      onClick={handleClick}
      className={`w-full text-left px-3 py-2.5 transition-all border-l-2 ${
        isActive
          ? 'bg-navy-light/40 border-gold text-fg'
          : 'border-transparent hover:bg-fg-03 text-fg-60 hover:text-fg'
      }`}
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-[13px] font-medium truncate">{map.title}</span>
            {loading && (
              <svg className="animate-spin h-3 w-3 text-gold shrink-0" viewBox="0 0 24 24" fill="none">
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
              </svg>
            )}
          </div>
          {map.description && isActive && (
            <div className="text-xs text-fg-50 mt-0.5 leading-relaxed">{map.description}</div>
          )}
          <div className="text-[11px] text-fg-50 mt-0.5">
            {map.pin_count} pins
            {map.is_featured && <span className="ml-2 text-fg-20">{'\u00b7'} 2025</span>}
            {map.stars > 0 && <span className="ml-2">{map.stars} {'\u2606'}</span>}
          </div>
        </div>
        <button
          onClick={handleStar}
          title={starred ? 'Starred' : 'Star this map'}
          className={`text-sm transition-colors shrink-0 ${starred ? 'text-gold' : 'text-fg-20 hover:text-gold'}`}
        >
          {starred ? '\u{2B50}' : '\u{2606}'}
        </button>
      </div>
      {/* Report button for community maps */}
      {isActive && !map.is_featured && (
        <div className="flex items-center gap-2 mt-1.5 ml-7">
          <a
            href={`mailto:pariosur@gmail.com?subject=Report: ${encodeURIComponent(map.title)}&body=Map ID: ${map.id}%0AReason: `}
            onClick={(e) => e.stopPropagation()}
            className="text-[9px] text-fg-20 hover:text-crimson uppercase tracking-wider font-bold transition-colors"
          >
            Report map
          </a>
        </div>
      )}
    </button>
  )
}
