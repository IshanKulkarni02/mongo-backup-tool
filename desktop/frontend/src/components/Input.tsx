import { InputHTMLAttributes, forwardRef, ReactNode } from "react";
import "./Input.css";

interface Props extends InputHTMLAttributes<HTMLInputElement> {
  label?: string;
  error?: string;
  mono?: boolean;
  trailing?: ReactNode;
}

export const Input = forwardRef<HTMLInputElement, Props>(
  ({ label, error, mono, trailing, className = "", id, ...rest }, ref) => {
    const inputId = id ?? rest.name;
    return (
      <div className="field">
        {label && (
          <label className="field-label" htmlFor={inputId}>
            {label}
          </label>
        )}
        <div className="field-control">
          <input
            ref={ref}
            id={inputId}
            className={`input ${mono ? "mono" : ""} ${error ? "input-error" : ""} ${className}`}
            {...rest}
          />
          {trailing && <div className="field-trailing">{trailing}</div>}
        </div>
        {error && <div className="field-error">{error}</div>}
      </div>
    );
  }
);
Input.displayName = "Input";
