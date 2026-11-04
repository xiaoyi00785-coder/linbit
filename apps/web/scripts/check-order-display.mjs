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
assert.equal(fmtFeeExact("-0.00911000"), "-0.00911000");
assert.equal(fmtFeeExact("-0.03540000"), "-0.03540000");
console.log("pnl-exact ok");
// network picker display: labels / icons / fallback / min amount
import { netLabel, netIcon, netInfosOf, positiveAmount, withdrawMin, DEP_NETS_FALLBACK } from "../lib/assetDisplay.ts";
assert.equal(netLabel("TRC20"), "Tron（TRC20）");
assert.equal(netLabel("BEP20"), "BNB智能链（BEP20）");
assert.equal(netLabel("ERC20"), "以太坊（ERC20）");
assert.equal(netLabel("ARBITRUM"), "Arbitrum One（ARB）");
assert.equal(netLabel("Arbitrum One"), "Arbitrum One（ARB）");
assert.equal(netLabel("ARB"), "Arbitrum One（ARB）");
for (const [a, b] of [["SOL", "Solana"], ["ARBITRUM", "Arbitrum One"], ["BEP20", "BEP20(BSC)"], ["BEP20", "BSC"], ["TRC20", "TRX"], ["ERC20", "ETH"], ["AVAXC", "AVAX-C"], ["POLYGON", "matic"]]) {
  assert.equal(netLabel(a), netLabel(b), `${a} vs ${b}`);
  assert.equal(netIcon(a), netIcon(b), `icon ${a} vs ${b}`);
}
assert.equal(netLabel("UNKNOWNNET"), "UNKNOWNNET");
assert.equal(netLabel(""), "");
assert.equal(netIcon("UNKNOWNNET"), null);
assert.deepEqual(DEP_NETS_FALLBACK.USDT, ["TRC20", "ERC20", "BEP20", "SOL"]);
assert.deepEqual(netInfosOf("USDT", null).map((x) => x.network), ["TRC20", "ERC20", "BEP20", "SOL"]);
assert.equal(netInfosOf("USDT", null).some((x) => x.min || x.eta), false);
for (const v of ["0", "0.00000000", "00.0", "", "abc", "-1", "1e-7", undefined, null, 0]) assert.equal(positiveAmount(v), undefined, String(v));
assert.equal(positiveAmount("1"), "1");
assert.equal(positiveAmount("10.00000000"), "10");
assert.equal(positiveAmount("0.00010000"), "0.0001");
assert.equal(positiveAmount("0.00000001"), "0.00000001");
assert.equal(positiveAmount("123456789012345678.12300000"), "123456789012345678.123");
const apiNets = { ok: true, source: "api", data: { networks: [
  { asset: "USDT", network: "TRC20", minDeposit: "0.00000000", minWithdraw: "0", estimatedArrivalMinutes: 0, recommended: true },
  { asset: "USDT", network: "SOL", minDeposit: "1.00000000", minWithdraw: "10.00000000", estimatedArrivalMinutes: 1 },
] } };
const dep = netInfosOf("USDT", apiNets);
assert.deepEqual(dep[0], { network: "TRC20", min: undefined, eta: undefined, recommended: true });
assert.deepEqual(dep[1], { network: "SOL", min: "1", eta: 1, recommended: undefined });
assert.equal(netInfosOf("USDT", apiNets, true)[0].min, undefined);
assert.equal(netInfosOf("USDT", apiNets, true)[1].min, "10");
// withdraw min: one value = max(api min, local floor)
assert.equal(withdrawMin("10", 12), "12");
assert.equal(withdrawMin("20", 12), "20");
assert.equal(withdrawMin("12", 12), "12");
assert.equal(withdrawMin(undefined, 12), "12");
assert.equal(withdrawMin("0.0005", undefined), "0.0005");
assert.equal(withdrawMin(undefined, undefined), undefined);
console.log("net-display ok");
// QA 批次 1（主网 5e65414）
import { fmtVolume, fmtFeeEst } from "../lib/format.ts";
import { stLabel, KNOWN_STATUSES } from "../lib/orderStatus.ts";
import { etaMinutes, withdrawTip } from "../lib/assetDisplay.ts";
// 1. 零 / 缺失成交额统一 "--"
for (const v of [0, "0", "0.00", "0.00000000", "", undefined, null, "abc", NaN, -5]) assert.equal(fmtVolume(v), "--", String(v));
assert.equal(fmtVolume("1234567.891"), "1,234,567.89");
assert.equal(fmtVolume(0.5, 4), "0.5000");
// 7. 预估手续费：最多 8 位（基础币）/ 4 位（USDT），不输出 0.00000006383
assert.equal(fmtFeeEst(0.00000006383, 8), "0.00000006");
assert.equal(fmtFeeEst(0.0000638, 8), "0.0000638");
assert.equal(fmtFeeEst(0.123456789, 8), "0.12345679");
assert.equal(fmtFeeEst(0.5, 8), "0.5");
assert.equal(fmtFeeEst(0.00000000383, 8), "< 0.00000001");
assert.equal(fmtFeeEst(0.00123456, 4), "0.0012");
assert.equal(fmtFeeEst(0.00001, 4), "< 0.0001");
assert.equal(fmtFeeEst(0, 8), "--");
assert.equal(fmtFeeEst(undefined, 8), "--");
assert.doesNotMatch(fmtFeeEst(1e-7, 8), /e/i);
// 6. 状态：已知枚举都有中文，done = 已完成；上下文覆盖
assert.equal(stLabel("done"), "已完成");
assert.equal(stLabel("DONE"), "已完成");
assert.equal(stLabel("completed"), "已完成");
assert.equal(stLabel("pending", "deposit"), "确认中");
assert.equal(stLabel("pending", "withdraw"), "审核中");
assert.equal(stLabel("pending"), "待处理");
assert.equal(stLabel(undefined), "--");
assert.equal(stLabel("brand_new_enum"), "brand_new_enum");
for (const k of KNOWN_STATUSES) assert.match(stLabel(k), /[\u4e00-\u9fa5]/, k + " has a Chinese label");
for (const k of ["new", "filled", "canceled", "rejected", "failed", "completed", "done", "processing", "pending", "success", "settled", "running", "stopped"]) assert.ok(KNOWN_STATUSES.includes(k), k);
// 5. 提现网络下拉也带预计到账（后端 estimatedArrivalMinutes），缺失 / 0 不编造；提示随币种 / 网络变化
assert.equal(etaMinutes(30), 30);
for (const v of [0, -1, NaN, undefined, null, "5"]) assert.equal(etaMinutes(v), undefined, String(v));
const wdNets = { ok: true, source: "api", data: { networks: [
  { asset: "BTC", network: "BTC", minWithdraw: "0.001", withdrawEnabled: true, estimatedArrivalMinutes: 30 },
  { asset: "BTC", network: "LN", minWithdraw: "0.0001", withdrawEnabled: true },
] } };
const wd = netInfosOf("BTC", wdNets, true);
assert.equal(wd[0].eta, 30);
assert.equal(wd[0].min, "0.001");
assert.equal(wd[1].eta, undefined);
assert.equal(withdrawTip("USDT", "TRC20", "12", undefined), "当前选择 USDT · Tron（TRC20）：单笔最小提现 12 USDT。");
assert.equal(withdrawTip("BTC", "BTC", "0.001", 30), "当前选择 BTC · 比特币（BTC）：单笔最小提现 0.001 BTC，预计到账约 30 分钟。");
assert.equal(withdrawTip("ETH", "ERC20", undefined, undefined), undefined);
assert.doesNotMatch(withdrawTip("BTC", "BTC", "0.001", undefined), /USDT|12/);
console.log("qa-batch1 ok");
