"use client";
/** 轻量 i18n：zh-CN 为源文案；zh-TW 逐字转繁（opencc 生成），en 按词典精确替换。
 *  以 MutationObserver 翻译文本节点与 placeholder/title/aria-label，WeakMap 保存原文以便切回。语言存 localStorage lb_lang。 */
import { EN } from "./i18n-en";
import { TW_PAIRS, TW_PHRASES } from "./i18n-tw";

export type Lang = "zh-CN" | "zh-TW" | "en";
export const LANGS: { k: Lang; label: string }[] = [{ k: "zh-CN", label: "简体中文" }, { k: "zh-TW", label: "繁體中文" }, { k: "en", label: "English" }];
const KEY = "lb_lang";
const TW = new Map<string, string>();
for (let i = 0; i + 1 < TW_PAIRS.length; i += 2) TW.set(TW_PAIRS[i], TW_PAIRS[i + 1]);

export function getLang(): Lang {
  if (typeof window === "undefined") return "zh-CN";
  const v = localStorage.getItem(KEY);
  return v === "zh-TW" || v === "en" ? v : "zh-CN";
}
const toTW = (s: string) => { const t = s.trim(); if (TW_PHRASES[t]) return s.replace(t, TW_PHRASES[t]); return s.replace(/[\u4e00-\u9fff]/g, (c) => TW.get(c) ?? c); };
export function tr(s: string, lang: Lang = getLang()): string {
  if (lang === "zh-CN" || !/[\u4e00-\u9fff]/.test(s)) return s;
  if (lang === "zh-TW") return toTW(s);
  const t = s.trim();
  const hit = EN[t];
  return hit ? s.replace(t, hit) : s;
}

const orig = new WeakMap<Node, string>();
const origAttr = new WeakMap<Element, Record<string, string>>();
const ATTRS = ["placeholder", "title", "aria-label"];
let busy = false;
function walk(root: Node, lang: Lang) {
  busy = true;
  try {
    const w = document.createTreeWalker(root, NodeFilter.SHOW_TEXT | NodeFilter.SHOW_ELEMENT);
    let n: Node | null = root;
    while (n) {
      if (n.nodeType === 3) {
        const pe = n.parentElement; const p = pe?.tagName;
        if (p !== "SCRIPT" && p !== "STYLE" && !pe?.closest("[translate=no]")) {
          const cur = n.nodeValue ?? "";
          let src = orig.get(n);
          // 节点内容被 React 改写过 ⇒ 以新内容为原文
          if (src === undefined || (cur !== src && cur !== tr(src, lang) && cur !== tr(src, "zh-TW") && cur !== tr(src, "en"))) { src = cur; orig.set(n, src); }
          const out = tr(src, lang);
          if (out !== cur) n.nodeValue = out;
        }
      } else if (n.nodeType === 1) {
        const el = n as Element;
        for (const a of ATTRS) {
          const cur = el.getAttribute(a);
          if (cur == null) continue;
          const rec = origAttr.get(el) ?? {};
          let src = rec[a];
          if (src === undefined || (cur !== src && cur !== tr(src, lang))) { src = cur; rec[a] = src; origAttr.set(el, rec); }
          const out = tr(src, lang);
          if (out !== cur) el.setAttribute(a, out);
        }
      }
      n = w.nextNode();
    }
  } finally { busy = false; }
}

let obs: MutationObserver | null = null;
export function applyLang(lang: Lang = getLang()) {
  if (typeof document === "undefined") return;
  document.documentElement.lang = lang;
  walk(document.body, lang);
  if (!obs) {
    obs = new MutationObserver((ms) => {
      if (busy) return;
      const l = getLang();
      for (const m of ms) {
        if (m.type === "characterData") walk(m.target, l);
        else if (m.type === "attributes") walk(m.target, l);
        else m.addedNodes.forEach((x) => walk(x, l));
      }
    });
    obs.observe(document.body, { subtree: true, childList: true, characterData: true, attributes: true, attributeFilter: ATTRS });
  }
}
export function setLang(lang: Lang) {
  localStorage.setItem(KEY, lang);
  applyLang(lang);
  window.dispatchEvent(new CustomEvent("lb-lang", { detail: lang }));
}
export function initLang() { applyLang(getLang()); }
