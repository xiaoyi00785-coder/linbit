"use client";
import { useRef, useState } from "react";
import { Shell, Side, ACC } from "./layout";
import { submitKyc, uploadKycDocument, type KycSubmission } from "@/lib/services";
import { Src } from "./common";

type UpState = { file?: File; preview?: string; status: "idle" | "uploading" | "done" | "error"; fileId?: string; msg?: string };

/** KYC 证件上传组件：本地校验（JPG/PNG、≤10MB）+ 预览 + 上传状态。上传接口待定，当前走 mock。 */
export function KycUpload({ label, side, onDone }: { label: string; side: "front" | "selfie"; onDone: (id: string) => void }) {
  const ref = useRef<HTMLInputElement>(null);
  const [s, setS] = useState<UpState>({ status: "idle" });
  const pick = async (file?: File) => {
    if (!file) return;
    if (!/image\/(jpeg|png)/.test(file.type)) return setS({ status: "error", msg: "仅支持 JPG / PNG" });
    if (file.size > 10 * 1024 * 1024) return setS({ status: "error", msg: "文件不能超过 10MB" });
    const preview = URL.createObjectURL(file);
    setS({ file, preview, status: "uploading" });
    const r = await uploadKycDocument(file, side);
    if (r.ok && r.data) { setS({ file, preview, status: "done", fileId: r.data.fileId }); onDone(r.data.fileId); }
    else setS({ file, preview, status: "error", msg: r.error ?? "上传失败" });
  };
  return (
    <label className="fx-field">
      {label}
      <div className="fx-upl" onClick={() => ref.current?.click()} onDragOver={(e) => e.preventDefault()} onDrop={(e) => { e.preventDefault(); pick(e.dataTransfer.files[0]); }}>
        {s.preview ? <img src={s.preview} alt="" /> : <><span className="upi">▭</span><a className="lime2">点击或拖拽上传 ⤒</a><span>JPG / PNG，≤ 10MB</span></>}
        {s.status !== "idle" && <div className="st">{s.status === "uploading" ? "上传中…" : s.status === "done" ? "✓ 已上传（Mock）" : s.msg}</div>}
      </div>
      <input ref={ref} type="file" accept="image/jpeg,image/png" hidden onChange={(e) => pick(e.target.files?.[0])} />
    </label>
  );
}

export function KycBasic() {
  const [f, setF] = useState<KycSubmission>({ firstName: "", lastName: "", country: "", idType: "id_card", idNumber: "", dateOfBirth: "" });
  const [docs, setDocs] = useState<{ front?: string; selfie?: string }>({});
  const [msg, setMsg] = useState("");
  const [src, setSrc] = useState<"api" | "mock">();
  const set = (k: keyof KycSubmission) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => setF({ ...f, [k]: e.target.value });
  const ready = f.firstName && f.lastName && f.country && f.idNumber && f.dateOfBirth && docs.front && docs.selfie;
  return (
    <Shell side={<Side items={ACC} />}>
      <div className="lcard">
        <h3>基础认证 <Src s={src} /></h3>
        <div className="lg">1. 请上传清晰、完整、未遮挡的有效证件照片<br />2. 单个文件不超过 10MB，支持 JPG / PNG</div>
        <div className="fx-grid2">
          <label className="fx-field">国家或地区<input placeholder="请输入国家或地区" value={f.country} onChange={set("country")} /></label>
          <label className="fx-field">证件类型
            <select value={f.idType} onChange={set("idType")}><option value="id_card">身份证</option><option value="passport">护照</option><option value="driver_license">驾照</option></select>
          </label>
          <label className="fx-field">姓<input placeholder="请输入姓氏" value={f.lastName} onChange={set("lastName")} /></label>
          <label className="fx-field">名<input placeholder="请输入名字" value={f.firstName} onChange={set("firstName")} /></label>
          <label className="fx-field">出生日期<input type="date" value={f.dateOfBirth} onChange={set("dateOfBirth")} /></label>
          <label className="fx-field">证件号码<input placeholder="请输入证件号码" value={f.idNumber} onChange={set("idNumber")} /></label>
          <KycUpload label="上传证件正面" side="front" onDone={(id) => setDocs((d) => ({ ...d, front: id }))} />
          <KycUpload label="上传手持证件照片" side="selfie" onDone={(id) => setDocs((d) => ({ ...d, selfie: id }))} />
        </div>
        {msg && <div className="fx-ok">{msg}</div>}
        <button className="lb lime wide" disabled={!ready} style={{ opacity: ready ? 1 : 0.5 }} onClick={async () => {
          const r = await submitKyc(f);
          setSrc(r.source);
          setMsg(r.ok ? "✓ 已提交，审核结果将通过消息通知告知" : r.error ?? "提交失败");
        }}>提交</button>
      </div>
    </Shell>
  );
}
