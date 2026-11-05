// node --experimental-strip-types scripts/check-vip-apply.mjs
import assert from "node:assert/strict";
import { vipPayload, vipApplyError, vipRequirements, vipSubmitState, VIP_CHANNEL } from "../lib/vip.ts";

// 联系方式类型 -> 后端 channel；联系方式去首尾空白
assert.deepEqual(vipPayload("邮箱", "  a@b.co "), { channel: "email", contact: "a@b.co" });
assert.deepEqual(vipPayload("手机", "+86 138 0000 0000"), { channel: "phone", contact: "+86 138 0000 0000" });
assert.deepEqual(vipPayload("Telegram", "@yourname"), { channel: "telegram", contact: "@yourname" });
assert.deepEqual(Object.values(VIP_CHANNEL).sort(), ["email", "phone", "telegram"]); // 后端 vipChannels 的子集

// 错误文案：后端 message 原样展示（不满足条件 / 已在审核 / 已是 VIP）
const nelMsg = "暂不满足 VIP 申请条件：需账户总资产 ≥ 500,000 USDT，或近 30 日交易量 ≥ 50,000,000 USDT（您当前总资产约 1,234.5 USDT，近 30 日交易量约 0 USDT）";
assert.equal(vipApplyError({ ok: false, source: "api", status: 403, code: "VIP_NOT_ELIGIBLE", error: nelMsg }), nelMsg);
assert.equal(vipApplyError({ ok: false, source: "api", status: 409, code: "APPLICATION_PENDING", error: "已有审核中的申请" }), "已有审核中的申请");
assert.equal(vipApplyError({ ok: false, source: "api", status: 409, code: "ALREADY_VIP", error: "您已是 VIP" }), "您已是 VIP");
assert.equal(vipApplyError({ ok: false, source: "api", status: 503, code: "VIP_ELIGIBILITY_UNAVAILABLE", error: "暂时无法核验您的资产与交易量，请稍后重试" }), "暂时无法核验您的资产与交易量，请稍后重试");
// 无后端 message 时按错误码兜底；网络错误提示重试
assert.equal(vipApplyError({ ok: false, source: "api", status: 403, code: "VIP_NOT_ELIGIBLE", error: "HTTP 403" }).startsWith("暂不满足 VIP 申请条件"), true);
assert.equal(vipApplyError({ ok: false, source: "api", status: 409, code: "ALREADY_VIP" }), "您已是 VIP");
assert.equal(vipApplyError({ ok: false, source: "api", status: 503 }).includes("稍后重试"), true);
assert.equal(vipApplyError({ ok: false, source: "network", error: "服务暂不可用" }), "服务暂不可用，请稍后重试");
assert.equal(vipApplyError({ ok: false, source: "api", status: 500 }), "申请提交失败，请稍后重试");

// 申请要求文案：默认公示值；跟随后端阈值
assert.deepEqual(vipRequirements(), ["账户总资产（资金 + 现货 + 合约账户）≥ 500,000 USDT", "近 30 日交易量（现货 + 合约）≥ 5,000 万 USDT"]);
assert.deepEqual(vipRequirements({ minTotalAssetsUSDT: "250000", min30dVolumeUSDT: "10000000" }), ["账户总资产（资金 + 现货 + 合约账户）≥ 250,000 USDT", "近 30 日交易量（现货 + 合约）≥ 1,000 万 USDT"]);
assert.equal(vipRequirements({ minTotalAssetsUSDT: "x", min30dVolumeUSDT: "" })[0].includes("500,000"), true);

// 按钮状态
assert.deepEqual(vipSubmitState(null, false, false), { label: "登录后提交", disabled: false });
assert.deepEqual(vipSubmitState({ status: "none" }, true, false), { label: "提交", disabled: false });
assert.deepEqual(vipSubmitState({ status: "rejected" }, true, false), { label: "提交", disabled: false }); // 被拒后可重新申请
assert.equal(vipSubmitState({ status: "pending" }, true, false).disabled, true);
assert.equal(vipSubmitState({ vip: true, status: "approved" }, true, false).label, "您已是 VIP");
assert.deepEqual(vipSubmitState(null, true, true), { label: "提交中…", disabled: true });
console.log("vip-apply ok");
