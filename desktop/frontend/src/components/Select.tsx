import { useEffect, useRef, useState } from "react";
import { Check, ChevronDown, Search } from "lucide-react";
import "./Select.css";

export interface SelectOption {
  value: string;
  label: string;
  meta?: string;
  disabled?: boolean;
}

// Replaces the native <select> everywhere in the app: same drop-in
// value/onChange/options shape, but a custom-styled panel instead of the
// browser's own OS-chrome dropdown, with search for anything long enough
// to need it. Used for option sets that are dynamic or too long for a
// SegmentedControl (connections, databases, tables, columns, ...).
export function Select({
  value,
  onChange,
  options,
  placeholder = "Select…",
  disabled,
  searchable,
  className = "",
}: {
  value: string;
  onChange: (value: string) => void;
  options: SelectOption[];
  placeholder?: string;
  disabled?: boolean;
  searchable?: boolean;
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [highlighted, setHighlighted] = useState(0);
  const rootRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const showSearch = searchable ?? options.length > 6;

  useEffect(() => {
    if (!open) return;
    function onClickOutside(e: MouseEvent) {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false);
    }
    document.addEventListener("mousedown", onClickOutside);
    return () => document.removeEventListener("mousedown", onClickOutside);
  }, [open]);

  useEffect(() => {
    if (!open) return;
    setQuery("");
    setHighlighted(Math.max(0, options.findIndex((o) => o.value === value)));
    if (showSearch) {
      const id = requestAnimationFrame(() => inputRef.current?.focus());
      return () => cancelAnimationFrame(id);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const filtered =
    showSearch && query.trim()
      ? options.filter((o) => o.label.toLowerCase().includes(query.trim().toLowerCase()))
      : options;

  function commit(opt: SelectOption) {
    if (opt.disabled) return;
    onChange(opt.value);
    setOpen(false);
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setHighlighted((h) => Math.min(h + 1, filtered.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setHighlighted((h) => Math.max(h - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      const opt = filtered[highlighted];
      if (opt) commit(opt);
    } else if (e.key === "Escape") {
      e.preventDefault();
      setOpen(false);
    }
  }

  const selected = options.find((o) => o.value === value);

  return (
    <div className={`select-root ${className}`} ref={rootRef} onKeyDown={handleKeyDown}>
      <button
        type="button"
        className={`select-trigger ${disabled ? "disabled" : ""}`}
        disabled={disabled}
        onClick={() => setOpen((v) => !v)}
      >
        <span className={`select-trigger-label ${!selected ? "placeholder" : ""}`}>{selected?.label ?? placeholder}</span>
        <ChevronDown size={14} className={`select-chevron ${open ? "open" : ""}`} />
      </button>
      {open && (
        <div className="select-panel">
          {showSearch && (
            <div className="select-search">
              <Search size={13} />
              <input
                ref={inputRef}
                value={query}
                onChange={(e) => {
                  setQuery(e.target.value);
                  setHighlighted(0);
                }}
                placeholder="Search..."
                className="select-search-input"
              />
            </div>
          )}
          <div className="select-list">
            {filtered.length === 0 && <div className="select-empty">No matches</div>}
            {filtered.map((opt, i) => (
              <button
                type="button"
                key={opt.value}
                className={`select-option ${i === highlighted ? "highlighted" : ""} ${opt.value === value ? "selected" : ""}`}
                disabled={opt.disabled}
                onMouseEnter={() => setHighlighted(i)}
                onClick={() => commit(opt)}
              >
                <span className="select-option-label">{opt.label}</span>
                {opt.meta && <span className="select-option-meta">{opt.meta}</span>}
                {opt.value === value && <Check size={13} />}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
