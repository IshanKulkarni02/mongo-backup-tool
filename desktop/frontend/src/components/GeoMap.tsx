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
        // react-leaflet's <GeoJSON> only reacts to `style` prop changes and
        // never re-applies `data` on updates, so remounting via `key` is
        // the only way to reflect new data. The full serialized string is
        // used rather than a truncated prefix: two genuinely different
        // GeoJSON payloads routinely share the same first several dozen
        // characters (the boilerplate FeatureCollection/Feature/properties
        // prefix alone exceeds that), which left the map showing stale
        // data for a shape that actually changed. Truncating never saved
        // any work anyway — JSON.stringify already does the full
        // serialization before a .slice() would cut the result down.
        key={JSON.stringify(geojson)}
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
