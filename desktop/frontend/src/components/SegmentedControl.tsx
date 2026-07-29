import "./SegmentedControl.css";

export interface SegmentedOption {
  value: string;
  label: string;
  disabled?: boolean;
}

// A visible, click-once button group for small fixed option sets (engine
// type, chart type, export format, ...) — every choice is on screen at
// once, so there's no menu to open at all.
export function SegmentedControl({
  value,
  onChange,
  options,
  disabled,
  ariaLabel,
}: {
  value: string;
  onChange: (value: string) => void;
  options: SegmentedOption[];
  disabled?: boolean;
  ariaLabel?: string;
}) {
  return (
    <div className="segmented-control" role="radiogroup" aria-label={ariaLabel}>
      {options.map((opt) => (
        <button
          key={opt.value}
          type="button"
          role="radio"
          aria-checked={value === opt.value}
          className={`segmented-control-btn ${value === opt.value ? "active" : ""}`}
          disabled={disabled || opt.disabled}
          onClick={() => onChange(opt.value)}
        >
          {opt.label}
        </button>
      ))}
    </div>
  );
}
