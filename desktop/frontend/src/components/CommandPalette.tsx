import { useEffect, useMemo, useRef, useState, type ComponentType } from "react";
import { Search } from "lucide-react";
import "./CommandPalette.css";

export interface CommandPaletteItem {
  id: string;
  label: string;
  icon?: ComponentType<{ size?: number }>;
  onSelect: () => void;
}

// CommandPalette is the app's first global keyboard shortcut (Cmd/Ctrl+K)
// — every other keydown listener in the codebase (CellContextMenu, Modal)
// is scoped to a single open overlay and only handles Escape. Commands are
// passed in rather than owned here, since App.tsx already computes the
// capability-filtered nav list this palette is built from.
export function CommandPalette({ items }: { items: CommandPaletteItem[] }) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen((v) => !v);
      } else if (e.key === "Escape") {
        setOpen(false);
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  useEffect(() => {
    if (!open) return;
    setQuery("");
    setSelected(0);
    const id = requestAnimationFrame(() => inputRef.current?.focus());
    return () => cancelAnimationFrame(id);
  }, [open]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return items;
    return items.filter((i) => i.label.toLowerCase().includes(q));
  }, [items, query]);

  useEffect(() => {
    setSelected(0);
  }, [query]);

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setSelected((s) => Math.min(s + 1, filtered.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setSelected((s) => Math.max(s - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      const item = filtered[selected];
      if (item) {
        item.onSelect();
        setOpen(false);
      }
    }
  }

  if (!open) return null;

  return (
    <div className="command-palette-overlay" onMouseDown={() => setOpen(false)}>
      <div className="command-palette" onMouseDown={(e) => e.stopPropagation()}>
        <div className="command-palette-search">
          <Search size={16} />
          <input
            ref={inputRef}
            className="command-palette-input"
            placeholder="Jump to..."
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={handleKeyDown}
          />
        </div>
        <div className="command-palette-list">
          {filtered.length === 0 && <div className="command-palette-empty">No matches</div>}
          {filtered.map((item, i) => {
            const Icon = item.icon;
            return (
              <button
                key={item.id}
                className={`command-palette-item ${i === selected ? "active" : ""}`}
                onMouseEnter={() => setSelected(i)}
                onClick={() => {
                  item.onSelect();
                  setOpen(false);
                }}
              >
                {Icon && <Icon size={14} />}
                {item.label}
              </button>
            );
          })}
        </div>
      </div>
    </div>
  );
}
