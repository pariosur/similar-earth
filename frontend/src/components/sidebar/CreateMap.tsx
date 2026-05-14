import { useState, useEffect, useRef } from 'react'
import { useQueryStore } from '../../stores/queryStore'
import { createMap, postQuery, getQueryStatus } from '../../api/client'
import { useIsMobile } from '../../hooks/useIsMobile'

interface CreateMapProps {
  onPublished?: () => void
}

export function CreateMap({ onPublished }: CreateMapProps) {
  const isMobile = useIsMobile()
  const pins = useQueryStore((s) => s.pins)
  const createMode = useQueryStore((s) => s.createMode)
  const setCreateMode = useQueryStore((s) => s.setCreateMode)
  const removePin = useQueryStore((s) => s.removePin)
  const setQuery = useQueryStore((s) => s.setQuery)
  const tileUrl = useQueryStore((s) => s.tileUrl)

  const [showPublish, setShowPublish] = useState(false)
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [category, setCategory] = useState('')
  const [visibility, setVisibility] = useState<'unlisted' | 'public'>('unlisted')
  const [publishing, setPublishing] = useState(false)
  const [published, setPublished] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [searchResults, setSearchResults] = useState<{ display_name: string; lat: string; lon: string }[]>([])
  const [computing, setComputing] = useState(false)
  const [elapsed, setElapsed] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const timerRef = useRef<ReturnType<typeof setInterval>>(undefined)
  const pollRef = useRef<ReturnType<typeof setInterval>>(undefined)

  // Enable create mode when this tab mounts (desktop only — mobile sets it from SidePanel)
  useEffect(() => {
    if (!isMobile && !createMode) setCreateMode(true)
    return () => { if (!isMobile) setCreateMode(false) }
  }, [])

  // Search for places to add as pins
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined)
  function handleSearch(q: string) {
    setSearchQuery(q)
    if (debounceRef.current) clearTimeout(debounceRef.current)
    if (q.length < 3) { setSearchResults([]); return }
    debounceRef.current = setTimeout(async () => {
      try {
        const res = await fetch(
          `https://nominatim.openstreetmap.org/search?format=json&limit=4&q=${encodeURIComponent(q)}`,
          { headers: { 'Accept-Language': 'en' } }
        )
        setSearchResults(await res.json())
      } catch { setSearchResults([]) }
    }, 400)
  }

  function addFromSearch(r: { lat: string; lon: string; display_name: string }) {
    const store = useQueryStore.getState()
    store.addPin(parseFloat(r.lat), parseFloat(r.lon))
    setSearchQuery('')
    setSearchResults([])
  }

  // Build map (compute similarity)
  async function handleBuildMap() {
    if (pins.length < 1 || computing) return
    setComputing(true)
    setElapsed(0)
    setError(null)
    const startTime = Date.now()
    timerRef.current = setInterval(() => setElapsed(Math.floor((Date.now() - startTime) / 1000)), 1000)

    try {
      const res = await postQuery(pins)
      const queryId = res.id
      const url = res.tile_url

      pollRef.current = setInterval(async () => {
        try {
          const status = await getQueryStatus(queryId)
          if (status.status === 'completed') {
            clearInterval(pollRef.current)
            clearInterval(timerRef.current)
            setQuery(queryId, url)
            setComputing(false)
            // Save query ID to URL so it survives reload
            const currentUrl = new URL(window.location.href)
            currentUrl.searchParams.set('query', queryId)
            currentUrl.searchParams.delete('s')
            currentUrl.searchParams.delete('map')
            window.history.replaceState({}, '', currentUrl.toString())
          }
        } catch {}
      }, 1500)
    } catch {
      clearInterval(timerRef.current)
      setComputing(false)
      setError('Computation failed. Try again.')
    }
  }

  // Publish map
  async function handlePublish() {
    if (!title || pins.length === 0) return
    setPublishing(true)
    try {
      const map = await createMap({
        title,
        description,
        category,
        pins: pins.map((p) => ({ lat: p.lat, lng: p.lng, label: p.label })),
      })
      setPublished(map.id)
      useQueryStore.getState().setCreateMode(false)
    } catch {
      setError('Publishing failed. Try again.')
    } finally {
      setPublishing(false)
    }
  }

  // Cleanup timers
  useEffect(() => {
    return () => {
      if (timerRef.current) clearInterval(timerRef.current)
      if (pollRef.current) clearInterval(pollRef.current)
    }
  }, [])

  // Published success state
  if (published) {
    const shareUrl = `${window.location.origin}?map=${published}`
    return (
      <div className="text-center py-8">
        <span className="material-symbols-outlined text-4xl text-green-500 mb-3 block">check_circle</span>
        <p className="text-sm text-fg font-semibold mb-1">Map Published</p>
        <p className="text-xs text-fg-40 mb-2">Your map is live in Community Maps.</p>
        <p className="text-xs text-fg-30 mb-5 leading-relaxed">Share it with others. Maps with great reference pins and community stars can be promoted to Featured.</p>
        <div className="bg-fg-05 border border-fg-08 p-3 mb-4">
          <p className="text-[11px] text-fg-30 uppercase tracking-widest mb-1">Share Link</p>
          <p className="text-xs text-gold break-all font-mono">{shareUrl}</p>
        </div>
        <div className="flex gap-2 justify-center">
          <button
            onClick={() => { navigator.clipboard.writeText(shareUrl); setCopied(true); setTimeout(() => setCopied(false), 2000) }}
            className="text-[11px] px-4 py-2 bg-navy-light text-gold font-bold uppercase tracking-widest hover:bg-navy transition-colors"
          >
            {copied ? 'Copied!' : 'Copy Link'}
          </button>
          <button
            onClick={() => onPublished?.()}
            className="text-[11px] px-4 py-2 border border-fg-10 text-fg-60 font-bold uppercase tracking-widest hover:bg-fg-05 transition-colors"
          >
            Browse Maps
          </button>
        </div>
      </div>
    )
  }

  // Publish form overlay
  if (showPublish) {
    return (
      <div className="space-y-5">
        <button
          onClick={() => setShowPublish(false)}
          className="text-[11px] text-fg-40 hover:text-fg-60 flex items-center gap-1 uppercase tracking-widest font-bold"
        >
          <span className="material-symbols-outlined text-sm">arrow_back</span>
          Back
        </button>

        <h3 className="text-[11px] font-bold uppercase tracking-[0.2em] text-fg-40">Publish Your Map</h3>

        <div>
          <label className="text-[11px] text-fg-30 uppercase tracking-widest block mb-1.5 font-bold">Title *</label>
          <input
            type="text"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="e.g. Highland Coffee Regions"
            className="w-full text-sm px-3 py-2.5 bg-fg-05 border border-fg-08 text-fg-80 placeholder-fg-20 outline-none focus:border-gold/40"
          />
        </div>

        <div>
          <label className="text-[11px] text-fg-30 uppercase tracking-widest block mb-1.5 font-bold">Description</label>
          <textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="What do these locations have in common?"
            rows={2}
            className="w-full text-sm px-3 py-2.5 bg-fg-05 border border-fg-08 text-fg-80 placeholder-fg-20 outline-none focus:border-gold/40 resize-none"
          />
        </div>

        <div>
          <label className="text-[11px] text-fg-30 uppercase tracking-widest block mb-1.5 font-bold">Category</label>
          <select
            value={category}
            onChange={(e) => setCategory(e.target.value)}
            className="w-full text-sm px-3 py-2.5 bg-fg-05 border border-fg-08 text-fg-80 outline-none focus:border-gold/40"
          >
            <option value="">Select...</option>
            <option value="Agriculture">Agriculture</option>
            <option value="Energy">Energy</option>
            <option value="Natural Ecosystems">Natural Ecosystems</option>
            <option value="Climate Risk">Climate Risk</option>
            <option value="Other">Other</option>
          </select>
        </div>

        <div>
          <label className="text-[11px] text-fg-30 uppercase tracking-widest block mb-1.5 font-bold">Visibility</label>
          <div className="space-y-2">
            <label className="flex items-start gap-2 cursor-pointer">
              <input type="radio" name="visibility" checked={visibility === 'unlisted'} onChange={() => setVisibility('unlisted')} className="mt-1" />
              <div>
                <span className="text-xs text-fg-60 font-medium">Unlisted</span>
                <p className="text-[11px] text-fg-25">Anyone with the link can view</p>
              </div>
            </label>
            <label className="flex items-start gap-2 cursor-pointer">
              <input type="radio" name="visibility" checked={visibility === 'public'} onChange={() => setVisibility('public')} className="mt-1" />
              <div>
                <span className="text-xs text-fg-60 font-medium">Public</span>
                <p className="text-[11px] text-fg-25">Appears in Community Maps</p>
              </div>
            </label>
          </div>
        </div>

        {visibility === 'public' && (
          <p className="text-[11px] text-fg-25 bg-fg-03 border border-fg-05 p-2.5 leading-relaxed">
            Your title, description, category, and reference pin locations will be visible to others.
          </p>
        )}

        <p className="text-[11px] text-fg-25">{pins.length} pins will be included</p>

        {title.length > 0 && title.length < 4 && (
          <p className="text-[11px] text-fg-25">Title must be at least 4 characters</p>
        )}
        {visibility === 'public' && !category && (
          <p className="text-[11px] text-fg-25">Category required for public maps</p>
        )}
        {pins.length < 2 && (
          <p className="text-[11px] text-fg-25">At least 2 pins required</p>
        )}

        <button
          onClick={handlePublish}
          disabled={!title || title.length < 4 || publishing || pins.length < 2 || (visibility === 'public' && !category)}
          className={`w-full py-3 text-[11px] font-bold uppercase tracking-[0.2em] transition-all ${
            !title || title.length < 4 || publishing || pins.length < 2 || (visibility === 'public' && !category)
              ? 'bg-fg-05 text-fg-20 cursor-not-allowed'
              : 'bg-navy-light text-gold hover:bg-navy'
          }`}
        >
          {publishing ? 'Publishing...' : 'Publish to Community'}
        </button>
      </div>
    )
  }

  // Mobile compact form (Phase 2 — pins already placed via CreateModePill)
  if (isMobile) {
    return (
      <div className="space-y-4">
        <div className="bg-fg-03 border border-fg-05 p-3">
          <p className="text-xs text-fg-50">
            {pins.length} pin{pins.length !== 1 ? 's' : ''} on the map.{' '}
            {!tileUrl && pins.length >= 1 && 'Ready to build.'}
            {tileUrl && 'Map built! Add a title and publish.'}
          </p>
        </div>

        {/* Title + Category (always visible on mobile) */}
        <div>
          <input
            type="text"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="Map title (required)"
            className="w-full text-sm px-3 py-2.5 bg-fg-05 border border-fg-08 text-fg-80 placeholder-fg-20 outline-none focus:border-gold/40"
          />
        </div>
        <div>
          <select
            value={category}
            onChange={(e) => setCategory(e.target.value)}
            className="w-full text-sm px-3 py-2.5 bg-fg-05 border border-fg-08 text-fg-80 outline-none focus:border-gold/40"
          >
            <option value="">Category...</option>
            <option value="Agriculture">Agriculture</option>
            <option value="Energy">Energy</option>
            <option value="Natural Ecosystems">Natural Ecosystems</option>
            <option value="Climate Risk">Climate Risk</option>
            <option value="Other">Other</option>
          </select>
        </div>

        {/* Build or Publish */}
        {!tileUrl ? (
          <button
            onClick={handleBuildMap}
            disabled={computing || pins.length < 1}
            className={`w-full py-3.5 text-[11px] font-bold uppercase tracking-[0.2em] transition-all flex items-center justify-center gap-2 ${
              computing || pins.length < 1
                ? 'bg-fg-05 text-fg-20 cursor-not-allowed'
                : 'bg-navy-light text-gold hover:bg-navy'
            }`}
          >
            {computing ? (
              <>
                <svg className="animate-spin h-3.5 w-3.5" viewBox="0 0 24 24" fill="none">
                  <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                  <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                </svg>
                Scanning... {elapsed}s
              </>
            ) : (
              <>
                <span className="material-symbols-outlined text-sm">map</span>
                Preview Similarity
              </>
            )}
          </button>
        ) : (
          <button
            onClick={handlePublish}
            disabled={!title || publishing}
            className={`w-full py-3.5 text-[11px] font-bold uppercase tracking-[0.2em] transition-all flex items-center justify-center gap-2 ${
              !title || publishing
                ? 'bg-fg-05 text-fg-20 cursor-not-allowed'
                : 'bg-navy-light text-gold hover:bg-navy'
            }`}
          >
            {publishing ? 'Publishing...' : 'Publish to Community'}
          </button>
        )}

        {error && (
          <p className="text-xs text-crimson flex items-center gap-1.5">
            <span className="material-symbols-outlined text-sm">error</span>
            {error}
          </p>
        )}

        <button
          onClick={() => {
            if (pins.length > 0 && !window.confirm('Clear all pins and start over?')) return
            useQueryStore.getState().clearPins()
            setCreateMode(true)
          }}
          className="w-full text-[11px] py-2 text-fg-25 hover:text-red-400 hover:bg-red-400/5 uppercase tracking-widest font-bold transition-colors"
        >
          Start Over
        </button>
      </div>
    )
  }

  // Desktop create flow (unchanged)
  return (
    <div className="space-y-4">
      {/* Instructions */}
      <div className="bg-fg-03 border border-fg-05 p-4">
        <p className="text-xs text-fg-50 leading-relaxed">
          {pins.length === 0
            ? 'Click anywhere on the map to drop pins, or search for a location below.'
            : `${pins.length} pin${pins.length > 1 ? 's' : ''} placed. ${pins.length < 3 ? 'Add more for better results.' : 'Click "Preview Similarity" to see similar places.'}`
          }
        </p>
      </div>

      {/* Search to add pins */}
      <div className="relative">
        <span className="material-symbols-outlined text-fg-25 text-sm absolute left-3 top-1/2 -translate-y-1/2">
          search
        </span>
        <input
          type="text"
          value={searchQuery}
          onChange={(e) => handleSearch(e.target.value)}
          placeholder="Search a place to add as pin..."
          className="w-full text-xs pl-9 pr-3 py-2.5 bg-fg-05 border border-fg-08 text-fg-60 placeholder-fg-20 outline-none focus:border-gold/40"
        />
        {searchResults.length > 0 && (
          <div className="absolute top-full left-0 right-0 mt-px bg-dark-700 border border-fg-08 overflow-hidden z-10">
            {searchResults.map((r, i) => (
              <button
                key={i}
                onClick={() => addFromSearch(r)}
                className="w-full text-left text-xs px-3 py-2.5 text-fg-60 hover:bg-fg-05 transition-colors border-b border-fg-05 last:border-0"
              >
                {r.display_name.length > 55 ? r.display_name.slice(0, 55) + '...' : r.display_name}
              </button>
            ))}
          </div>
        )}
      </div>

      {/* Pin list */}
      {pins.length > 0 && (
        <div className="space-y-px max-h-[250px] overflow-y-auto">
          {pins.map((pin, i) => (
            <div
              key={`${pin.lat}-${pin.lng}-${i}`}
              className="flex items-center gap-3 px-3 py-2.5 hover:bg-fg-03 group transition-colors"
            >
              <span className="w-5 h-5 bg-gold text-navy text-[11px] font-bold flex items-center justify-center shrink-0">
                {i + 1}
              </span>
              <div className="flex-1 min-w-0">
                {pin.label ? (
                  <>
                    <div className="text-xs text-fg-60 truncate">{pin.label}</div>
                    <div className="text-[10px] text-fg-25 font-mono">{pin.lat.toFixed(4)}, {pin.lng.toFixed(4)}</div>
                  </>
                ) : (
                  <div className="text-xs text-fg-40 font-mono">
                    {pin.lat.toFixed(4)}, {pin.lng.toFixed(4)}
                  </div>
                )}
              </div>
              <button
                onClick={() => removePin(i)}
                className="opacity-0 group-hover:opacity-100 text-fg-25 hover:text-red-400 transition-all"
              >
                <span className="material-symbols-outlined text-sm">close</span>
              </button>
            </div>
          ))}
        </div>
      )}

      {/* Action buttons */}
      {pins.length > 0 && (
        <div className="space-y-2 pt-1">
          {/* Preview Similarity */}
          {!tileUrl && (
            <button
              onClick={handleBuildMap}
              disabled={computing || pins.length < 1}
              className={`w-full py-3.5 text-[11px] font-bold uppercase tracking-[0.2em] transition-all flex items-center justify-center gap-2 ${
                computing || pins.length < 1
                  ? 'bg-fg-05 text-fg-20 cursor-not-allowed'
                  : 'bg-navy-light text-gold hover:bg-navy'
              }`}
            >
              {computing ? (
                <>
                  <svg className="animate-spin h-3.5 w-3.5" viewBox="0 0 24 24" fill="none">
                    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                    <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                  </svg>
                  Building... {elapsed}s
                </>
              ) : (
                <>
                  <span className="material-symbols-outlined text-sm">map</span>
                  Preview Similarity
                </>
              )}
            </button>
          )}

          {/* Publish (only after map is built) */}
          {tileUrl && (
            <button
              onClick={() => setShowPublish(true)}
              className="w-full py-3.5 bg-navy-light text-gold text-[11px] font-bold uppercase tracking-[0.2em] hover:bg-navy flex items-center justify-center gap-2 transition-colors"
            >
              <span className="material-symbols-outlined text-sm">publish</span>
              Publish to Community
            </button>
          )}

          {/* Error message */}
          {error && (
            <p className="text-xs text-crimson flex items-center gap-1.5">
              <span className="material-symbols-outlined text-sm">error</span>
              {error}
            </p>
          )}

          {/* Clear */}
          <button
            onClick={() => {
              if (pins.length > 0 && !window.confirm('Clear all pins and start over?')) return
              const store = useQueryStore.getState()
              store.clearPins()
              setCreateMode(true)
            }}
            className="w-full text-[11px] py-2 text-fg-25 hover:text-red-400 hover:bg-red-400/5 uppercase tracking-widest font-bold transition-colors"
          >
            Start Over
          </button>
        </div>
      )}
    </div>
  )
}
