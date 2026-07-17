import { useState } from "react";
import { engine } from "../../wailsjs/go/models";
import "./CellEditor.css";

interface Props {
  cell: engine.Cell | undefined;
  editable: boolean;
  onCommit: (newDisplay: string) => void;
}

// CellEditor renders one SQL result cell, type-aware (null/binary render as
// muted placeholders rather than literal text), and turns into an inline
// text input on double-click when the column is editable (has a resolvable
// single-column primary key — see TableView).
export function CellEditor({ cell, editable, onCommit }: Props) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");

  if (!cell) return <span className="cell-null">–</span>;

  if (editing) {
    return (
      <input
        className="cell-input mono"
        autoFocus
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={() => setEditing(false)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            setEditing(false);
            if (draft !== cell.display) onCommit(draft);
          } else if (e.key === "Escape") {
            setEditing(false);
          }
        }}
      />
    );
  }

  const startEdit = () => {
    if (!editable) return;
    setDraft(cell.display);
    setEditing(true);
  };

  if (cell.type === "null") {
    return (
      <span className="cell-null" onDoubleClick={startEdit}>
        NULL
      </span>
    );
  }
  if (cell.type === "binary") {
    return <span className="cell-binary">{cell.display}</span>;
  }
  return (
    <span
      className={`cell-value mono ${cell.type === "number" ? "cell-number" : ""} ${editable ? "cell-editable" : ""}`}
      onDoubleClick={startEdit}
      title={editable ? "Double-click to edit" : undefined}
    >
      {cell.display}
    </span>
  );
}
