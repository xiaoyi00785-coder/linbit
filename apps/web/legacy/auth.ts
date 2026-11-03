// @ts-nocheck
import { useEffect, useState } from 'react'
import { useAuthStore } from '@/lib/store'
import { API_BASE } from '@/lib/api'
// 唯一登录态来源：zustand 持久化的 lb-auth（accessToken）。旧的 localStorage lb_auth 标记不再作为判断依据
// （它会与真实会话脱节：退出/清除会话后仍为 '1'，或只有会话没有标记），仅为兼容旧代码继续写入。
const tokenOk = s => !!(s && s.isAuthed && s.accessToken)
export const isAuthed = () => tokenOk(useAuthStore.getState())
export const setAuthed = v => { if (!v) useAuthStore.getState().logout(); try { localStorage.setItem('lb_auth', v && isAuthed() ? '1' : '0') } catch {} ; window.dispatchEvent(new Event('lb_auth')) }
/** 退出：先调 POST /auth/logout 吊销刷新令牌，无论成功失败都执行本地 logout() */
export async function signOut(){
  const { accessToken, refreshToken } = useAuthStore.getState()
  try {
    if (accessToken || refreshToken) await fetch(API_BASE + '/auth/logout', {
      method: 'POST', keepalive: true,
      headers: { 'Content-Type': 'application/json', ...(accessToken ? { Authorization: `Bearer ${accessToken}` } : {}) },
      body: JSON.stringify({ refreshToken }),
      signal: typeof AbortSignal !== 'undefined' && AbortSignal.timeout ? AbortSignal.timeout(5000) : undefined,
    })
  } catch {} finally { setAuthed(false) }
}
// 跨标签页同步：其它标签页登录/退出时重新水合
if (typeof window !== 'undefined') window.addEventListener('storage', e => { if (e.key === 'lb-auth') useAuthStore.persist.rehydrate() })
/** 订阅登录态；首帧（持久化恢复前）按未登录渲染，避免 SSR 水合不一致 */
export function useAuth(){
  const authed = useAuthStore(tokenOk)
  const [ready, setReady] = useState(false)
  useEffect(() => { setReady(true) }, [])
  return ready && authed
}
// 模拟用户（全部为虚构数据）
export const MOCK={name:'lb_Demo8K2',uid:'80000001',acct:'demo***@example.com',invite:'LB8DEMO'}
