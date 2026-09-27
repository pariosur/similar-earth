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
import { BasemapToggle } from './BasemapToggle'
import { SearchMarkers } from './SearchMarkers'
import { SearchContextChip } from './SearchContextChip'
import { useSearchStore } from '../../stores/searchStore'

interface MapContainerProps {
  onFlyToReady?: (fn: (lat: number, lng: number) => void) => void
}

export function MapContainer({ onFlyToReady }: MapContainerProps) {
  const { mapContainerRef, mapRef, map, zoom, handleFlyTo } = useMapInit({ onFlyToReady })
  const { handlePillClick } = useHdState(mapRef, zoom)
  const hdState = useQueryStore((s) => s.hdState)
  const isSearch = useSearchStore((s) => s.mapMode) === 'search'

  return (
    <div className="relative w-full h-full">
      <div ref={mapContainerRef} className="absolute inset-0" />
      {map && !isSearch && <PinMarkers map={map} />}
      {map && !isSearch && <DiscoveryMarkers map={map} />}
      {map && isSearch && <SearchMarkers map={map} />}
      {map && <HeatmapLayer map={map} />}
      <SearchBar onFlyTo={handleFlyTo} />
      {isSearch ? <SearchContextChip /> : <>
        <ContextChip />
        <SelectionBanner />
        <MapLegend />
        <LegendBar onPillClick={handlePillClick} />
      </>}
      <BasemapToggle />

      {/* Shimmer overlay during HD computation */}
      {hdState === 'computing' && (
        <div className="absolute inset-0 pointer-events-none z-[5] animate-shimmer" />
      )}
    </div>
  )
}
