import { useEffect, useRef } from 'react'
import maplibregl from 'maplibre-gl'
import { useDiscoveries } from '../../hooks/useDiscoveries'
import { useQueryStore } from '../../stores/queryStore'

interface DiscoveryMarkersProps {
  map: maplibregl.Map
}

export function DiscoveryMarkers({ map }: DiscoveryMarkersProps) {
  const { discoveries } = useDiscoveries()
  const selectedDiscoveryIndex = useQueryStore((s) => s.selectedDiscoveryIndex)
  const selectedPinIndex = useQueryStore((s) => s.selectedPinIndex)
  const setSelectedDiscovery = useQueryStore((s) => s.setSelectedDiscovery)
  const markersRef = useRef<{ marker: maplibregl.Marker; el: HTMLDivElement }[]>([])

  // Create markers when discoveries change
  useEffect(() => {
    markersRef.current.forEach(({ marker }) => marker.remove())
    markersRef.current = []

    discoveries.forEach((d, i) => {
      const el = document.createElement('div')
      el.className = 'discovery-marker'

      const tooltip = document.createElement('div')
      tooltip.className = 'discovery-marker-tooltip'
      tooltip.textContent = `#${i + 1} ${d.name} · ${Math.round(d.score * 100)}%${d.similar_to ? ` · similar to ${d.similar_to}` : ''}`
      el.appendChild(tooltip)

      el.addEventListener('click', (e) => {
        e.stopPropagation()
        setSelectedDiscovery(i)
      })

      const marker = new maplibregl.Marker({ element: el })
        .setLngLat([d.lng, d.lat])
        .addTo(map)

      markersRef.current.push({ marker, el })
    })

    return () => {
      markersRef.current.forEach(({ marker }) => marker.remove())
      markersRef.current = []
    }
  }, [discoveries, map, setSelectedDiscovery])

  // Apply selection/related styles
  useEffect(() => {
    markersRef.current.forEach(({ el }, i) => {
      el.classList.toggle('selected', i === selectedDiscoveryIndex)
      // Show "related" ring when a pin is selected and this discovery came from it
      const isRelated = selectedPinIndex !== null
        && discoveries[i]?.best_pin_index === selectedPinIndex
        && selectedDiscoveryIndex === null
      el.classList.toggle('related', isRelated)
    })
  }, [selectedDiscoveryIndex, selectedPinIndex, discoveries])

  return null
}
