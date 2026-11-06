// 跨语言切换时 placeholder/title/aria-label 原文缓存不得被译文覆盖。
// node --experimental-strip-types --import ./scripts/ts-resolve-register.mjs scripts/check-i18n-attr-cache.mjs
import assert from "node:assert/strict";

/** 极简 DOM：满足 i18n walk / TreeWalker / MutationObserver / localStorage */
const store = new Map();
const localStorage = {
  getItem: (k) => (store.has(k) ? store.get(k) : null),
  setItem: (k, v) => { store.set(k, String(v)); },
  removeItem: (k) => { store.delete(k); },
};

class TextNode {
  constructor(v) { this.nodeType = 3; this.nodeValue = v; this.parentElement = null; }
}
class Elem {
  constructor(tag) {
    this.nodeType = 1; this.tagName = tag.toUpperCase(); this.parentElement = null;
    this.childNodes = []; this.attrs = Object.create(null);
  }
  appendChild(n) { n.parentElement = this; this.childNodes.push(n); return n; }
  getAttribute(a) { return a in this.attrs ? this.attrs[a] : null; }
  setAttribute(a, v) { this.attrs[a] = String(v); }
  closest() { return null; }
  querySelector() { return null; }
}

function flatten(root, out = []) {
  out.push(root);
  if (root.nodeType === 1) for (const c of root.childNodes) flatten(c, out);
  return out;
}

const NodeFilter = { SHOW_TEXT: 4, SHOW_ELEMENT: 1 };
class TreeWalker {
  constructor(root, what) {
    this._nodes = flatten(root).filter((n) =>
      (n.nodeType === 3 && (what & 4)) || (n.nodeType === 1 && (what & 1)));
    this._i = -1;
  }
  nextNode() { this._i += 1; return this._i < this._nodes.length ? this._nodes[this._i] : null; }
}

class MutationObserver {
  constructor() {}
  observe() {}
}

const html = new Elem("html");
const body = new Elem("body");
html.appendChild(body);
const document = {
  body,
  documentElement: html,
  createTreeWalker: (root, what) => new TreeWalker(root, what),
};
const window = {
  localStorage,
  dispatchEvent() {},
};
globalThis.window = window;
globalThis.document = document;
globalThis.localStorage = localStorage;
globalThis.NodeFilter = NodeFilter;
globalThis.MutationObserver = MutationObserver;
globalThis.CustomEvent = class { constructor(t, i) { this.type = t; this.detail = i?.detail; } };

const { setLang, applyLang, tr, getLang } = await import("../lib/i18n.ts");

const ATTRS = ["placeholder", "title", "aria-label"];
function makeFixture() {
  body.childNodes.length = 0;
  const wrap = new Elem("div");
  const input = new Elem("input");
  input.setAttribute("placeholder", "搜索");
  input.setAttribute("title", "语言与地区");
  input.setAttribute("aria-label", "关闭");
  const txt = new TextNode("资产");
  wrap.appendChild(input);
  wrap.appendChild(txt);
  body.appendChild(wrap);
  return { input, txt };
}

function expectAttrs(input, want, tag) {
  for (const a of ATTRS) {
    assert.equal(input.getAttribute(a), want[a], `${tag} ${a}`);
  }
}

function runSequence(seq, tag) {
  store.clear();
  const { input, txt } = makeFixture();
  // 首帧：原文进缓存
  applyLang("zh-CN");
  expectAttrs(input, { placeholder: "搜索", title: "语言与地区", "aria-label": "关闭" }, `${tag} init`);
  assert.equal(txt.nodeValue, "资产", `${tag} init text`);

  for (const lang of seq) {
    setLang(lang);
    assert.equal(getLang(), lang, `${tag} getLang ${lang}`);
    assert.equal(document.documentElement.lang, lang === "ko" ? "ko" : lang, `${tag} html.lang ${lang}`);
    expectAttrs(input, {
      placeholder: tr("搜索", lang),
      title: tr("语言与地区", lang),
      "aria-label": tr("关闭", lang),
    }, `${tag} ${lang}`);
    assert.equal(txt.nodeValue, tr("资产", lang), `${tag} text ${lang}`);
  }
}

// 评审复现：简中 → 韩 → 英，属性不得卡在韩语
runSequence(["ko", "en"], "zh→ko→en");
assert.equal(tr("搜索", "en"), "Search");
assert.equal(tr("搜索", "ko"), "검색");

runSequence(["zh-CN", "en", "ko", "zh-CN"], "zh→en→ko→zh");
runSequence(["zh-CN", "ko", "en", "zh-TW"], "zh→ko→en→tw");

// React 真正改原文：占位从「搜索」改为「搜索币种」，缓存应跟随新原文
{
  store.clear();
  const { input } = makeFixture();
  setLang("ko");
  assert.equal(input.getAttribute("placeholder"), tr("搜索", "ko"));
  input.setAttribute("placeholder", "搜索币种");
  applyLang("ko");
  assert.equal(input.getAttribute("placeholder"), tr("搜索币种", "ko"), "react rewrite ko");
  setLang("en");
  assert.equal(input.getAttribute("placeholder"), tr("搜索币种", "en"), "react rewrite then en");
}

console.log("✓ i18n 属性原文缓存跨语言切换检查通过");
