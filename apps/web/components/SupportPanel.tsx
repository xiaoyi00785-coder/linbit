"use client";
import { useEffect, useRef, useState } from "react";
import { supportChannels } from "@/lib/support";
import { socialLinks } from "@/lib/social";
import { listAnnouncements, type Announcement } from "@/lib/services";
import { isOn } from "@/lib/features";

const HELP = "/p/" + encodeURIComponent("帮助中心");
const FAQ: [string, string][] = [["账户与安全", "忘记密码、绑定谷歌验证器、修改邮箱/手机"], ["充值与提现", "充值未到账、提现到账时间、网络选择"], ["交易", "手续费说明、账户间划转"]];

/** 右下角客服入口 + 面板。遮罩用 .xmask：Esc 关闭、焦点陷阱、背景滚动锁定由 OverlayManager 统一处理。 */
export function SupportPanel() {
  const [o, setO] = useState(false);
  const [ann, setAnn] = useState<Announcement[] | null>(null);
  const btn = useRef<HTMLButtonElement>(null);
  useEffect(() => { const h = () => setO(true); window.addEventListener("lb-support-open", h); return () => window.removeEventListener("lb-support-open", h); }, []);
  useEffect(() => {
    if (!o) return;
    const k = (e: KeyboardEvent) => { if (e.key === "Escape") setO(false); };
    window.addEventListener("keydown", k);
    if (ann === null) listAnnouncements().then((r) => setAnn(r.ok ? (r.data ?? []).slice(0, 3) : [])).catch(() => setAnn([]));
    return () => window.removeEventListener("keydown", k);
  }, [o, ann]);
  const close = () => { setO(false); setTimeout(() => btn.current?.focus({ preventScroll: true }), 0); };
  const ch = supportChannels();
  const community = socialLinks().filter(([k]) => k === "telegram" || k === "discord");
  return <>
    <button ref={btn} className="svc" onClick={() => setO((v) => !v)} title="在线客服" aria-label="在线客服" aria-haspopup="dialog" aria-expanded={o} data-testid="svc-btn">
      <svg width="22" height="22" viewBox="0 0 24 24" fill="none" style={{ stroke: "var(--lb-on-primary)" }} strokeWidth="2"><path d="M4 13v-1a8 8 0 0116 0v1" /><rect x="3" y="13" width="4" height="6" rx="1.5" /><rect x="17" y="13" width="4" height="6" rx="1.5" /><path d="M19 19c0 2-2 3-5 3" /></svg>
    </button>
    {o && <div className="xmask svc-mask" onClick={(e) => { if (e.target === e.currentTarget) close(); }}>
      <div className="svcp2" role="dialog" aria-modal="true" aria-labelledby="svc-t" data-testid="svc-panel">
        <div className="svc-hd"><b id="svc-t">客服与帮助</b><button className="fx-x" aria-label="关闭" onClick={close}>×</button></div>
        <div className="svc-bd">
          <section>
            <h4>联系客服</h4>
            {ch.length ? <div className="svc-ch">{ch.map((c) => <a key={c.key} href={c.href} target={c.key === "email" ? undefined : "_blank"} rel="noopener noreferrer" data-testid={"svc-ch-" + c.key}><span>{c.label}</span><em>{c.hint}</em></a>)}</div>
              : <p className="svc-note" data-testid="svc-none">在线客服暂未开通。请先查看下方帮助中心和常见问题，联系方式开通后会在这里显示。</p>}
          </section>
          <section>
            <h4>常见问题</h4>
            <ul className="svc-faq">{FAQ.map(([h, p]) => <li key={h}><a href={HELP}><span>{h}</span><em>{p}</em></a></li>)}</ul>
            <a className="svc-more" href={HELP}>进入帮助中心 →</a>
          </section>
          {ann && ann.length > 0 && <section>
            <h4>最新公告</h4>
            <ul className="svc-faq">{ann.map((a) => <li key={a.id}><a href={`/announcements?id=${encodeURIComponent(a.id)}`}><span>{a.title}</span><em>{a.date}</em></a></li>)}</ul>
            <a className="svc-more" href="/announcements">全部公告 →</a>
          </section>}
          {(isOn("chatRoom") || community.length > 0) && <section>
            <h4>社区交流</h4>
            <p className="svc-note">与其他交易者交流，不是官方客服，请勿在此透露账户信息。</p>
            <div className="svc-ch">
              {isOn("chatRoom") && <a href="/chat"><span>交易者聊天室</span><em>站内</em></a>}
              {community.map(([k, n, u]) => <a key={k} href={u} target="_blank" rel="noopener noreferrer"><span>{n} 社区</span><em>{u.replace(/^https?:\/\//, "")}</em></a>)}
            </div>
          </section>}
        </div>
      </div>
    </div>}
  </>;
}
