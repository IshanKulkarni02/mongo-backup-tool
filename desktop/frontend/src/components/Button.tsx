import { ButtonHTMLAttributes, forwardRef } from "react";
import "./Button.css";

type Variant = "primary" | "ghost" | "danger";

interface Props extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
}

export const Button = forwardRef<HTMLButtonElement, Props>(
  ({ variant = "primary", className = "", ...rest }, ref) => (
    <button ref={ref} className={`btn btn-${variant} ${className}`} {...rest} />
  )
);
Button.displayName = "Button";
