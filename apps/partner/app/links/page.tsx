"use client";
import { copy, useApi, type Links } from "@/lib/api";
import { ErrBox, Skel } from "../ui";

export default function LinksPage() {
  const r = useApi<Links>("/partner/links");
  const L = r.data;
  return (
    <>
      <h1>推广链接</h1>
      {r.loading ? <Skel n={3} h={76} /> : r.err ? <ErrBox err={r.err} reload={r.reload} /> : L && (
        <div className="links fade">
          {[["邀请码", L.code], ["邀请链接", L.url], ["注册链接", L.registerUrl]].map(([t, v]) => (
            <div key={t} className="card linkrow"><div><small>{t}</small><code>{v}</code></div><button className="btn ghost" onClick={() => copy(v)}>复制</button></div>
          ))}
        </div>
      )}
      <p className="muted">被邀请用户通过以上链接或邀请码注册后，终身绑定为您的直属用户。</p>
    </>
  );
}
