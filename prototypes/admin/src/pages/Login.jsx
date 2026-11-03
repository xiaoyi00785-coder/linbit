import { useState } from 'react'
import { api, errText, setToken } from '../api.js'

export default function Login({ onDone }) {
  const [f, setF] = useState({ username: '', password: '', totpCode: '' }); const [err, setErr] = useState(''); const [busy, setBusy] = useState(false)
  const set = k => e => setF({ ...f, [k]: k === 'totpCode' ? e.target.value.replace(/\D/g, '').slice(0, 6) : e.target.value })
  const submit = async e => {
    e.preventDefault(); setErr(''); setBusy(true)
    try { const r = await api('auth/login', { method: 'POST', body: f }); setToken(r.token); onDone() }
    catch (x) { setErr(x && (x.code === 'ADMIN_LOCKED' || x.status === 423) ? '连续失败5次，账号已锁定15分钟' : x && x.code === 'TOTP_INVALID' ? '验证码错误' : errText(x)) } finally { setBusy(false) }
  }
  return <div style={{ minHeight: '100vh', display: 'grid', placeItems: 'center' }}>
    <form className="card pad" style={{ width: 360 }} onSubmit={submit}>
      <div className="brand" style={{ marginBottom: 16 }}><i />linbit<em>Admin</em></div>
      {err && <div className="banner bad">{err}</div>}
      <div className="fr"><label>账号</label><input className="in" autoComplete="username" value={f.username} onChange={set('username')} /></div>
      <div className="fr"><label>密码</label><input className="in" type="password" autoComplete="current-password" value={f.password} onChange={set('password')} /></div>
      <div className="fr"><label>Google 验证码</label><input className="in" inputMode="numeric" placeholder="6 位动态码" value={f.totpCode} onChange={set('totpCode')} /></div>
      <button className="btn p" style={{ width: '100%', marginTop: 8 }} disabled={busy || !f.username || !f.password}>{busy ? '登录中…' : '登录'}</button>
    </form></div>
}
