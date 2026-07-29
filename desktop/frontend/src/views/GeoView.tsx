import { useEffect, useState } from "react";
import { Clipboard } from "lucide-react";
import { GeoMap } from "../components/GeoMap";
import { useToast } from "../components/Toast";
import { takePendingTransfer, subscribeTransfer } from "../lib/transferStore";
import "./GeoView.css";

const SAMPLE_GEOJSON = `{
  "type": "FeatureCollection",
  "features": [
    {
      "type": "Feature",
      "properties": { "name": "job site geofence" },
      "geometry": {
        "type": "Polygon",
        "coordinates": [[
          [-122.084, 37.4219], [-122.083, 37.4219],
          [-122.083, 37.4225], [-122.084, 37.4225],
          [-122.084, 37.4219]
        ]]
      }
    },
    {
      "type": "Feature",
      "properties": { "name": "clock-in punch" },
      "geometry": { "type": "Point", "coordinates": [-122.0835, 37.4222] }
    }
  ]
}`;

// GeoView renders GeoJSON (right-click a GeoJSON-shaped cell in Tables or
// Query and choose "Send to Geo Viewer", or paste directly — e.g. the
// output of Postgres' ST_AsGeoJSON, or a GeoJSON field from a MongoDB
// document) as an actual map instead of raw polygon/point text — for
// spotting geofence boundary bugs at a glance. Requires internet access
// for the base map tiles; the shapes themselves still render without it.
export function GeoView() {
  const [text, setText] = useState(SAMPLE_GEOJSON);
  const [parsed, setParsed] = useState<object | null>(() => JSON.parse(SAMPLE_GEOJSON));
  const [error, setError] = useState("");
  const toast = useToast();

  useEffect(() => {
    const applyOnMount = takePendingTransfer("geojson");
    if (applyOnMount?.kind === "geojson") handleChange(applyOnMount.value);
    return subscribeTransfer("geojson", (t) => {
      if (t.kind !== "geojson") return;
      handleChange(t.value);
      toast.push("success", "Sent to Geo Viewer");
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function handleChange(value: string) {
    setText(value);
    try {
      setParsed(JSON.parse(value));
      setError("");
    } catch (e) {
      setError(String(e));
      setParsed(null);
    }
  }

  async function pasteFromClipboard() {
    try {
      const clip = await navigator.clipboard.readText();
      handleChange(clip);
    } catch {
      toast.push("error", "Couldn't read the clipboard — your browser/OS may require a permission prompt first");
    }
  }

  return (
    <div>
      <div className="view-header">
        <h1 className="view-title">Geo Viewer</h1>
      </div>
      <p className="geo-hint">
        Paste GeoJSON — e.g. the output of Postgres' <span className="mono">ST_AsGeoJSON()</span>, or a GeoJSON field
        from a MongoDB document — to render points and polygons on a map instead of reading raw coordinate text.
        Right-click a cell in Tables or Query to copy its value first.
      </p>

      <div className="geo-editor-header">
        <button className="vector-paste-btn" onClick={pasteFromClipboard} title="Paste from clipboard">
          <Clipboard size={13} /> Paste from clipboard
        </button>
      </div>
      <textarea className="input geo-editor mono" rows={10} value={text} onChange={(e) => handleChange(e.target.value)} />
      {error && <div className="query-error">{error}</div>}

      {parsed && <GeoMap geojson={parsed} />}
    </div>
  );
}
