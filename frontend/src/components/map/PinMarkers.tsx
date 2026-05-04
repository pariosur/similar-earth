import { useEffect, useRef } from 'react'
import maplibregl from 'maplibre-gl'
import { useQueryStore } from '../../stores/queryStore'
import { useDiscoveries } from '../../hooks/useDiscoveries'

interface PinMarkersProps {
  map: maplibregl.Map
}

export function PinMarkers({ map }: PinMarkersProps) {
  const pins = useQueryStore((s) => s.pins)
  const selectedPinIndex = useQueryStore((s) => s.selectedPinIndex)
  const selectedDiscoveryIndex = useQueryStore((s) => s.selectedDiscoveryIndex)
  const setSelectedPin = useQueryStore((s) => s.setSelectedPin)
  const { discoveries } = useDiscoveries()
  const markersRef = useRef<{ marker: maplibregl.Marker; el: HTMLDivElement }[]>([])

  // Create/update markers when pins change
  useEffect(() => {
    markersRef.current.forEach(({ marker }) => marker.remove())
    markersRef.current = []

    pins.forEach((pin, i) => {
      const el = document.createElement('div')
      el.className = 'pin-marker'
      el.textContent = String(i + 1)

      const tooltip = document.createElement('div')
      tooltip.className = 'pin-marker-tooltip'
      tooltip.textContent = pin.label ? `#${i + 1} ${pin.label}` : `Pin #${i + 1}`
      el.appendChild(tooltip)

      el.addEventListener('click', (e) => {
        e.stopPropagation()
        setSelectedPin(i)
      })

      const marker = new maplibregl.Marker({ element: el })
        .setLngLat([pin.lng, pin.lat])
        .addTo(map)

      markersRef.current.push({ marker, el })
    })

    return () => {
      markersRef.current.forEach(({ marker }) => marker.remove())
      markersRef.current = []
    }
  }, [pins, map, setSelectedPin])

  // Apply selection/related styles when selection changes
  useEffect(() => {
    const relatedPinIdx = selectedDiscoveryIndex !== null && discoveries[selectedDiscoveryIndex]
      ? discoveries[selectedDiscoveryIndex].best_pin_index
      : null

    markersRef.current.forEach(({ el }, i) => {
      el.classList.toggle('selected', i === selectedPinIndex)
      el.classList.toggle('related', i === relatedPinIdx && selectedPinIndex === null)
    })
  }, [selectedPinIndex, selectedDiscoveryIndex, discoveries])

  return null
}
