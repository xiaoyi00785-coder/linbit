"use client";
/** 轻量 i18n：zh-CN 为源文案；zh-TW 逐字转繁（opencc 生成），en/ko 按词典精确替换。
 *  以 MutationObserver 翻译文本节点与 placeholder/title/aria-label，WeakMap 保存原文以便切回。语言存 localStorage lb_lang。 */
import { EN } from "./i18n-en";
import { EN_T, EN_P } from "./i18n-en2";
import { KO } from "./i18n-ko";
import { KO_T, KO_P } from "./i18n-ko2";
import { TW_PAIRS, TW_PHRASES, TW_P } from "./i18n-tw";

export type Lang = "zh-CN" | "zh-TW" | "en" | "ko";
export const LANGS: { k: Lang; label: string }[] = [
  { k: "zh-CN", label: "简体中文" },
  { k: "zh-TW", label: "繁體中文" },
  { k: "en", label: "English" },
  { k: "ko", label: "한국어" },
];
const KEY = "lb_lang";
const TW = new Map<string, string>();
for (let i = 0; i + 1 < TW_PAIRS.length; i += 2) TW.set(TW_PAIRS[i], TW_PAIRS[i + 1]);

export function getLang(): Lang {
  if (typeof window === "undefined") return "zh-CN";
  const v = localStorage.getItem(KEY);
  return v === "zh-TW" || v === "en" || v === "ko" ? v : "zh-CN";
}
const TW_PATS = TW_P.map(([k, v]) => [new RegExp("^" + k.split("{}").map((x) => x.replace(/[.*+?^$()|[\]\\]/g, "\\$&")).join("(.+?)") + "$"), v] as const);
function toTW(s: string) {
  const t = s.trim();
  if (TW_PHRASES[t]) return s.replace(t, TW_PHRASES[t]);
  for (const [re, v] of TW_PATS) {
    const m = t.match(re);
    if (m) { let i = 1; return s.replace(t, v.replace(/\{\}/g, () => toTW(m[i++] ?? ""))); }
  }
  return s.replace(/[\u4e00-\u9fff]/g, (c) => TW.get(c) ?? c);
}

const DICT: Record<"en" | "ko", Record<string, string>> = { en: EN, ko: KO };
const TPL: Record<"en" | "ko", Record<string, string>> = { en: EN_T, ko: KO_T };
const PATS: Record<"en" | "ko", ReadonlyArray<readonly [RegExp, string]>> = {
  en: EN_P.map(([k, v]) => [new RegExp("^" + k.split("{}").map((x) => x.replace(/[.*+?^$()|[\]\\]/g, "\\$&")).join("(.+?)") + "$"), v] as const),
  ko: KO_P.map(([k, v]) => [new RegExp("^" + k.split("{}").map((x) => x.replace(/[.*+?^$()|[\]\\]/g, "\\$&")).join("(.+?)") + "$"), v] as const),
};

export function tr(s: string, lang: Lang = getLang()): string {
  if (lang === "zh-CN") return s;
  if (!/[\u4e00-\u9fff]/.test(s)) {
    // 无汉字但含全角括号的网络展示名（如「Tron（TRC20）」）：en/ko 下仅按词典精确项换成半角写法
    if ((lang === "en" || lang === "ko") && /[（）]/.test(s)) {
      const t = s.trim();
      const hit = DICT[lang][t];
      if (hit) return s.replace(t, hit);
    }
    return s;
  }
  if (lang === "zh-TW") return toTW(s);
  const dict = DICT[lang];
  const t = s.trim();
  const hit = dict[t] ?? tpl(t, lang) ?? pat(t, lang);
  return hit ? s.replace(t, hit) : s.replace(/[\u4e00-\u9fff][^\x00-\x7f]*[\u4e00-\u9fff]|[\u4e00-\u9fff]/g, (r) => dict[r] ?? tpl(r, lang) ?? r);
}
/** 整句占位匹配：{} 捕获的片段（币种、网络名、金额等）各自再翻译后回填 */
function pat(t: string, lang: "en" | "ko"): string | undefined {
  const dict = DICT[lang];
  for (const [re, v] of PATS[lang]) {
    const m = t.match(re);
    if (m) { let i = 1; return v.replace(/\{\}/g, () => { const x = m[i++] ?? ""; return dict[x.trim()] ?? tr(x, lang); }); }
  }
  return undefined;
}
/** 模板匹配：数字归一为 #，命中后按顺序回填原数字 */
const NUM = /\d+(?:[.,]\d+)*/g;
function tpl(t: string, lang: "en" | "ko"): string | undefined {
  const v = TPL[lang][t.replace(NUM, "#")];
  if (!v) return undefined;
  const nums = t.match(NUM) ?? [];
  let i = 0;
  return v.replace(/#/g, () => nums[i++] ?? "#");
}

const orig = new WeakMap<Node, string>();
const origAttr = new WeakMap<Element, Record<string, string>>();
const ATTRS = ["placeholder", "title", "aria-label"];
let busy = false;
function isTranslatedForm(src: string, cur: string, lang: Lang) {
  if (cur === tr(src, lang)) return true;
  if (cur === tr(src, "zh-TW")) return true;
  if (cur === tr(src, "en")) return true;
  if (cur === tr(src, "ko")) return true;
  return false;
}
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
          if (src === undefined || (cur !== src && !isTranslatedForm(src, cur, lang))) { src = cur; orig.set(n, src); }
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
          // 与文本节点相同：已是任一语言译文时保留原文缓存，避免跨语言切换把译文写进 origAttr
          if (src === undefined || (cur !== src && !isTranslatedForm(src, cur, lang))) { src = cur; rec[a] = src; origAttr.set(el, rec); }
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
  document.documentElement.lang = lang === "ko" ? "ko" : lang;
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
