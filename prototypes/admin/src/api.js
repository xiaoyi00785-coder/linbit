// 后台 API 客户端：统一 base URL、Bearer token、401 跳登录、错误信封 {code,message,requestId}
import { useCallback, useEffect, useState } from 'react'

const RAW = import.meta.env.VITE_ADMIN_API_BASE || '/admin/v1/'
export const API_BASE = RAW.endsWith('/') ? RAW : RAW + '/'
const KEY = 'lb_admin_token'

export const getToken = () => sessionStorage.getItem(KEY)
export const setToken = t => t ? sessionStorage.setItem(KEY, t) : sessionStorage.removeItem(KEY)

export class ApiError extends Error {
  constructor(status, body) { super((body && body.message) || ('HTTP ' + status)); this.status = status; this.code = body && body.code; this.requestId = body && body.requestId }
}

let onUnauthorized = () => {}
export const setUnauthorizedHandler = fn => { onUnauthorized = fn }

export async function api(path, { method = 'GET', body, query } = {}) {
  let url = API_BASE + path.replace(/^\//, '')
  if (query) { const q = new URLSearchParams(Object.entries(query).filter(([, v]) => v !== undefined && v !== '' && v !== null)); if ([...q].length) url += '?' + q }
  const headers = { Accept: 'application/json' }
  const tok = getToken(); if (tok) headers.Authorization = 'Bearer ' + tok
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  let res
  try { res = await fetch(url, { method, headers, body: body !== undefined ? JSON.stringify(body) : undefined }) }
  catch (e) { throw new ApiError(0, { code: 'NETWORK', message: '网络错误，无法连接后台接口' }) }
  const text = await res.text(); let data = null
  try { data = text ? JSON.parse(text) : null } catch { data = { message: text.slice(0, 200) } }
  if (res.status === 401 && path !== 'auth/login') { setToken(null); onUnauthorized(); throw new ApiError(401, data || { code: 'UNAUTHORIZED', message: '登录已失效，请重新登录' }) }
  if (!res.ok) throw new ApiError(res.status, data)
  return data
}
export const errText = e => (e && e.message ? e.message : String(e)) + (e && e.code ? ` (${e.code})` : '')

/** useApi(path, query) -> { data, error, loading, reload } */
export function useApi(path, query) {
  const qk = JSON.stringify(query || {})
  const [st, setSt] = useState({ data: null, error: null, loading: true })
  const load = useCallback(() => {
    if (!path) return
    setSt(s => ({ ...s, loading: true, error: null }))
    api(path, { query }).then(data => setSt({ data, error: null, loading: false }), error => setSt({ data: null, error, loading: false }))
  }, [path, qk]) // eslint-disable-line
  useEffect(load, [load])
  return { ...st, reload: load }
}
export const items = d => (d && Array.isArray(d.items) ? d.items : [])

// ---- 展示格式 ----
const pad = n => String(n).padStart(2, '0')
export const ft = v => { if (v === null || v === undefined || v === '') return '-'; const d = new Date(typeof v === 'number' ? v : v); if (isNaN(d)) return String(v); return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}` }
export const num = (v, d = 2) => (v === null || v === undefined || v === '' || isNaN(+v)) ? '-' : Number(v).toLocaleString('en-US', { minimumFractionDigits: d, maximumFractionDigits: d })
export const USER_ST = { active: '正常', frozen: '冻结', withdraw_restricted: '限制提现', disabled: '停用' }
export const KYC_LV = l => l >= 2 ? 'L2 高级' : l === 1 ? 'L1 基础' : '未认证'
export const KYC_ST = { pending: '待审核', approved: '已通过', rejected: '已驳回', reviewing: '复审中' }
export const ROLE_CN = { super_admin: '超级管理员', operations: '运营', kyc_reviewer: 'KYC 审核员', finance_first: '财务初审', finance_second: '财务复审', risk: '风控专员', support: '客服', marketing: '市场', auditor: '审计（只读）' }
export const roleCn = rs => (rs || []).map(r => ROLE_CN[r] || r).join(' / ') || '-'
