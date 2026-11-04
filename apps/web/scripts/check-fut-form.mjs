// P0 回归：计划委托与限价的价格状态隔离 + 现货市价买按 USDT 金额下单
// 用法：BASE=http://localhost:3055 QA_EMAIL=.. QA_PASS=..（或 QA_TOKEN=<accessToken>，新设备触发验证时用） node scripts/check-fut-form.mjs（请求均被拦截，不会真实下单）
import { chromium } from "playwright";
const BASE = process.env.BASE || "http://localhost:3055";
const login = process.env.QA_TOKEN ? { accessToken: process.env.QA_TOKEN, refreshToken: process.env.QA_REFRESH || "" } : await (await fetch(BASE + "/gw/auth/login", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ email: process.env.QA_EMAIL, password: process.env.QA_PASS }) })).json();
if (!login.accessToken) { console.error("登录失败"); process.exit(1); }
const b = await chromium.launch(); const c = await b.newContext({ viewport: { width: 1440, height: 900 } });
await c.addInitScript((t) => { ["lb_ck", "lb_guide", "lb_cookie", "lb_guide_done"].forEach((k) => localStorage.setItem(k, "1")); localStorage.setItem("lb-auth", JSON.stringify({ state: { isAuthed: true, accessToken: t.accessToken, refreshToken: t.refreshToken }, version: 0 })); }, login);
const bodies = [];
await c.route(/\/gw\/(futures\/(orders|plan-orders)|spot\/orders)(\?|$)/, (r) => { if (r.request().method() !== "POST") return r.continue(); bodies.push({ url: r.request().url(), body: r.request().postDataJSON() }); r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ id: "test", status: "NEW" }) }); });
// 现货：模拟 1000 USDT 可用，避免余额校验拦截
await c.route(/\/gw\/assets\/balances/, (r) => r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ items: [{ asset: "USDT", free: "1000", locked: "0", total: "1000", valuationUsd: null }, { asset: "BTC", free: "0", locked: "0", total: "0", valuationUsd: null }] }) }));
const p = await c.newPage(); p.on('load', () => p.addStyleTag({ content: '.gmask2,.ghole,.gtip{display:none!important}' }).catch(() => {})); let fail = 0; const ok = (cond, m) => { console.log((cond ? "✓ " : "✗ ") + m); if (!cond) fail++; };
const form = (pg) => pg.locator('[aria-label="订单类型"]').locator("..");
async function confirm(pg) { await pg.waitForTimeout(400); const btn = pg.getByRole("button", { name: /^确认/ }).last(); if (await btn.isVisible().catch(() => false)) await btn.click(); await pg.waitForTimeout(600); }
await p.goto(BASE + "/futures/BTCUSDT"); await p.waitForTimeout(3000);
for (let i = 0; i < 3; i++) { const sk = p.locator(".gskip"); if (await sk.isVisible().catch(() => false)) { await sk.click(); await p.waitForTimeout(300); } }
const f = form(p);
await f.getByText("计划委托", { exact: true }).click();
const trigIn = f.locator('.inp:has(small:text-is("触发价")) input'); await trigIn.fill("50000"); await f.locator('input[placeholder="留空则市价"]').fill("51000");
await f.getByText("限价", { exact: true }).click();
const pin = f.locator('.inp:has(small:text-is("价格")) input'); await pin.fill("60000");
await f.locator('input[aria-label="数量"]').fill("0.01");
await f.getByRole("button", { name: "开多" }).click(); await confirm(p);
const lim = bodies.find((x) => /futures\/orders/.test(x.url));
ok(lim && Number(lim.body.price) === 60000 && lim.body.type === "limit", `限价单价格=60000（实际 ${lim?.body?.price}）`);
ok(lim && !("triggerPrice" in lim.body), "限价单不含计划委托触发价");
await f.getByText("计划委托", { exact: true }).click();
ok(Number(await f.locator('input[placeholder="留空则市价"]').inputValue()) === 51000, "计划委托委托价保持独立 51000");
await f.locator('input[placeholder="留空则市价"]').fill(""); await f.locator('input[aria-label="数量"]').fill("0.01");
await f.getByRole("button", { name: "开多" }).click(); await confirm(p);
const plan = bodies.find((x) => x.body && x.body.triggerPrice);
ok(plan && plan.body.type === "market" && !plan.body.price && Number(plan.body.triggerPrice) === 50000, `计划委托留空=市价（${JSON.stringify(plan?.body)}）`);
// 现货市价买
await p.goto(BASE + "/spot/BTCUSDT"); await p.waitForTimeout(3000);
for (let i = 0; i < 3; i++) { const sk = p.locator(".gskip"); if (await sk.isVisible().catch(() => false)) { await sk.click(); await p.waitForTimeout(300); } }
const s = form(p); await s.getByText("市价", { exact: true }).click();
const amt = s.locator('input[aria-label="买入金额"]').first(); await amt.fill("20");
await s.getByRole("button", { name: /买入/ }).first().click(); await confirm(p);
const sp = bodies.find((x) => /spot\/orders/.test(x.url));
ok(!/e-\d/.test(await s.innerText()), "预估手续费无科学计数法");
ok(sp && String(sp.body.quoteOrderQty) === "20" && !sp.body.qty && !sp.body.quantity, `现货市价买 quoteOrderQty=20（${JSON.stringify(sp?.body)}）`);
await b.close(); process.exit(fail ? 1 : 0);
