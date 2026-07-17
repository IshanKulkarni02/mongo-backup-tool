import { useRef, useState, type MouseEvent } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { engine } from "../../wailsjs/go/models";
import { CellEditor } from "./CellEditor";
import { CellContextMenu, type CellContextMenuItem } from "./CellContextMenu";
import { useToast } from "./Toast";
import { sendTransfer, looksLikeVector, looksLikeGeoJSON } from "../lib/transferStore";
import "./DataGrid.css";

const COL_WIDTH = 200;
const ROW_HEIGHT = 32;

interface Props {
  columns: string[];
  rows: Record<string, engine.Cell>[];
  editableColumns?: Set<string>;
  onCellCommit?: (rowIndex: number, column: string, newDisplay: string, cell: engine.Cell) => void;
  /** Columns whose cells are foreign-key hyperlinks rather than editable text. */
  linkColumns?: Set<string>;
  onLinkClick?: (rowIndex: number, column: string, cell: engine.Cell) => void;
  onRowClick?: (rowIndex: number) => void;
  selectedRowIndex?: number;
}

// DataGrid renders a SQL result page: virtualized rows (so a few hundred
// rows scroll smoothly) laid out with CSS instead of a native <table> —
// absolutely-positioned <tr>s don't play well with virtualization, plain
// divs with fixed column widths do.
export function DataGrid({ columns, rows, editableColumns, onCellCommit, linkColumns, onLinkClick, onRowClick, selectedRowIndex }: Props) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const toast = useToast();
  const [menu, setMenu] = useState<{ x: number; y: number; items: CellContextMenuItem[] } | null>(null);

  function copyCell(cell: engine.Cell) {
    navigator.clipboard.writeText(cell.display);
    toast.push("success", "Copied to clipboard");
  }

  // Right-click a cell for a menu of what can be done with its value
  // directly — copy, or (when the content looks like the right shape)
  // send it straight to Vector Compare or the Geo Viewer instead of
  // round-tripping through the clipboard.
  function openCellMenu(e: MouseEvent, cell: engine.Cell | undefined) {
    e.preventDefault();
    if (!cell) return;
    const items: CellContextMenuItem[] = [{ label: "Copy value", onClick: () => copyCell(cell) }];
    if (looksLikeVector(cell.display)) {
      items.push(
        { label: "Send to Vector Compare — A", onClick: () => sendTransfer({ kind: "vector", slot: "a", value: cell.display }) },
        { label: "Send to Vector Compare — B", onClick: () => sendTransfer({ kind: "vector", slot: "b", value: cell.display }) }
      );
    }
    if (looksLikeGeoJSON(cell.display)) {
      items.push({ label: "Send to Geo Viewer", onClick: () => sendTransfer({ kind: "geojson", value: cell.display }) });
    }
    setMenu({ x: e.clientX, y: e.clientY, items });
  }

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 15,
  });

  const totalWidth = columns.length * COL_WIDTH;

  return (
    <div className="data-grid">
      <div className="data-grid-header-scroll">
        <div className="data-grid-header-row" style={{ width: totalWidth }}>
          {columns.map((col) => (
            <div key={col} className="data-grid-cell data-grid-header-cell mono" style={{ width: COL_WIDTH }}>
              {col}
            </div>
          ))}
        </div>
      </div>
      <div className="data-grid-body" ref={scrollRef}>
        <div style={{ height: virtualizer.getTotalSize(), width: totalWidth, position: "relative" }}>
          {virtualizer.getVirtualItems().map((vRow) => {
            const row = rows[vRow.index];
            return (
              <div
                key={vRow.key}
                className={`data-grid-row ${selectedRowIndex === vRow.index ? "selected" : ""}`}
                onClick={() => onRowClick?.(vRow.index)}
                style={{
                  position: "absolute",
                  top: 0,
                  left: 0,
                  width: totalWidth,
                  height: vRow.size,
                  transform: `translateY(${vRow.start}px)`,
                }}
              >
                {columns.map((col) => (
                  <div
                    key={col}
                    className="data-grid-cell"
                    style={{ width: COL_WIDTH }}
                    onContextMenu={(e) => openCellMenu(e, row[col])}
                    title="Right-click for options"
                  >
                    {linkColumns?.has(col) && row[col] ? (
                      <button className="data-grid-fk-link mono" onClick={() => onLinkClick?.(vRow.index, col, row[col])}>
                        {row[col].display}
                      </button>
                    ) : (
                      <CellEditor
                        cell={row[col]}
                        editable={!!editableColumns?.has(col)}
                        onCommit={(newDisplay) => onCellCommit?.(vRow.index, col, newDisplay, row[col])}
                      />
                    )}
                  </div>
                ))}
              </div>
            );
          })}
        </div>
      </div>
      {menu && <CellContextMenu x={menu.x} y={menu.y} items={menu.items} onClose={() => setMenu(null)} />}
    </div>
  );
}
