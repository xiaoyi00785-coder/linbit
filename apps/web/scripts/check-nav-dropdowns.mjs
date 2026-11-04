// 头部下拉回归检查（买币/交易/合约/理财）：在 1280 与 1440 宽度下悬停每个导航项，断言下拉为
// 锚定在导航项下的紧凑卡片、条目纵向堆叠、未溢出视口左右边缘、背景 #1c1c1c；合约为左右两栏（含搜索币对 + USDT 列表）。
// 用法：BASE=http://localhost:3055 node scripts/check-nav-dropdowns.mjs [--shots DIR]   （需本地已安装 playwright）
import { chromium } from "playwright";
const BASE = process.env.BASE || "http://localhost:3000";
const si = process.argv.indexOf("--shots"); const SHOTS = si > 0 ? process.argv[si + 1] : "";
const fails = [];
const b = await chromium.launch();
for (const w of [1280, 1440]) {
  const p = await (await b.newContext({ viewport: { width: w, height: 900 } })).newPage();
  await p.goto(BASE + "/"); await p.waitForTimeout(1500);
  for (const l of ["买币", "交易", "合约", "理财"]) {
    const ni = p.locator(".hdr nav .ni", { has: p.locator(`a:text-is("${l}")`) }).first();
    await ni.hover(); await p.waitForTimeout(400);
    const m = await ni.evaluate((n) => {
      const d = n.querySelector(".dd"); if (!d) return null;
      const r = d.getBoundingClientRect(), a = n.getBoundingClientRect();
      const its = [...d.querySelectorAll(".dit")].map((x) => x.getBoundingClientRect());
      const ys = its.map((i) => Math.round(i.y));
      return { x: r.x, r: r.right, w: r.width, top: r.top, anchorBottom: a.bottom, anchorX: a.x, bg: getComputedStyle(d).backgroundColor,
        stacked: new Set(ys).size === its.length, n: its.length, txt: d.innerText,
        coins: d.querySelectorAll(".ddcoins a").length, split: !!d.querySelector(".ddsplit") };
    });
    const tag = `${w} ${l}`;
    if (!m) { fails.push(`${tag}: 下拉未出现`); continue; }
    if (m.x < 0 || m.r > w) fails.push(`${tag}: 溢出视口 x=${m.x} right=${m.r}`);
    if (m.w > (l === "合约" ? 600 : 320)) fails.push(`${tag}: 宽度 ${m.w}px，应为紧凑卡片`);
    if (m.top < m.anchorBottom - 20 || Math.abs(m.x - m.anchorX) > 300) fails.push(`${tag}: 未锚定在导航项下`);
    if (!m.stacked || m.n < 3) fails.push(`${tag}: 条目未纵向堆叠（${m.n} 项）`);
    if (m.bg !== "rgb(28, 28, 28)") fails.push(`${tag}: 背景 ${m.bg}`);
    if (/定投|100%\s*APR/.test(m.txt)) fails.push(`${tag}: 出现 定投 / 硬编码 100%APR`);
    if (l === "合约" && (!m.split || m.coins < 5 || !m.txt.includes("搜索币对"))) fails.push(`${tag}: 合约应为两栏 + 搜索币对 + USDT 列表`);
    if (SHOTS) await p.screenshot({ path: `${SHOTS}/nav-${w}-${l}.png`, clip: { x: 0, y: 0, width: w, height: 560 } });
    await p.mouse.move(5, 880); await p.waitForTimeout(400);
  }
  // 活动中心：宽面板左边缘须落在触发项范围内；鼠标从触发项缓慢移入面板过程中下拉保持打开
  {
    const l = "活动中心"; const tag = `${w} ${l}`;
    const ni = p.locator(".hdr nav .ni", { has: p.locator(`a:text-is("${l}")`) }).first();
    await ni.hover(); await p.waitForTimeout(400);
    const m = await ni.evaluate((n) => { const d = n.querySelector(".dd"); if (!d) return null; const r = d.getBoundingClientRect(), a = n.getBoundingClientRect(); return { x: r.x, r: r.right, top: r.top, ax: a.x, ar: a.right, ay: a.y + a.height / 2, acx: a.x + a.width / 2 }; });
    if (!m) fails.push(`${tag}: 下拉未出现`);
    else {
      if (m.x < 0 || m.r > w) fails.push(`${tag}: 溢出视口 x=${m.x} right=${m.r}`);
      else if (m.r <= w - 16 && (m.x < m.ax - 1 || m.x > m.ar)) fails.push(`${tag}: 面板左边缘 ${m.x} 不在触发项 [${m.ax}, ${m.ar}] 内`);
      await p.mouse.move(m.acx, m.ay);
      const tx = m.x + 40, ty = m.top + 40; let lost = false;
      for (let i = 1; i <= 12; i++) { await p.mouse.move(m.acx + (tx - m.acx) * i / 12, m.ay + (ty - m.ay) * i / 12); await p.waitForTimeout(30); if (!(await ni.evaluate((n) => !!n.querySelector(".dd")))) { lost = true; break; } }
      await p.waitForTimeout(300);
      if (lost || !(await ni.evaluate((n) => !!n.querySelector(".dd")))) fails.push(`${tag}: 鼠标移入面板时悬停丢失`);
      if (SHOTS) await p.screenshot({ path: `${SHOTS}/nav-${w}-${l}.png`, clip: { x: 0, y: 0, width: w, height: 560 } });
    }
    await p.mouse.move(5, 880); await p.waitForTimeout(400);
  }
}
await b.close();
if (fails.length) { console.error("✗ 导航下拉检查失败：\n" + fails.join("\n")); process.exit(1); }
console.log("✓ 导航下拉检查通过（1280/1440 × 买币/交易/合约/理财/活动中心）");
