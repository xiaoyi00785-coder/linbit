"use client";
/**
 * 全站遮罩层（弹窗）统一行为：
 * - Esc 关闭最上层弹窗（等价于点击遮罩；组件自带 Esc 处理的先执行，已关闭则不重复）
 * - 打开期间锁定背景滚动，并用 padding-right 补偿滚动条宽度，关闭后原样恢复（无布局抖动）
 * - 关闭后焦点回到打开前的元素
 * 仅依赖遮罩类名，不改动各弹窗组件的开关逻辑。
 */
import { useEffect } from "react";

export const MASK_SEL = ".fx-modal,.xmask,.mask,.ff-mask";

export function OverlayManager() {
  useEffect(() => {
    const html = document.documentElement;
    let locked = false;
    let saved: { pr: string; ov: string } | null = null;
    let focusBack: HTMLElement | null = null;
    let lastFocus: HTMLElement | null = null;
    const top = () => { const a = document.querySelectorAll<HTMLElement>(MASK_SEL); return a.length ? a[a.length - 1] : null; };
    const onFocus = (e: FocusEvent) => { const t = e.target as HTMLElement; if (t && !t.closest?.(MASK_SEL)) lastFocus = t; };
    const onDown = (e: Event) => { const t = e.target as HTMLElement; if (t && !t.closest?.(MASK_SEL)) lastFocus = t.closest?.("button,a,[tabindex],[role=button],input,select") as HTMLElement || lastFocus; };
    const sync = () => {
      const open = !!top();
      if (open && !locked) {
        locked = true;
        focusBack = lastFocus && document.contains(lastFocus) ? lastFocus : null;
        const sb = window.innerWidth - html.clientWidth;
        saved = { pr: document.body.style.paddingRight, ov: html.style.overflow };
        html.style.overflow = "hidden";
        if (sb > 0) document.body.style.paddingRight = `${(parseFloat(getComputedStyle(document.body).paddingRight) || 0) + sb}px`;
      } else if (!open && locked) {
        locked = false;
        if (saved) { html.style.overflow = saved.ov; document.body.style.paddingRight = saved.pr; saved = null; }
        const f = focusBack; focusBack = null;
        if (f && document.contains(f) && (!document.activeElement || document.activeElement === document.body)) { try { f.focus({ preventScroll: true }); } catch {} }
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      const m = top(); if (!m) return;
      // 让组件自身的 Esc 处理先执行；若弹窗仍在，则模拟点击遮罩关闭
      setTimeout(() => { if (m.isConnected && top() === m) m.click(); }, 0);
    };
    const mo = new MutationObserver(sync);
    mo.observe(document.body, { childList: true, subtree: true });
    document.addEventListener("focusin", onFocus);
    document.addEventListener("pointerdown", onDown, true);
    window.addEventListener("keydown", onKey);
    sync();
    return () => { mo.disconnect(); document.removeEventListener("focusin", onFocus); document.removeEventListener("pointerdown", onDown, true); window.removeEventListener("keydown", onKey); if (locked && saved) { html.style.overflow = saved.ov; document.body.style.paddingRight = saved.pr; } };
  }, []);
  return null;
}
