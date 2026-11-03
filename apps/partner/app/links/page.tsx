import { ME } from "@/lib/rules";
export default function Links() {
  return (
    <>
      <h1>推广链接</h1>
      <div className="card"><small>合伙人邀请码</small><b>{ME.invite}</b><p className="muted">linbit.example.com/r/{ME.invite}</p></div>
      <p className="muted">通过合伙人链接注册的用户终身绑定，不再享受普通邀请返佣（不叠加）。</p>
    </>
  );
}
