import { useMemo } from "react";
import { MapContainer, TileLayer, GeoJSON } from "react-leaflet";
import L from "leaflet";
import "leaflet/dist/leaflet.css";
import "./GeoMap.css";

// Leaflet's default marker icons reference image files by URL that Vite's
// bundler doesn't resolve automatically; wire them up explicitly once.
delete (L.Icon.Default.prototype as unknown as { _getIconUrl?: unknown })._getIconUrl;
L.Icon.Default.mergeOptions({
  iconRetinaUrl: "https://unpkg.com/leaflet@1.9.4/dist/images/marker-icon-2x.png",
  iconUrl: "https://unpkg.com/leaflet@1.9.4/dist/images/marker-icon.png",
  shadowUrl: "https://unpkg.com/leaflet@1.9.4/dist/images/marker-shadow.png",
});

// Typed as `object` rather than pulling in the separate `geojson` package's
// types (react-leaflet's own props accept any GeoJSON-shaped object) — one
// fewer dependency for a component that just needs "some JSON with a
// `type` field Leaflet recognizes."
interface Props {
  geojson: object;
}

// GeoMap renders GeoJSON (points, polygons, or a FeatureCollection of
// either) on a map — geofence boundaries and punch-in coordinates as an
// actual shape instead of raw ST_AsGeoJSON/GeoJSON text. Requires internet
// access for the base map tiles (OpenStreetMap); the GeoJSON layer itself
// still renders without it, just without a basemap underneath.
export function GeoMap({ geojson }: Props) {
  const bounds = useMemo(() => {
    try {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const layer = L.geoJSON(geojson as any);
      const b = layer.getBounds();
      return b.isValid() ? b : undefined;
    } catch {
      return undefined;
    }
  }, [geojson]);

  return (
    <div className="geo-map-wrap">
      <MapContainer
        key={JSON.stringify(geojson).slice(0, 64)}
        bounds={bounds}
        center={[0, 0]}
        zoom={2}
        className="geo-map"
        scrollWheelZoom
      >
        <TileLayer attribution="&copy; OpenStreetMap contributors" url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png" />
        {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
        <GeoJSON data={geojson as any} />
      </MapContainer>
    </div>
  );
}
