"use client";
/** 通用交互件：全局弹窗宿主、下拉、日期区间、刷新按钮。legacy 静态标记通过 openM() 打开弹窗。 */
import { openTransfer } from "@/components/Transfer";
import { useEffect, useRef, useState } from "react";
import { QRCodeSVG } from "qrcode.react";
import { toast, copyText } from "@/lib/ui";
import { rest, rget } from "@/lib/rest";
import { useAuthStore } from "@/lib/store";
const authed = () => !!useAuthStore.getState().accessToken || (typeof localStorage !== "undefined" && !!localStorage.getItem("lb_auth"));

type MP = Record<string, any>;
/** transfer 统一走全局划转弹窗（components/Transfer + lib/transfer、lib/wallet） */
export const openM = (name: string, props: MP = {}) => name === "transfer" ? openTransfer(props.from, props.to) : window.dispatchEvent(new CustomEvent("lb-modal", { detail: { name, props } }));

function Box({ title, onClose, children, foot }: { title: string; onClose: () => void; children: React.ReactNode; foot?: React.ReactNode }) {
  useEffect(() => { const k = (e: KeyboardEvent) => e.key === "Escape" && onClose(); window.addEventListener("keydown", k); return () => window.removeEventListener("keydown", k); }, [onClose]);
  return <div className="fx-modal" onClick={onClose}><div className="tk-modal" role="dialog" aria-label={title} onClick={(e) => e.stopPropagation()}>
    <div className="tk-mh"><b>{title}</b><span className="tk-x" onClick={onClose}>✕</span></div>{children}{foot && <div className="tk-mf">{foot}</div>}</div></div>;
}

const DL: Record<string, string> = { apple: "App Store", beta: "测试版 (TestFlight)", gplay: "Google Play", apk: "Android APK" };
function Download({ kind = "apple", close }: MP) {
  const [k, setK] = useState(kind);
  const url = (typeof location !== "undefined" ? location.origin : "") + "/download?ch=" + k;
  return <Box title="下载 linbit App" onClose={close}>
    <div className="tk-sel">{Object.entries(DL).map(([id, t]) => <button key={id} className={"tk-chip" + (k === id ? " long" : "")} onClick={() => setK(id)}>{t}</button>)}</div>
    <div className="kit-qr"><QRCodeSVG value={url} size={160} marginSize={2} /></div>
    <p className="tk-txt" style={{ textAlign: "center" }}>使用手机扫码下载「{DL[k]}」版本</p>
    <div className="tk-mf"><button className="tk-b2" onClick={() => copyText(url, "下载链接已复制")}>复制链接</button><button className="tk-b1" onClick={close}>完成</button></div>
  </Box>;
}

const SEC: Record<string, [string, string[]]> = {
  password: ["修改登录密码", ["当前密码", "新密码", "确认新密码"]], email: ["更换邮箱", ["新邮箱", "邮箱验证码"]], phone: ["设置手机号", ["手机号", "短信验证码"]],
  totp: ["设置谷歌验证", ["谷歌验证码"]], fund: ["设置资金密码", ["资金密码", "确认资金密码"]], passkey: ["管理通行密钥", []], anti: ["设置防钓鱼码", ["防钓鱼码"]], other: ["安全设置", []],
};
function Security({ kind = "other", label, close }: MP) {
  const [t, fields] = SEC[kind] || SEC.other; const [v, setV] = useState<Record<string, string>>({});
  const [bind, setBind] = useState<{ secret: string; otpauthUri: string } | null>(null); const [be, setBe] = useState(""); const [busy, setBusy] = useState(false);
  useEffect(() => { if (kind !== "totp") return; if (!authed()) { close(); location.href = "/login"; return; }
    rest<any>("POST", "/2fa/totp/bind").then((r) => r.ok && r.data?.otpauthUri ? setBind(r.data) : setBe(r.status === 401 ? "请先登录" : (r as any).error || "获取绑定密钥失败")); }, [kind]);
  const ok = async () => { const miss = fields.find((x) => !v[x]); if (miss) return toast("请填写" + miss, "err"); if (fields[1]?.startsWith("确认") && v[fields[0]] !== v[fields[1]]) return toast("两次输入不一致", "err");
    if (kind === "totp") { if (!bind) return toast(be || "绑定密钥未就绪", "err"); const code = (v[fields[0]] || "").trim(); if (!/^\d{6}$/.test(code)) return toast("请输入 6 位验证码", "err");
      setBusy(true); const r = await rest<any>("POST", "/2fa/totp/verify", { code }); setBusy(false); if (r.ok) { toast("谷歌验证已开启"); close(); } else toast(r.status === 400 ? "验证码错误或已过期" : (r as any).error || "验证失败", "err"); return; }
    toast("该安全设置接口暂未开放，请稍后再试", "info"); close(); };
  return <Box title={label || t} onClose={close} foot={<><button className="tk-b2" onClick={close}>取消</button><button className="tk-b1" disabled={busy} onClick={ok}>{busy ? "提交中…" : "确认"}</button></>}>
    {kind === "totp" && (bind ? <><div className="kit-qr"><QRCodeSVG value={bind.otpauthUri} size={140} marginSize={2} /></div><p className="tk-txt">无法扫码？手动输入密钥：<b>{bind.secret}</b> <a role="button" onClick={() => copyText(bind.secret)}>复制</a></p></> : <p className="tk-txt">{be || "正在获取绑定密钥…"}</p>)}
    {kind === "passkey" && <p className="tk-txt">尚未添加通行密钥。通行密钥使用设备生物识别登录，更安全便捷。</p>}
    {fields.map((x) => <div key={x}><div className="tk-lbl">{x}</div><input className="kit-in" type={/密码/.test(x) ? "password" : "text"} value={v[x] || ""} onChange={(e) => setV({ ...v, [x]: e.target.value })} placeholder={"请输入" + x} /></div>)}
  </Box>;
}


const GERR: Record<string, string> = { INVALID_RANGE: "价格区间无效", INVALID_GRID_COUNT: "网格数量无效", INVESTMENT_TOO_LOW: "投资额过低", GRID_TOO_DENSE: "网格过密", INSUFFICIENT_BALANCE: "余额不足", PRICE_OUT_OF_RANGE: "当前价格不在区间内", TOO_MANY_BOTS: "运行中的策略过多", NO_PRICE: "暂无价格" };
function Grid({ symbol = "BTCUSDT", close }: MP) {
  const [v, setV] = useState({ lowerPrice: "70000", upperPrice: "85000", gridCount: "20", investment: "100" }); const [pv, setPv] = useState<any>(null); const [busy, setBusy] = useState(false);
  const authed = typeof localStorage !== "undefined" && !!localStorage.getItem("lb_auth");
  useEffect(() => { const t = setTimeout(async () => { const q = new URLSearchParams({ symbol, mode: "arithmetic", ...v }).toString(); const r = await rest<any>("GET", "/strategy/grid/preview?" + q); setPv(r.ok ? r.data : { err: GERR[(r as any).code] || r.error }); }, 400); return () => clearTimeout(t); }, [v, symbol]);
  const go = async () => { if (!authed) { close(); location.href = "/login"; return; } setBusy(true); const r = await rest<any>("POST", "/strategy/grid", { symbol, mode: "arithmetic", ...v }); setBusy(false); if (r.ok) { toast("网格策略已创建"); close(); } else toast(GERR[(r as any).code] || r.error || "创建失败", "err"); };
  return <Box title={"复制策略 · 现货网格 " + symbol.replace("USDT", "/USDT")} onClose={close} foot={<><button className="tk-b2" onClick={close}>取消</button><button className="tk-b1" disabled={busy} onClick={go}>{authed ? (busy ? "创建中…" : "创建") : "登录后创建"}</button></>}>
    {([["lowerPrice", "价格下限"], ["upperPrice", "价格上限"], ["gridCount", "网格数量"], ["investment", "投资额 (USDT)"]] as const).map(([k, t]) => <div key={k}><div className="tk-lbl">{t}</div><input className="kit-in" inputMode="decimal" value={(v as any)[k]} onChange={(e) => setV({ ...v, [k]: e.target.value.replace(/[^\d.]/g, "") })} /></div>)}
    <p className="tk-txt">{pv?.err ? "预览：" + pv.err : pv ? "预览：每格利润 " + (pv.profitPerGridPct ?? pv.profitPerGrid ?? "--") + (pv.profitPerGridPct ? "%" : "") : "预览计算中…"}</p>
  </Box>;
}
function MyBots({ close }: MP) {
  const [l, setL] = useState<any[] | null>(null); const [e, setE] = useState("");
  useEffect(() => { rest<any>("GET", "/strategy/grid?status=all").then((r) => r.ok ? setL(Array.isArray(r.data) ? r.data : r.data?.items || []) : (setE(r.status === 401 ? "请先登录查看我的策略" : r.error || "加载失败"), setL([]))); }, []);
  return <Box title="我的策略" onClose={close}>{l === null ? <p className="tk-txt">加载中…</p> : l.length ? l.map((b: any) => <div key={b.id} className="lrow pd"><span>{b.symbol} · {b.status}</span><b>{b.investment} USDT</b></div>) : <p className="tk-txt">{e || "暂无运行中的策略"}</p>}</Box>;
}
const MODALS: Record<string, (p: MP) => JSX.Element> = { download: Download, security: Security, grid: Grid, mybots: MyBots };
export function ModalHost() {
  const [m, setM] = useState<{ name: string; props: MP } | null>(null);
  useEffect(() => { const h = (e: any) => setM(e.detail); window.addEventListener("lb-modal", h); return () => window.removeEventListener("lb-modal", h); }, []);
  if (!m || !MODALS[m.name]) return null;
  const C = MODALS[m.name]; return <C {...m.props} close={() => setM(null)} />;
}

const DEF: [RegExp, string[]][] = [[/限价 \| 市价/, ["限价 | 市价", "限价", "市价"]], [/最近 7 天/, ["最近 7 天", "最近 30 天", "最近 90 天"]], [/链上充值/, ["链上充值", "内部转账"]], [/链上提现/, ["链上提现", "内部转账"]], [/^活期$/, ["活期", "定期"]], [/^CNY$|^USD$/, ["USD", "CNY", "EUR", "JPY", "KRW", "HKD"]], [/USDT/, ["USDT", "BTC", "ETH"]]];
const defOpts = (v: string, k?: string) => (DEF.find(([r]) => r.test(v))?.[1]) || (k && /币|资产|合约|交易对|品种/.test(k) ? [v, "BTC", "ETH", "USDT"] : k && /状态/.test(k) ? [v, "处理中", "已完成", "已失败"] : [v, "买入", "卖出"]);
/** 下拉选择：点击展开，选中后回写并派发 lb-filter 事件。 */
export function Dropdown({ k, v, opts, onChange, cls = "lsel", store }: { k?: string; v: string; opts?: string[]; onChange?: (x: string) => void; cls?: string; store?: string }) {
  const [val, setVal] = useState(() => (store && typeof localStorage !== "undefined" && localStorage.getItem(store)) || v); const [o, setO] = useState(false); const r = useRef<HTMLDivElement>(null);
  useEffect(() => setVal((store && localStorage.getItem(store)) || v), [v, store]);
  useEffect(() => { if (!o) return; const h = (e: MouseEvent) => !r.current?.contains(e.target as Node) && setO(false); document.addEventListener("mousedown", h); return () => document.removeEventListener("mousedown", h); }, [o]);
  const list = opts || defOpts(v, k);
  return <div ref={r} className={cls + " kit-dd"} role="combobox" aria-expanded={o} onClick={() => setO(!o)}>{val} <em>▾</em>
    {o && <div className="kit-menu" onClick={(e) => e.stopPropagation()}>{list.map((x) => <div key={x} className={"kit-it" + (x === val ? " on" : "")} onClick={() => { setVal(x); setO(false); store && localStorage.setItem(store, x); onChange?.(x); window.dispatchEvent(new CustomEvent("lb-filter", { detail: { k, v: x } })); }}>{x}</div>)}</div>}
  </div>;
}
const iso = (d: Date) => d.toISOString().slice(0, 10);
/** 日期区间：点击展开两个日期输入 + 快捷范围。 */
export function DateRange({ preset }: { preset?: boolean }) {
  const now = new Date(); const [a, setA] = useState(preset ? iso(new Date(+now - 6 * 864e5)) : ""); const [b, setB] = useState(preset ? iso(now) : ""); const [o, setO] = useState(false); const r = useRef<HTMLDivElement>(null);
  useEffect(() => { if (!o) return; const h = (e: MouseEvent) => !r.current?.contains(e.target as Node) && setO(false); document.addEventListener("mousedown", h); return () => document.removeEventListener("mousedown", h); }, [o]);
  const q = (n: number) => { setA(iso(new Date(+now - (n - 1) * 864e5))); setB(iso(now)); };
  return <div ref={r} className="lsel date kit-dd" onClick={() => setO(!o)}>{a ? <span>{a}</span> : <span className="lg">开始日期</span>}<span className="lg">→</span>{b ? <span>{b}</span> : <span className="lg">结束日期</span>}<em>▦</em>
    {o && <div className="kit-menu kit-date" onClick={(e) => e.stopPropagation()}><div className="tk-sel">{[7, 30, 90].map((n) => <button key={n} className="tk-chip" onClick={() => q(n)}>近{n}天</button>)}</div>
      <input type="date" className="kit-in" value={a} max={b || undefined} onChange={(e) => setA(e.target.value)} /><input type="date" className="kit-in" value={b} min={a || undefined} onChange={(e) => setB(e.target.value)} />
      <button className="tk-chip long" onClick={() => { setO(false); window.dispatchEvent(new CustomEvent("lb-filter", { detail: { k: "date", v: [a, b] } })); toast(a && b ? `已筛选 ${a} 至 ${b}` : "已清除日期筛选", "info"); }}>确定</button></div>}
  </div>;
}
export function Refresh({ cls = "lb sm" }: { cls?: string }) {
  const [s, setS] = useState(false);
  return <button className={cls} disabled={s} onClick={() => { setS(true); window.dispatchEvent(new Event("lb-refresh")); setTimeout(() => { setS(false); toast("已刷新"); }, 500); }}>{s ? "刷新中…" : "↻ 刷新"}</button>;
}
