import { useEffect, useRef } from 'react'
import maplibregl from 'maplibre-gl'
import { useSearchStore } from '../../stores/searchStore'
import { useIsMobile } from '../../hooks/useIsMobile'

interface SearchMarkersProps {
  map: maplibregl.Map
}

/**
 * Find places results on the map: the reference place (gold square) and the
 * numbered matches (crimson), kept in sync with the result cards via hover.
 */
export function SearchMarkers({ map }: SearchMarkersProps) {
  const matches = useSearchStore((s) => s.matches)
  const reference = useSearchStore((s) => s.reference)
  const hovered = useSearchStore((s) => s.hovered)
  const selected = useSearchStore((s) => s.selected)
  const explored = useSearchStore((s) => s.explored)
  const isMobile = useIsMobile()
  const markersRef = useRef<{ marker: maplibregl.Marker; el: HTMLDivElement }[]>([])

  // Reference marker
  useEffect(() => {
    if (!reference) return
    const el = document.createElement('div')
    el.className = 'pin-marker search-reference-marker'
    const icon = document.createElement('span')
    icon.className = 'material-symbols-outlined'
    icon.textContent = 'star'
    el.appendChild(icon)
    const tooltip = document.createElement('div')
    tooltip.className = 'pin-marker-tooltip'
    tooltip.textContent = `Reference · ${reference.name}`
    el.appendChild(tooltip)
    const marker = new maplibregl.Marker({ element: el }).setLngLat([reference.lng, reference.lat]).addTo(map)
    return () => { marker.remove() }
  }, [reference, map])

  // Result markers
  useEffect(() => {
    const { update } = useSearchStore.getState()
    markersRef.current = matches.map((match, i) => {
      const el = document.createElement('div')
      el.className = 'search-result-marker'
      el.textContent = String(i + 1)
      const tooltip = document.createElement('div')
      tooltip.className = 'discovery-marker-tooltip'
      tooltip.textContent = `#${i + 1} ${match.name} · ${Math.round(match.score * 100)}%`
      el.appendChild(tooltip)
      el.addEventListener('mouseenter', () => update({ hovered: i }))
      el.addEventListener('mouseleave', () => update({ hovered: null }))
      el.addEventListener('click', (e) => {
        e.stopPropagation()
        update({ selected: i })
      })
      const marker = new maplibregl.Marker({ element: el }).setLngLat([match.lng, match.lat]).addTo(map)
      return { marker, el }
    })
    return () => {
      markersRef.current.forEach(({ marker }) => marker.remove())
      markersRef.current = []
    }
  }, [matches, map])

  // Frame each new result set. The reference is often on another continent,
  // so it isn't included: it stays on the map and the sidebar can fly to it.
  useEffect(() => {
    if (!matches.length) return
    const bounds = new maplibregl.LngLatBounds()
    matches.forEach((m) => bounds.extend([m.lng, m.lat]))
    map.fitBounds(bounds, {
      padding: isMobile ? { top: 90, bottom: 200, left: 40, right: 40 } : 100,
      maxZoom: 7,
      duration: 1500,
    })
    // Only re-frame when the result set changes, not on viewport resizes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [matches, map])

  // Highlight state
  useEffect(() => {
    markersRef.current.forEach(({ el }, i) => {
      el.classList.toggle('selected', i === hovered || i === selected)
      el.classList.toggle('explored', i === explored?.index)
    })
  }, [hovered, selected, explored, matches])

  return null
}
