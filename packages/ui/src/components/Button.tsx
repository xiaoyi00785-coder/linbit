import * as React from "react";
import { cn } from "../cn";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger";
export type ButtonSize = "sm" | "md" | "lg";

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  block?: boolean;
}

const variants: Record<ButtonVariant, string> = {
  primary:
    "bg-lb-primary text-lb-on-primary hover:bg-lb-primary-dark disabled:opacity-50",
  secondary:
    "bg-lb-card-2 text-lb-text hover:bg-lb-border border border-lb-border",
  ghost: "bg-transparent text-lb-text hover:bg-lb-card-2",
  danger: "bg-lb-down text-white hover:opacity-90",
};

const sizes: Record<ButtonSize, string> = {
  sm: "h-8 px-3 text-sm",
  md: "h-10 px-4 text-sm",
  lg: "h-12 px-6 text-base",
};

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  function Button(
    { variant = "primary", size = "md", block, className, ...props },
    ref,
  ) {
    return (
      <button
        ref={ref}
        className={cn(
          "inline-flex items-center justify-center rounded-lb font-medium transition-colors disabled:cursor-not-allowed",
          variants[variant],
          sizes[size],
          block && "w-full",
          className,
        )}
        {...props}
      />
    );
  },
);
