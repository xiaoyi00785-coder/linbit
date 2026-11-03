// @ts-nocheck
import { useEffect, useState } from 'react'
export const isAuthed=()=>localStorage.getItem('lb_auth')==='1'
import { useAuthStore } from '@/lib/store'
export const setAuthed=v=>{if(!v)useAuthStore.getState().logout();localStorage.setItem('lb_auth',v?'1':'0');window.dispatchEvent(new Event('lb_auth'))}
export function useAuth(){const[a,s]=useState(isAuthed());useEffect(()=>{const h=()=>s(isAuthed());window.addEventListener('lb_auth',h);return()=>window.removeEventListener('lb_auth',h)},[]);return a}
// 模拟用户（全部为虚构数据）
export const MOCK={name:'lb_Demo8K2',uid:'80000001',acct:'demo***@example.com',invite:'LB8DEMO'}
