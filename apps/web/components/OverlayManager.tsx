"use client";
/**
 * 全站遮罩层（弹窗）统一行为：
 * - Esc 关闭最上层弹窗（等价于点击遮罩；组件自带 Esc 处理的先执行，已关闭则不重复）
 * - 打开期间锁定背景滚动，并用 padding-right 补偿滚动条宽度，关闭后原样恢复（无布局抖动）
 * - 关闭后焦点回到打开前的元素
 * - 焦点陷阱：打开时焦点移入弹窗，Tab / Shift+Tab 在弹窗内循环；弹窗补齐 role="dialog" 与 aria-modal
 * - 防误关：遮罩带 data-dismissible="false"、表单已输入内容（自动标记 data-dirty）或有提交中（[aria-busy="true"]）时，
 *   点击遮罩与 Esc 均不关闭（轻微抖动提示），只能用弹窗内的关闭按钮 / 取消关闭。
 * 仅依赖遮罩类名，不改动各弹窗组件的开关逻辑。
 */
import { useEffect } from "react";

export const MASK_SEL = ".fx-modal,.xmask,.mask,.ff-mask";

/** 遮罩是否受保护（不可通过点击遮罩 / Esc 关闭） */
export function isGuarded(m: HTMLElement) {
  if (m.dataset.dismissible === "false" || m.getAttribute("aria-busy") === "true" || m.querySelector('[aria-busy="true"]')) return true;
  // 已输入内容：仅当弹窗内有显式关闭控件时才拦截，避免把用户困在没有关闭按钮的弹窗里
  return m.dataset.dirty === "1" && hasCloser(m);
}
const CLOSER = '.fx-x,.tk-x,[aria-label="关闭"],[aria-label="Close"]';
function hasCloser(m: HTMLElement) {
  if (m.querySelector(CLOSER)) return true;
  return [...m.querySelectorAll("button,a")].some((b) => /^(取消|关闭|✕|×)$/.test((b.textContent || "").trim()));
}
const FOCUSABLE = 'a[href],button:not([disabled]),input:not([disabled]):not([type="hidden"]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"]),[role="button"],[role="tab"]';
const focusables = (m: HTMLElement) => [...m.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((e) => e.offsetParent !== null || e === document.activeElement);
/** 焦点移入弹窗：优先 autofocus / 输入框，其次第一个可聚焦元素，再次弹窗本身 */
function focusInto(m: HTMLElement) {
  const box = (m.querySelector<HTMLElement>('[role="dialog"]') || m.firstElementChild || m) as HTMLElement;
  if (!box.getAttribute("role")) box.setAttribute("role", "dialog");
  box.setAttribute("aria-modal", "true");
  if (box.contains(document.activeElement)) return;
  const f = box.querySelector<HTMLElement>("[autofocus],input:not([disabled]):not([type=hidden]),textarea") || focusables(box)[0];
  if (f) { try { f.focus({ preventScroll: true }); } catch {} return; }
  if (!box.hasAttribute("tabindex")) box.setAttribute("tabindex", "-1");
  try { box.focus({ preventScroll: true }); } catch {}
}
const nudge = (m: HTMLElement) => { const b = m.firstElementChild as HTMLElement | null; if (!b) return; b.classList.remove("ov-nudge"); void b.offsetWidth; b.classList.add("ov-nudge"); };

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
    let trapped: HTMLElement | null = null;
    const sync = () => {
      const m0 = top();
      if (m0 && m0 !== trapped) { trapped = m0; focusInto(m0); } else if (!m0) trapped = null;
      const open = !!m0;
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
    // 捕获阶段拦截：受保护弹窗的 Esc / 遮罩点击不传给组件自身的关闭处理
    const onKeyCap = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      const m = top(); if (!m || !isGuarded(m)) return;
      e.stopImmediatePropagation(); e.preventDefault(); nudge(m);
    };
    const onClickCap = (e: MouseEvent) => {
      const m = top(); if (!m || e.target !== m || !isGuarded(m)) return;
      e.stopPropagation(); e.preventDefault(); nudge(m);
    };
    const onInput = (e: Event) => {
      const t = e.target as HTMLInputElement; if (!t || t.type === "search") return;
      const m = t.closest?.(MASK_SEL) as HTMLElement | null; if (m) { if (t.value) m.dataset.dirty = "1"; }
    };
    const onTab = (e: KeyboardEvent) => {
      if (e.key !== "Tab") return;
      const m = top(); if (!m) return;
      const L = focusables(m); if (!L.length) { e.preventDefault(); return; }
      const a = document.activeElement as HTMLElement | null, first = L[0], last = L[L.length - 1];
      if (!a || !m.contains(a)) { e.preventDefault(); first.focus(); }
      else if (e.shiftKey && a === first) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && a === last) { e.preventDefault(); first.focus(); }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      const m = top(); if (!m || isGuarded(m)) return;
      // 让组件自身的 Esc 处理先执行；若弹窗仍在，则模拟点击遮罩关闭
      setTimeout(() => { if (m.isConnected && top() === m) m.click(); }, 0);
    };
    const mo = new MutationObserver(sync);
    mo.observe(document.body, { childList: true, subtree: true });
    document.addEventListener("focusin", onFocus);
    document.addEventListener("pointerdown", onDown, true);
    window.addEventListener("keydown", onKey);
    window.addEventListener("keydown", onTab, true);
    window.addEventListener("keydown", onKeyCap, true);
    window.addEventListener("click", onClickCap, true);
    document.addEventListener("input", onInput, true);
    sync();
    return () => { mo.disconnect(); document.removeEventListener("focusin", onFocus); document.removeEventListener("pointerdown", onDown, true); window.removeEventListener("keydown", onKey); window.removeEventListener("keydown", onTab, true); window.removeEventListener("keydown", onKeyCap, true); window.removeEventListener("click", onClickCap, true); document.removeEventListener("input", onInput, true); if (locked && saved) { html.style.overflow = saved.ov; document.body.style.paddingRight = saved.pr; } };
  }, []);
  return null;
}
