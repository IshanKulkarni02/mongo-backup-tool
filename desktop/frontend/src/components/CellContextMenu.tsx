import { useEffect, useRef } from "react";
import "./CellContextMenu.css";

export interface CellContextMenuItem {
  label: string;
  onClick: () => void;
}

interface Props {
  x: number;
  y: number;
  items: CellContextMenuItem[];
  onClose: () => void;
}

// CellContextMenu is a minimal floating menu (Copy / Send to Vector Compare
// / Send to Geo Viewer) anchored at the right-click point — DataGrid's
// mechanism for "act on this specific cell" without needing a global
// current-selection concept (see lib/transferStore.ts).
export function CellContextMenu({ x, y, items, onClose }: Props) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    }
    function handleKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    document.addEventListener("mousedown", handleClick);
    document.addEventListener("keydown", handleKey);
    return () => {
      document.removeEventListener("mousedown", handleClick);
      document.removeEventListener("keydown", handleKey);
    };
  }, [onClose]);

  return (
    <div className="cell-context-menu" style={{ left: x, top: y }} ref={ref}>
      {items.map((item) => (
        <button
          key={item.label}
          className="cell-context-menu-item"
          onClick={() => {
            item.onClick();
            onClose();
          }}
        >
          {item.label}
        </button>
      ))}
    </div>
  );
}
