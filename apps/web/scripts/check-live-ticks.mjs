// node --experimental-strip-types scripts/check-live-ticks.mjs
import assert from "node:assert/strict";
import { flashDir, mergeTick } from "../lib/tick.ts";
assert.equal(flashDir(null, 1), null);
assert.equal(flashDir(78600, 78600), null);
assert.equal(flashDir(78600, 78600.01), "up");
assert.equal(flashDir(78600, 78599.9), "down");
assert.equal(flashDir(78600, NaN), null);
const b = { time: 1, open: 100, high: 101, low: 99, close: 100, v: 0, src: "index" };
assert.deepEqual(mergeTick(b, 102), { ...b, close: 102, high: 102 });
assert.deepEqual(mergeTick(b, 98), { ...b, close: 98, low: 98 });
assert.deepEqual(mergeTick(b, 100.5), { ...b, close: 100.5 });
console.log("live-ticks ok");
