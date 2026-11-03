import {useEffect,useState} from 'react'
// 隐藏小于 1 USD 的资产：全局偏好（localStorage），各列表通过 useHideSmall 读取并过滤
const K='lb_hide_small',EV='lb-hide-small'
const read=()=>typeof localStorage!=='undefined'&&localStorage.getItem(K)==='1'
export function setHideSmall(v:boolean){try{localStorage.setItem(K,v?'1':'0')}catch{};if(typeof window!=='undefined')window.dispatchEvent(new CustomEvent(EV,{detail:v}))}
export function useHideSmall():[boolean,(v:boolean)=>void]{const[h,setH]=useState(false);useEffect(()=>{setH(read());const f=(e:any)=>setH(!!e.detail);window.addEventListener(EV,f);return()=>window.removeEventListener(EV,f)},[]);return[h,setHideSmall]}
/** 过滤 usd 估值 < 1 的行 */
export const filterSmall=<T,>(rows:T[],usd:(r:T)=>number,on:boolean)=>on?rows.filter(r=>Number(usd(r)||0)>=1):rows
