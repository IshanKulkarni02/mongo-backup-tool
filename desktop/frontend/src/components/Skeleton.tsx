import "./Skeleton.css";

export function Skeleton({ width = "100%", height = "1em" }: { width?: string | number; height?: string | number }) {
  return <div className="skeleton" style={{ width, height }} />;
}
