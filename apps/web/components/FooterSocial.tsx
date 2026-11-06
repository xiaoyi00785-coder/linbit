"use client";
import { useEffect, useRef, useState } from "react";
import { BRAND_PATHS } from "@/lib/brandIcons";
import { MORE_SOCIAL, PRIMARY_SOCIAL, socialItem, useSocialLinks } from "@/lib/social";
import { toast } from "@/lib/ui";

const Icon = ({ k }: { k: string }) => <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true" fill="currentColor"><path d={BRAND_PATHS[k]} /></svg>;
const soon = (name: string) => toast(`${name} 社区即将开放`, "info");

function Item({ k, row }: { k: string; row?: boolean }) {
  const it = socialItem(k);
  const label = it.url ? it.name : `${it.name}（即将开放）`;
  const body = <><Icon k={k} />{row && <span>{it.name}</span>}</>;
  return it.url
    ? <a className={row ? "fsoc-row" : "fsoc-i"} href={it.url} target="_blank" rel="noopener noreferrer" aria-label={label} title={it.name} role={row ? "menuitem" : undefined}>{body}</a>
    : <button type="button" className={row ? "fsoc-row" : "fsoc-i"} aria-label={label} title={it.name} role={row ? "menuitem" : undefined} onClick={() => soon(it.name)}>{body}</button>;
}

/** 页脚「社区」：两行圆形图标，始终展示；读取公开配置（构建期环境变量兜底）的新标签页打开，未配置的点击提示即将开放 */
export function FooterSocial() {
  useSocialLinks();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const d = (e: MouseEvent) => { if (!ref.current?.contains(e.target as Node)) setOpen(false); };
    const k = (e: KeyboardEvent) => { if (e.key === "Escape") { setOpen(false); ref.current?.querySelector<HTMLButtonElement>(".fsoc-more")?.focus(); } };
    document.addEventListener("mousedown", d); document.addEventListener("keydown", k);
    ref.current?.querySelector<HTMLElement>(".fsoc-pop .fsoc-row")?.focus();
    return () => { document.removeEventListener("mousedown", d); document.removeEventListener("keydown", k); };
  }, [open]);
  return (
    <div className="fsoc" ref={ref} data-testid="footer-social">
      {PRIMARY_SOCIAL.map((k) => <Item key={k} k={k} />)}
      <button type="button" className="fsoc-i fsoc-more" aria-label="更多社区" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(!open)}>
        <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true" fill="currentColor"><circle cx="5" cy="12" r="2" /><circle cx="12" cy="12" r="2" /><circle cx="19" cy="12" r="2" /></svg>
      </button>
      {open && <div className="fsoc-pop" role="menu" aria-label="更多社区">{MORE_SOCIAL.map((k) => <Item key={k} k={k} row />)}</div>}
    </div>
  );
}
