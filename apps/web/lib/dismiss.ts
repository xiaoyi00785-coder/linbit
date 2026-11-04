"use client";
import { useEffect, type RefObject } from "react";
/** 下拉/弹出层统一关闭（同一时间仅一个打开）：点击外部（mousedown）或按 Esc 关闭；Esc 关闭后焦点回到触发区域。 */
export function useDismiss(open: boolean, close: () => void, ...refs: RefObject<HTMLElement | null>[]) {
  useEffect(() => {
    if (!open) return;
    // 同一时间只保留一个下拉：本下拉打开时广播，其他已打开的下拉收到后关闭
    const me = Symbol();
    window.dispatchEvent(new CustomEvent("lb-dd-open", { detail: me }));
    const other = (e: Event) => { if ((e as CustomEvent).detail !== me) close(); };
    window.addEventListener("lb-dd-open", other);
    const inside = (t: EventTarget | null) => refs.some((r) => r.current?.contains(t as Node));
    const down = (e: MouseEvent) => { if (!inside(e.target)) close(); };
    const key = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      close();
      const f = refs[0]?.current?.querySelector<HTMLElement>("button,[tabindex],input") || refs[0]?.current;
      if (f && document.activeElement && refs.some((r) => r.current?.contains(document.activeElement))) { try { f.focus({ preventScroll: true }); } catch {} }
    };
    document.addEventListener("mousedown", down);
    window.addEventListener("keydown", key);
    return () => { window.removeEventListener("lb-dd-open", other); document.removeEventListener("mousedown", down); window.removeEventListener("keydown", key); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
}
