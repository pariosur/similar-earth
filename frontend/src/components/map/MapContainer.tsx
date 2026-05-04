import 'maplibre-gl/dist/maplibre-gl.css'
import { useQueryStore } from '../../stores/queryStore'
import { useMapInit } from '../../hooks/useMapInit'
import { useHdState } from '../../hooks/useHdState'
import { PinMarkers } from './PinMarkers'
import { DiscoveryMarkers } from './DiscoveryMarkers'
import { HeatmapLayer } from './HeatmapLayer'
import { SearchBar } from './SearchBar'
import { ContextChip } from './ContextChip'
import { LegendBar } from './LegendBar'
import { SelectionBanner } from './SelectionBanner'
import { MapLegend } from './MapLegend'

interface MapContainerProps {
  onFlyToReady?: (fn: (lat: number, lng: number) => void) => void
}

export function MapContainer({ onFlyToReady }: MapContainerProps) {
  const { mapContainerRef, mapRef, mapReady, zoom, handleFlyTo } = useMapInit({ onFlyToReady })
  const { handlePillClick } = useHdState(mapRef, zoom)
  const hdState = useQueryStore((s) => s.hdState)

  return (
    <div className="relative w-full h-full">
      <div ref={mapContainerRef} className="absolute inset-0" />
      {mapReady && mapRef.current && <PinMarkers map={mapRef.current} />}
      {mapReady && mapRef.current && <DiscoveryMarkers map={mapRef.current} />}
      {mapReady && mapRef.current && <HeatmapLayer map={mapRef.current} />}
      <SearchBar onFlyTo={handleFlyTo} />
      <ContextChip />
      <SelectionBanner />
      <MapLegend />
      <LegendBar onPillClick={handlePillClick} />

      {/* Shimmer overlay during HD computation */}
      {hdState === 'computing' && (
        <div className="absolute inset-0 pointer-events-none z-[5] animate-shimmer" />
      )}
    </div>
  )
}
