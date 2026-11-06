import { useEffect, useState } from 'react'
import { ApiState, Confirm } from '../ui.jsx'
import { api, useApi } from '../api.js'

const CHANNELS = [
  ['x', 'X'], ['telegram', 'Telegram'], ['facebook', 'Facebook'], ['discord', 'Discord'],
  ['instagram', 'Instagram'], ['youtube', 'YouTube'], ['tiktok', 'TikTok'], ['reddit', 'Reddit'], ['medium', 'Medium'],
]
const EMPTY = Object.fromEntries(CHANNELS.map(([key]) => [key, '']))

export function SocialSettings() {
  const query = useApi('site/social')
  const [draft, setDraft] = useState(EMPTY)
  const [confirm, setConfirm] = useState(false)
  const [clientError, setClientError] = useState('')
  useEffect(() => { if (query.data?.links) setDraft({ ...EMPTY, ...query.data.links }) }, [query.data])
  const changed = CHANNELS.filter(([key]) => (draft[key] || '') !== ((query.data?.links || EMPTY)[key] || ''))
  const set = (key, value) => { setDraft(previous => ({ ...previous, [key]: value })); setClientError('') }
  const save = () => {
    try {
      for (const [key] of changed) {
        const value = draft[key].trim()
        if (value) {
          const url = new URL(value)
          if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) throw new Error()
        }
      }
      setConfirm(true)
    } catch { setClientError('请输入合法的 http(s) 绝对 URL，且不要包含用户名或密码；清空字段可移除链接。') }
  }
  return <div className="card pad" style={{ marginTop: 12 }}>
    <div className="sec" style={{ marginTop: 0 }}>社区链接</div>
    <p className="muted">配置 X / Telegram / Facebook / Discord / Instagram / YouTube / TikTok / Reddit / Medium。空字符串清除；未配置渠道继续显示「即将开放」。公开页面仅显示合法 HTTPS 链接。</p>
    <ApiState q={query}>{CHANNELS.map(([key, label]) => <div className="fr" key={key}>
      <label htmlFor={`social-${key}`}>{label}</label>
      <input id={`social-${key}`} className="in" type="url" inputMode="url" placeholder="https://…（留空为未配置）" maxLength={2048} value={draft[key] || ''} onChange={event => set(key, event.target.value)} />
    </div>)}</ApiState>
    {clientError && <p role="alert" style={{ color: 'var(--red, #e66)' }}>{clientError}</p>}
    <div style={{ textAlign: 'right' }}><button className="btn" disabled={!changed.length} onClick={() => { setDraft({ ...EMPTY, ...(query.data?.links || {}) }); setClientError('') }}>还原</button>{' '}
      <button className="btn p" disabled={!changed.length} onClick={save}>保存</button></div>
    {confirm && <Confirm title="保存 · 社区链接" rows={changed.map(([key, label]) => [label, `${(query.data?.links || EMPTY)[key] || '（未配置）'} → ${draft[key].trim() || '（清除）'}`])} onClose={() => setConfirm(false)}
      onOk={async ({ totpCode, reason }) => {
        const links = Object.fromEntries(changed.map(([key]) => [key, draft[key].trim()]))
        await api('site/social', { method: 'PUT', body: { links, totpCode, reason } })
        query.reload()
        return '已保存；公开页面短缓存刷新后生效'
      }} />}
  </div>
}
