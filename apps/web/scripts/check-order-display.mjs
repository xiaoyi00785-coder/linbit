// node --experimental-strip-types scripts/check-order-display.mjs
import assert from "node:assert/strict";
import { liqText, orderQtyText, orderAmountText } from "../lib/orderDisplay.ts";
// acceptance r7: cross 5x short 0.0001 BTC @76676.8, ~2000 USDT equity -> exact liq 19995103.49
assert.equal(liqText("19995103.49033865", "76676.8", "short"), null);
assert.equal(liqText("84000.5", "76676.8", "short"), "84000.5");
assert.equal(liqText("0", "76676.8", "long"), null);
assert.equal(liqText("5000", "76676.8", "long"), null);
assert.equal(liqText("61000", "76676.8", "long"), "61000");
assert.equal(liqText("", "", "long"), null);
const fmt = (v) => v.toFixed(2);
const mkt = { symbol: "BTCUSDT", quantity: "0.000000", quoteOrderQty: "10.00", price: null };
assert.equal(orderQtyText(mkt), "按金额 10.00 USDT");
assert.equal(orderAmountText(mkt, fmt), "10.00");
const lim = { symbol: "BTCUSDT", quantity: "0.010000", price: "70000.00" };
assert.equal(orderQtyText(lim), "0.010000");
assert.equal(orderAmountText(lim, fmt), "700.00");
assert.equal(orderAmountText({ symbol: "BTCUSDT", quantity: "0.01", price: null }, fmt), "--");
console.log("order-display ok");
// acceptance r8
import { depositNetsOf, spotRecords, unitPrice } from "../lib/assetDisplay.ts";
import { fmtFeeExact } from "../lib/format.ts";
assert.deepEqual(depositNetsOf("BTC", null), ["BTC"]);
assert.deepEqual(depositNetsOf("BTC", { ok: true, source: "api", data: { networks: [{ asset: "BTC", network: "BTC", depositEnabled: true }] } }), ["BTC"]);
assert.deepEqual(depositNetsOf("SOL", { ok: true, source: "api", data: { networks: [{ asset: "SOL", network: "SOL", depositEnabled: false }] } }), []);
assert.ok(!depositNetsOf("BTC", null).includes("ERC20"));
assert.equal(unitPrice({ price: 78512.3 }, (v) => v.toFixed(2)), "$78512.30");
assert.equal(unitPrice({}, (v) => v.toFixed(2)), "--");
const recs = spotRecords([{ createdAt: "2026-11-04T01:00:00Z", tradeId: 1, symbol: "BTCUSDT", side: "buy", quantity: "0.1" }],
  [{ createdAt: "2026-11-04T02:00:00Z", id: 9, asset: "USDT", amount: "100", from: "spot", to: "futures" },
   { createdAt: "2026-11-04T03:00:00Z", id: 10, asset: "USDT", amount: "5", from: "funding", to: "futures" }], (v) => v);
assert.equal(recs.length, 2);
assert.deepEqual(recs[0].row, ["USDT", "划转转出（合约账户）", "-100"]);
assert.equal(fmtFeeExact("-0.00383840"), "-0.00383840");
assert.equal(fmtFeeExact("-0.00383384"), "-0.00383384");
console.log("asset-display r8 ok");
