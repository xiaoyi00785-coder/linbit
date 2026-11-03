import * as React from "react";
import { cn } from "../cn";

export interface TabItem {
  key: string;
  label: React.ReactNode;
}

export interface TabsProps {
  items: TabItem[];
  value: string;
  onChange: (key: string) => void;
  className?: string;
  size?: "sm" | "md";
}

export function Tabs({
  items,
  value,
  onChange,
  className,
  size = "md",
}: TabsProps) {
  return (
    <div
      role="tablist"
      className={cn("flex gap-6 border-b border-lb-border", className)}
    >
      {items.map((item) => {
        const active = item.key === value;
        return (
          <button
            key={item.key}
            role="tab"
            aria-selected={active}
            onClick={() => onChange(item.key)}
            className={cn(
              "-mb-px border-b-2 pb-2 font-medium transition-colors",
              size === "sm" ? "text-sm" : "text-base",
              active
                ? "border-lb-primary text-lb-text"
                : "border-transparent text-lb-text-2 hover:text-lb-text",
            )}
          >
            {item.label}
          </button>
        );
      })}
    </div>
  );
}
