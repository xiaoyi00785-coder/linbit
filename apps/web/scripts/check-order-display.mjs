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
