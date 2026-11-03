import * as React from "react";
import { cn } from "../cn";

export interface InputProps
  extends React.InputHTMLAttributes<HTMLInputElement> {
  label?: string;
  hint?: string;
  error?: string;
}

export const Input = React.forwardRef<HTMLInputElement, InputProps>(
  function Input({ label, hint, error, className, id, ...props }, ref) {
    const inputId = id ?? props.name;
    return (
      <label className="block" htmlFor={inputId}>
        {label && (
          <span className="mb-1.5 block text-sm text-lb-text-2">{label}</span>
        )}
        <input
          ref={ref}
          id={inputId}
          className={cn(
            "h-11 w-full rounded-lb border border-lb-border bg-lb-card-2 px-3 text-sm text-lb-text placeholder:text-lb-text-3 outline-none transition-colors focus:border-lb-primary",
            error && "border-lb-down focus:border-lb-down",
            className,
          )}
          {...props}
        />
        {error ? (
          <span className="mt-1 block text-xs text-lb-down">{error}</span>
        ) : hint ? (
          <span className="mt-1 block text-xs text-lb-text-3">{hint}</span>
        ) : null}
      </label>
    );
  },
);
