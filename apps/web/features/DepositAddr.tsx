"use client";
import { netLabel, positiveAmount } from "@/lib/assetDisplay";
import { useEffect, useState } from "react";
import { QRCodeSVG } from "qrcode.react";
import { api } from "@/lib/api";
import { copyTxt } from "@/lib/ui";
import { G } from "@/components/Glyph";

type Addr = { asset: string; network: string; address: string; memo?: string | null; minDeposit?: string; confirmations?: number };

/** 充值地址：GET /wallet/deposit/address?asset&network，取不到时如实提示，不展示占位地址 */
export function DepositAddr({ asset, network }: { asset: string; network: string }) {
  const [st, setSt] = useState<{ a?: Addr; err?: string; loading: boolean }>({ loading: true });
  useEffect(() => {
    let live = true;
    setSt({ loading: true });
    api.GET("/wallet/deposit/address", { params: { query: { asset, network: network.replace(/\s*\(.*\)/, "") } } })
      .then(({ data, error, response }) => {
        if (!live) return;
        if (response.ok && data) setSt({ a: data as Addr, loading: false });
        else setSt({ loading: false, err: response.status === 401 ? "请先登录后获取充值地址" : (error as { message?: string } | undefined)?.message || "充值地址暂时无法获取，请稍后再试" });
      })
      .catch(() => live && setSt({ loading: false, err: "网络异常，充值地址暂时无法获取" }));
    return () => { live = false; };
  }, [asset, network]);
  if (st.loading) return <div className="addr"><div className="qr"><i className="sk" style={{ width: 96, height: 96 }} /></div><div><span className="lg">充值地址</span><i className="sk" style={{ width: 260 }} /></div></div>;
  if (!st.a) return <div className="addr"><div><span className="lg">充值地址</span><b className="fx-err">{st.err}</b></div></div>;
  const a = st.a;
  return (
    <div className="addr">
      <div className="qr"><QRCodeSVG bgColor="transparent" fgColor="currentColor" value={a.address} size={96} marginSize={1} /></div>
      <div>
        <span className="lg">充值地址 · {netLabel(a.network)}</span>
        <b style={{ wordBreak: "break-all" }}>{a.address} <a role="button" aria-label="复制充值地址" onClick={() => copyTxt(a.address, "充值地址已复制")}><G n="copy" s={13} /></a></b>
        {a.memo && <span className="lg">Memo / Tag：<b>{a.memo}</b></span>}
        <span className="lg">{positiveAmount(a.minDeposit) ? `最小充值 ${positiveAmount(a.minDeposit)} ${asset}` : ""}{a.confirmations ? ` · ${a.confirmations} 个区块确认后到账` : ""}</span>
      </div>
    </div>
  );
}
