import * as React from "react";
import { cn } from "../cn";

export interface TextLogoProps extends React.HTMLAttributes<HTMLSpanElement> {
  /** 字号（px），默认 20 */
  size?: number;
}

/**
 * 文字 Logo：品牌名 "Linbit"。
 * 纯文字 + 品牌蓝，不依赖图片资源。
 */
export function TextLogo({ size = 20, className, style, ...props }: TextLogoProps) {
  return (
    <span
      className={cn(
        "font-extrabold tracking-tight text-lb-primary select-none",
        className,
      )}
      style={{ fontSize: size, lineHeight: 1, ...style }}
      {...props}
    >
      Linbit
    </span>
  );
}
