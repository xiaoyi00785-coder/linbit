// node --experimental-strip-types scripts/check-live-ticks.mjs
import assert from "node:assert/strict";
import { flashDir, mergeTick, mergeWsBar, wsKline } from "../lib/tick.ts";
import { changeClass, changeDir } from "../lib/format.ts";
assert.equal(flashDir(null, 1), null);
assert.equal(flashDir(78600, 78600), null);
assert.equal(flashDir(78600, 78600.01), "up");
assert.equal(flashDir(78600, 78599.9), "down");
assert.equal(flashDir(78600, NaN), null);
const b = { time: 1, open: 100, high: 101, low: 99, close: 100, v: 0, src: "index" };
assert.deepEqual(mergeTick(b, 102), { ...b, close: 102, high: 102 });
assert.deepEqual(mergeTick(b, 98), { ...b, close: 98, low: 98 });
assert.deepEqual(mergeTick(b, 100.5), { ...b, close: 100.5 });
const w = wsKline({ symbol: "BTCUSDT", interval: "1m", time: 1767225600000, open: "67000", high: "67010.5", low: "66990", close: "67001.5", volume: "0", closed: false, source: "index" });
assert.deepEqual(w, { time: 1767225600, open: 67000, high: 67010.5, low: 66990, close: 67001.5, v: 0, src: "index" });
assert.equal(wsKline({ time: 1, close: "0" }), null);
assert.deepEqual(mergeWsBar(null, w), w);
const rest = { ...w, high: 67020, low: 66980 };
assert.deepEqual(mergeWsBar(rest, { ...w, close: 67005 }), { ...w, close: 67005, high: 67020, low: 66980 }); // same bar: keep REST extremes
const nx = { ...w, time: w.time + 60, open: 67005, high: 67006, low: 67004, close: 67006 };
assert.deepEqual(mergeWsBar(w, nx), nx); // new time => append
assert.equal(mergeWsBar(nx, w), null); // stale frame ignored
import { readFileSync } from "node:fs";
const tk = readFileSync(new URL("../features/TradeKit.tsx", import.meta.url), "utf8");
assert.match(tk, /futures-ticker:\$\{sym\}/);
assert.match(tk, /futures-kline:\$\{symbol\}:\$\{interval\}/);
assert.doesNotMatch(tk, /mark:\$\{symbol\}/, "mark-index candle hack removed");
// 静态颜色 / 箭头只由 24h 涨跌符号决定（QA：-1.8% 的币不能是绿价，红价不能配上箭头）；tick 闪烁只是瞬时背景
assert.equal(changeDir(-1.8), "down");
assert.equal(changeDir("-0.01"), "down");
assert.equal(changeDir("1.14"), "up");
assert.equal(changeDir(0), "flat");
for (const v of [undefined, null, "", "--", NaN]) assert.equal(changeDir(v), "flat", String(v));
assert.equal(changeClass(-1.79), "down");
assert.equal(changeClass(0.77), "up");
assert.equal(changeClass(undefined), "flat");
const css = readFileSync(new URL("../app/linbit.css", import.meta.url), "utf8");
assert.doesNotMatch(css, /\.lb-flash-(up|down)2?\s*[,{][^}]*\bcolor\s*:/, "tick flash must not set the resting text colour");
const fl = readFileSync(new URL("../components/Flash.tsx", import.meta.url), "utf8");
assert.match(fl, /changeClass\(chg\)/, "Flash resting colour comes from 24h change");
assert.doesNotMatch(fl, /return \(\) => clearTimeout\(t\)/, "flash timer must not be cleared by the next tick (stuck flash class)");
assert.match(tk, /changeDir\(chg\)/, "order-book mid colour + arrow share one direction");
assert.doesNotMatch(tk, /chg >= 0/, "no sign test that disagrees with the colour");
console.log("live-ticks ok");
