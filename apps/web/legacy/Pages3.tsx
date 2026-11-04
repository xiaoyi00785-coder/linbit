// @ts-nocheck
"use client";
import { stLabel, isOpenStatus } from "@/lib/orderStatus";
import {useHideSmall,filterSmall} from '@/lib/hideSmall'
import { openTransfer } from "@/components/Transfer";
import { goto, copyTxt } from "@/lib/ui";
import { useOverview } from "@/lib/wallet";
import {G} from "@/components/Glyph";
import { MenuIcon } from "../components/MenuIcon";
import { SideIcon } from "../components/SideIcon";
import { toast } from '@/lib/ui'
import { openM, Dropdown, DateRange, Refresh } from '@/features/Kit'
import { CoinIcon } from '@/components/CoinIcon'
import { LoanRecords } from '@/features/Loan'
import { NavLink, Link, useNavigate, useLocation, useParams } from '@/lib/router'
import { useState } from 'react'
import { Footer } from './App'
import { useProfile } from './Pages'
import { signOut } from './auth'
import { asset } from '@/lib/asset';
import { useRest } from '@/lib/rest';
import { useAuthStore } from '@/lib/store';
import { fmtAmt, fmtFeeStr, fmtFeeExact } from '@/lib/format'
import { spotRecords, unitPrice } from '@/lib/assetDisplay'
import { orderQtyText, orderAmountText } from '@/lib/orderDisplay'
// —— 资产中心（浅色），路径与线上一致 ——
const Empty=()=><div className="lempty"><div className="lbox"><G n="inbox" s={28}/></div><span>暂无数据</span></div>
const Eye=()=><span className="eye"><G n="eye" s={14}/></span>
const U=()=><CoinIcon s="USDT" size={22}/>
const ACCTS=[['/assets/overview','overview','资产总览'],['/assets/funding-account','funding','资金账户'],['/assets/trading-account','futures','合约账户'],['/assets/spot-account','spot','现货账户'],['/assets/earn-account','earn','理财账户'],['/assets/tradfi-account','tradfi','传统金融账户'],['/assets/bots-account','bots','策略账户']]
export const RECS=[['funding','资金记录'],['futures','合约记录'],['spot','现货记录'],['tradfi','传统金融记录']]
export const ORDS=[['futures','合约订单'],['spot','现货订单'],['buy','买币订单'],['earn','理财订单'],['convert','闪兑记录'],['tradfi','传统金融订单'],['loan','借贷订单']]
function ASide(){const p=useLocation().pathname;const[h,setH]=useState(p.startsWith('/assets/history'));const[o,setO]=useState(p.startsWith('/orders'))
return <aside className="lside a2">{ACCTS.map(([to,i,t])=><NavLink key={to} to={to} className="lsi"><span className="lsic"><SideIcon n={i}/></span>{t}</NavLink>)}
<div className="lsi" onClick={()=>setH(!h)}><span className="lsic"><SideIcon n="records"/></span>资金流水<em>{h?'▴':'▾'}</em></div>{h&&RECS.map(([k,t])=><NavLink key={k} to={'/assets/history/'+k} className="lsi sub">{t}</NavLink>)}
<div className="lsi" onClick={()=>setO(!o)}><span className="lsic"><SideIcon n="orders"/></span>订单中心<em>{o?'▴':'▾'}</em></div>{o&&ORDS.map(([k,t])=><NavLink key={k} to={'/orders/'+k} className="lsi sub">{t}</NavLink>)}</aside>}
const AS=({children})=><main className="light"><div className="lwrap">{<ASide/>}<section className="lmain a2">{children}</section></div></main>
const Head=({t,btns})=>{const n=useNavigate();return <div className="lhead"><h2>{t}</h2><div className="lbtns">{btns.map(([b,to],i)=><button key={b} className={i?'lb':'lb dark'} onClick={()=>to?n(to):b==='划转'?openTransfer(t):toast(b+' 即将上线','info')}>{b}</button>)}</div></div>}
const fmtV=v=>{const n=Number(v||0);return Math.abs(n)>=1?n.toLocaleString('en-US',{maximumFractionDigits:2}):fmtAmt(n)}
// 估值来自 GET /assets/overview；all=全部账户，acct=单账户；接口未上线时显示 0
const Val=({t='资产估值',children,set,acct,all})=>{const ov=useOverview();const d=ov.data;const v=all?d?.totalValueUsdt:acct?d?.accounts?.find(a=>a.account===acct)?.totalValueUsdt:0;const bad=!d&&!ov.loading;const ld=!d&&ov.loading;return <div className="lcard v"><div className="lg">{t}(USDT) <Eye/>{set&&<span className="vset"><G n="gear" s={14}/></span>}</div><div className="big" aria-busy={ld||undefined}>{ld?<i className="sk-in" data-testid="val-skel" style={{width:160,height:28}}/>:bad?'--':fmtV(v)} <small>USDT ▾</small></div><div className="lg">{ld?'≈ $--':bad?<>{ov.status===401?'登录已过期，请重新登录':'资产加载失败'} <a onClick={()=>ov.status===401?goto('/login'):ov.reload()}>{ov.status===401?'去登录':'重试'}</a></>:<>≈ ${fmtV(v)}</>}</div>{children}</div>}
// 账户内各币种余额（来自 /assets/overview 的 accounts[].balances）
const balsOf=(d,acct)=>(d?.accounts||[]).filter(a=>!acct||a.account===acct).flatMap(a=>a.balances||[])
const usdOf=b=>Number(b.valuationUsd??0)
// 单价用后端返回的实时指数价 priceUsd（market-data index）；不再用 估值/数量 反推（估值保留 2 位小数，小额持仓会被放大，如 BTC 显示 85,714.29）
const sumBy=bs=>{const m={};for(const b of bs){const k=b.asset;const c=m[k]||(m[k]={asset:k,free:0,locked:0,usd:0,priced:false});c.free+=Number(b.free||0);c.locked+=Number(b.locked||0);if(b.valuationUsd!=null){c.usd+=usdOf(b);c.priced=true}if(b.priceUsd!=null&&Number(b.priceUsd)>0)c.price=Number(b.priceUsd)}return Object.values(m).filter(r=>r.free+r.locked>0).sort((a,b)=>b.usd-a.usd)}
const pct=(x,t)=>t>0?(x/t*100).toFixed(2)+'%':'0%'
const BalRows=({acct})=>{const ov=useOverview();const[hs]=useHideSmall();const rows=filterSmall(sumBy(balsOf(ov.data,acct)),r=>r.usd,hs);if(!rows.length)return <Empty/>;return <>{rows.map(r=><div className="ltr" key={r.asset}><span><CoinIcon s={r.asset} size={22}/> {r.asset}</span><span>{fmtV(r.free+r.locked)}</span><span>{fmtV(r.free)}</span><span>{fmtV(r.locked)}</span><span>{r.priced?'≈ $'+fmtV(r.usd):'--'}</span><span className="lime2 act"><a onClick={()=>openTransfer()}>划转</a></span></div>)}</>}
const DepRows=()=>{const isA=useAuthStore(st=>st.isAuthed);const r=useRest(isA?'/wallet/deposit/records?page=1&pageSize=5':null);const it=r.data?.items||[];if(!it.length)return <Empty/>;return <>{it.map(x=><div className="ltr" key={x.id}><span>{x.createdAt?new Date(x.createdAt).toLocaleString('zh-CN',{hour12:false}):'--'}</span><span>充值 {x.asset}</span><span>{fmtV(x.amount)}</span><span>{x.status==='completed'?'已完成':x.status==='pending'?'确认中':x.status}</span></div>)}</>}
const TH=({c,s})=><div className="lth t2">{c.map((x,i)=><span key={x}>{x}{s&&i>0&&i<c.length-1&&<em className="srt">⇅</em>}</span>)}</div>
const Srch=()=>{const[h,set]=useHideSmall();return <><span className="sp"/><span className="lsearch"><G n="search" s={14}/> 搜索币种</span><label className={"lchk"+(h?" on":"")} role="checkbox" aria-checked={h} onClick={()=>set(!h)}><i/>隐藏小于 1 USD 的资产</label></>}
// 合约账户：/futures/account（可用 / 钱包余额 / 仓位保证金 / 委托占用 / 未实现盈亏）
// 合约今日盈亏（UTC 0 点起的 已实现盈亏 + 资金费 + 手续费 + 强平费/穿仓补偿）：GET /futures/pnl/today
const useFutTodayPnl=()=>{const isA=useAuthStore(st=>st.isAuthed);const d=useRest(isA?'/futures/pnl/today':null).data;return d&&d.pnl!=null?Number(d.pnl)||0:null};
const FutToday=()=>{const v=useFutTodayPnl();return v==null?<>--</>:<span className={v>0?'up':v<0?'down':undefined}>{v>0?'+':''}{fmtV(v)}</span>};
const useFutAcct=()=>{const isA=useAuthStore(st=>st.isAuthed);return useRest(isA?'/futures/account':null)}
const FutUpnl=()=>{const a=useFutAcct().data;return <>{a?fmtV(Number(a.crossUnrealizedPnl||0)+Number(a.isolatedUnrealizedPnl||0)):'0'}</>}
const FutRow=()=>{const n=useNavigate();const[h]=useHideSmall();const a=useFutAcct().data;const wb=Number(a?.walletBalance||0);if(!a||(h&&wb<1&&!(a.positions||[]).length))return <Empty/>;const pm=Number(a.crossPositionMargin||0)+Number(a.isolatedMargin||0);const up=Number(a.crossUnrealizedPnl||0)+Number(a.isolatedUnrealizedPnl||0);return <div className="ltr"><span><U/> USDT</span><span>{fmtV(a.availableBalance)}<small className="lg d">≈ ${fmtV(a.availableBalance)}</small></span><span>{fmtV(wb)}<small className="lg d">≈ ${fmtV(wb)}</small></span><span>{fmtV(pm)}</span><span>{fmtV(a.orderMargin)}</span><span className={up>=0?'up':'down'}>{fmtV(up)}</span><span>0</span><span>0</span><span className="lime2 act"><a onClick={()=>n('/futures')}>交易</a><a onClick={()=>openTransfer()}>划转</a></span></div>}
// 现货账户可用 / 冻结（冻结 = 限价挂单占用，来自 /assets/overview 的 locked）
const SpotFL=()=>{const ov=useOverview();const rows=sumBy(balsOf(ov.data,'spot'));const val=k=>rows.reduce((t,r)=>{const q=r.free+r.locked;return t+(q>0&&r.priced?r.usd*r[k]/q:0)},0);return <div className="vaf">{[['可用','free'],['冻结','locked']].map(([x,k])=><div key={x}><span className="lg">{x}</span><b>{fmtV(val(k))} USDT</b><small className="lg">≈ ${fmtV(val(k))}</small></div>)}</div>}
const URow=({v,usd=0,f})=>{const n=useNavigate();const[h]=useHideSmall();if(f&&h&&usd<1)return <Empty/>;return <div className="ltr"><span><U/> USDT</span>{v.map((x,i)=><span key={i} className={x.lime?'lime2 act':''}>{x.lime?x.lime.map(l=><a key={l} onClick={()=>l==='划转'?openTransfer():n(l==='交易'?'/futures':'/assets/history/funding')}>{l}</a>):x==='u'?<>0<small className="lg d">≈ $0</small></>:x}</span>)}</div>}
export function AOverview(){const n=useNavigate();const[t,setT]=useState(0);const[hs]=useHideSmall();const ov=useOverview();return <AS><Val all><div className="lg">合约今日盈亏 <b className="lk" title="UTC 0 点起：已实现盈亏 + 资金费 + 手续费" data-testid="today-pnl"><FutToday/> USDT</b></div><div className="lbtns l"><button className="lb dark" onClick={()=>n('/deposit')}>充值</button><button className="lb" onClick={()=>n('/withdraw')}>提现</button><button className="lb" onClick={()=>openTransfer()}>划转</button></div><span className="vchev">⌄</span></Val>
<div className="lcard"><h3>我的资产</h3><div className="ltabs">{['币种','账户'].map((x,i)=><b key={x} className={t===i?'on':''} onClick={()=>setT(i)}>{x}</b>)}<Srch/></div>{!ov.data&&ov.loading?<div style={{padding:16}} aria-busy="true"><div className="lb-skel" style={{height:60}}/></div>:t?<><TH c={['账户','资产估值','占比']}/>{filterSmall(ACCTS.slice(1).map(([to,k,a])=>[to,k,a,Number(ov.data?.accounts?.find(x=>x.account===k)?.totalValueUsdt||0)]),r=>r[3],hs).map(([to,,a,v])=><div className="ltr" key={a}><span><Link to={to}>{a}</Link></span><span>{fmtV(v)} USDT</span><span>{pct(v,Number(ov.data?.totalValueUsdt||0))}</span></div>)}</>:<><TH c={['币种','数量','价格','占比']}/>{(()=>{const rows=filterSmall(sumBy(balsOf(ov.data)),r=>r.usd,hs);const tot=Number(ov.data?.totalValueUsdt||0);return rows.length?rows.map(r=>{const q=r.free+r.locked;return <div className="ltr" key={r.asset}><span><CoinIcon s={r.asset} size={22}/> {r.asset}</span><span>{fmtV(q)}</span><span>{unitPrice(r,fmtV)}</span><span>{pct(r.usd,tot)}</span></div>}):<Empty/>})()}</>}</div>
<div className="lcard"><div className="lrow"><h3>近期充值记录</h3><Link to="/assets/history/funding" className="lmore">全部 ›</Link></div><TH c={['时间','类型','数量','状态']}/><DepRows/></div></AS>}
export function AFunding(){return <AS><Head t="资金账户" btns={[['充值','/deposit'],['提现','/withdraw'],['划转']]}/><Val set acct="funding"/><div className="lcard"><div className="ltabs"><h3>资产明细</h3><Srch/></div><TH s c={['资产','账户余额','可用','冻结','折合','操作']}/><BalRows acct="funding"/></div></AS>}
export function AFutures(){return <AS><Head t="合约账户" btns={[['充值','/deposit'],['划转'],['合约记录','/assets/history/futures'],['订单明细','/orders/futures']]}/><Val t="总资产" acct="futures"><div className="lrow2"><span className="lg">今日盈亏 <b className="lk" title="UTC 0 点起：已实现盈亏 + 资金费 + 手续费" data-testid="today-pnl"><FutToday/> USDT</b></span><span className="lg">未实现盈亏 <b className="lk"><FutUpnl/> USDT</b></span></div></Val>
<div className="lcard"><div className="ltabs"><h3>合约资产明细</h3><Srch/></div><TH c={['资产','可用','账户余额','仓位保证金','委托占用','未实现盈亏','体验金','抵扣金','操作']}/><FutRow/></div></AS>}
export function ASpot(){return <AS><Head t="现货账户" btns={[['充值','/deposit'],['提现','/withdraw'],['划转'],['现货记录','/assets/history/spot']]}/><Val acct="spot"><div className="lg" style={{marginTop:6}}>今日盈亏 <b className="lk" style={{display:'inline'}} title="暂无今日盈亏数据" data-testid="today-pnl">-- USDT</b></div><SpotFL/></Val><div className="lcard"><div className="ltabs"><h3>资产明细</h3><Srch/></div><TH s c={['资产','账户余额','可用','冻结','折合','操作']}/><BalRows acct="spot"/></div></AS>}
export function ATradfi(){return <AS><Head t="传统金融账户" btns={[['充值','/deposit'],['划转'],['传统金融订单','/orders/tradfi'],['传统金融记录','/assets/history/tradfi']]}/><Val t="总资产"><div className="lg" style={{marginTop:14}}>未实现盈亏</div><b className="lk">0 USDT</b><div className="lg">≈ $0</div></Val>
<div className="lcard"><TH s c={['资产','可用','账户余额','仓位保证金','委托占用','未实现盈亏','操作']}/><URow v={['u','u','0','0','0',{lime:['划转']}]}/></div></AS>}
export function ABots(){return <AS><Head t="策略账户" btns={[['创建策略','/strategy'],['划转']]}/><Val/><div className="lcard"><h3>我的策略</h3><div className="seg"><b className="on">现货网格</b></div><TH c={['策略','交易对','投入','总收益','运行时长','操作']}/><Empty/></div></AS>}
const EARN_T=['理财宝','简单赚币','链上赚币','VIP 私享理财','质押借币']
export function AEarn(){const[t,setT]=useState(0);const[s,setS]=useState(0);return <AS><Head t="理财账户" btns={[['理财总览','/earn'],['理财订单','/orders/earn'],['现货记录','/assets/history/spot']]}/><Val/>
<div className="lcard"><div className="ltabs">{EARN_T.map((x,i)=><b key={x} className={t===i?'on':''} onClick={()=>setT(i)}>{x}</b>)}</div><div className="estat">{['资产估值','24h 收益','累计收益'].map(x=><div key={x}><span className="lg">{x}</span><b>0</b><small className="lg">≈ $0</small></div>)}</div>
{t===0&&<div className="seg">{['活期','定期','限时抢购'].map((x,i)=><b key={x} className={s===i?'on':''} onClick={()=>setS(i)}>{x}</b>)}</div>}<TH c={t===4?['借款币种','质押币种','借款金额','LTV','小时利率','操作']:['币种','参考年化','持有数量','24h 收益','累计收益','自动申购','操作']}/><Empty/></div></AS>}
// —— 记录 / 订单（通用） ——
const Sel=({k,v,w})=><div><span className="flab">{k}</span><Dropdown k={k} v={v}/></div>
const Dt=({v})=><div><span className="flab">&nbsp;</span><DateRange preset={v}/></div>
function Rec({t,tabs,fil,cols,extra,live,sub,rows}){const[i,setI]=useState(0);const[si,setSi]=useState(0);if(live)return <AS><h2 className="lh2">{t}</h2>{live}</AS>;const f=typeof fil==='function'?fil(i):fil;const c=typeof cols==='function'?cols(i):cols;return <AS><h2 className="lh2">{t}</h2><div className="lcard rec">{tabs&&<div className="ltabs">{tabs.map((x,j)=><b key={x} className={i===j?'on':''} onClick={()=>{setI(j);setSi(0)}}>{x}</b>)}</div>}{sub&&sub(i)&&<div className="lsub">{sub(i).map((x,j)=><b key={x} className={si===j?'on':''} onClick={()=>setSi(j)}>{x}</b>)}</div>}<div className="lfil">{f}<span className="sp"/>{extra}<Refresh cls="lb sm g rfb"/></div><div className="lth t2 sm">{c.map(x=><span key={x}>{x}</span>)}</div>{rows?rows(i,si):<Empty/>}</div></AS>}
// —— 记录 / 订单页的真实数据行：path 为空表示该分类暂无接口 ——
const tm=v=>{if(v==null||v==='')return '--';const d=new Date(typeof v==='number'||/^\d+$/.test(String(v))?Number(v):v);return isNaN(+d)?'--':d.toLocaleString('zh-CN',{hour12:false})}
const ST_={completed:'已完成',pending:'处理中',processing:'处理中',failed:'失败',rejected:'已拒绝',canceled:'已撤销',success:'成功',filled:'已成交',FILLED:'已成交',NEW:'未成交',PARTIALLY_FILLED:'部分成交',CANCELED:'已撤销',REJECTED:'已拒绝',EXPIRED:'已过期',closed:'已平仓',liquidated:'强平',adl:'自动减仓',new:'未成交',open:'未成交',partially_filled:'部分成交',partial:'部分成交',done:'已完成',cancelled:'已撤销',expired:'已过期',triggered:'已触发'}
const st_=v=>stLabel(v)
const ACCT_={funding:'资金账户',spot:'现货账户',futures:'合约账户',earn:'理财账户'}
const BILL_={realized_pnl:'已实现盈亏',fee:'手续费',funding:'资金费',transfer_in:'转入',transfer_out:'转出',liquidation:'强平',liquidation_fee:'强平手续费',insurance_cover:'保险基金补偿',bonus:'体验金'}
function Live({path,pick,row}){const isA=useAuthStore(st=>st.isAuthed);const r=useRest(isA&&path?path:null);if(!isA)return <Empty/>;if(r.loading&&!r.data)return <div style={{padding:16}}><div className="lb-skel" style={{height:60}}/></div>;const L=(r.data&&pick(r.data))||[];if(!L.length)return r.error?<div className="lempty"><span>{r.error} <a onClick={()=>r.reload()}>重试</a></span></div>:<Empty/>;return <>{L.map((x,k)=><div className="ltr" key={x.id??x.orderId??x.tradeId??k}>{row(x).map((c,ci)=><span key={ci}>{c}</span>)}</div>)}</>}
const items_=d=>d.items
// 全部：充值 + 提现 + 划转 合并按时间倒序（默认 tab，避免默认停在空的「充值」）
function FundAll(){const isA=useAuthStore(st=>st.isAuthed);const P=['/wallet/deposit/records?page=1&pageSize=50','/wallet/withdraw/records?page=1&pageSize=50','/assets/transfers?page=1&pageSize=50'];const a=useRest(isA?P[0]:null),b=useRest(isA?P[1]:null),c=useRest(isA?P[2]:null);if(!isA)return <Empty/>;const R3=[a,b,c];if(R3.some(r=>r.loading&&!r.data))return <div style={{padding:16}} aria-busy="true"><div className="lb-skel" style={{height:60}}/></div>;
const L=[...(a.data?.items||[]).map(x=>({...x,_k:'链上充值'})),...(b.data?.items||[]).map(x=>({...x,_k:'链上提现'})),...(c.data?.items||[]).map(x=>({...x,_k:'划转 '+(ACCT_[x.from]||x.from||'')+'→'+(ACCT_[x.to]||x.to||'')}))].sort((x,y)=>+new Date(y.createdAt)-+new Date(x.createdAt));
if(!L.length){const e=R3.find(r=>r.error);return e?<div className="lempty"><span>{e.error} <a onClick={()=>R3.forEach(r=>r.reload())}>重试</a></span></div>:<Empty/>}
return <>{L.map((x,k)=><div className="ltr" key={x._k+(x.id??k)}>{[tm(x.createdAt),x._k,x.asset,fmtV(x.amount),st_(x.status)].map((v,ci)=><span key={ci}>{v}</span>)}</div>)}</>}
const FUND_ROWS=i=>i===0?<FundAll/>:(j=>j===0?<Live path="/wallet/deposit/records?page=1&pageSize=50" pick={items_} row={x=>[tm(x.createdAt),'链上充值',x.asset,x.network,fmtV(x.amount),st_(x.status),x.txId?String(x.txId).slice(0,10)+'…':'--']}/>
 :j===1?<Live path="/wallet/withdraw/records?page=1&pageSize=50" pick={items_} row={x=>[tm(x.createdAt),'链上提现',x.asset,x.network,fmtV(x.amount),st_(x.status),'--']}/>
 :j===2?<Live path="/assets/transfers?page=1&pageSize=50" pick={items_} row={x=>[tm(x.createdAt),x.asset,fmtV(x.amount),ACCT_[x.from]||x.from,ACCT_[x.to]||x.to,st_(x.status)]}/>:<Empty/>)(i-1)
const FUT_BILLS=()=><Live path="/futures/bills?limit=200" pick={d=>d.bills} row={x=>[tm(x.time),'USDT',BILL_[x.kind]||x.kind,fmtFeeExact(x.amount),x.symbol||'--']}/>
// 现货记录：现货成交 + 涉及现货账户的划转（GET /assets/transfers，from/to 含 spot），按时间倒序合并
function SpotRec(){const isA=useAuthStore(st=>st.isAuthed);const a=useRest(isA?'/spot/trades?page=1&pageSize=50':null),b=useRest(isA?'/assets/transfers?page=1&pageSize=100':null);if(!isA)return <Empty/>;if([a,b].some(r=>r.loading&&!r.data))return <div style={{padding:16}} aria-busy="true"><div className="lb-skel" style={{height:60}}/></div>;const L=spotRecords(a.data?.items,b.data?.items,v=>fmtV(v));if(!L.length){const e=[a,b].find(r=>r.error);return e?<div className="lempty"><span>{e.error} <a onClick={()=>{a.reload();b.reload()}}>重试</a></span></div>:<Empty/>}return <>{L.map(x=><div className="ltr" key={x.k}>{[tm(x.t),...x.row,'--'].map((c,ci)=><span key={ci}>{c}</span>)}</div>)}</>}
const SPOT_REC=()=><SpotRec/>
const sd=v=>/buy|long/i.test(v)?'买入':'卖出'
const FUT_ORD=i=>i===1?<Live path="/futures/positions/history?limit=200" pick={d=>d.positions} row={x=>[x.symbol,x.side==='long'?'多':'空',x.entryPrice,x.closePrice,x.maxQty,fmtFeeExact(x.realizedPnl),tm(x.openTime),tm(x.closeTime)]}/>
 :i===2?<Live path="/futures/trades?limit=200" pick={d=>d.trades} row={x=>[x.symbol,sd(x.side),x.price,x.qty,fmtFeeExact(x.fee),x.role==='maker'?'Maker':'Taker',tm(x.time)]}/>
 :i===3?<Live path="/futures/bills?limit=200" pick={d=>(d.bills||[]).filter(b=>/pnl/i.test(b.kind))} row={x=>[x.symbol||'--',BILL_[x.kind]||x.kind,fmtFeeExact(x.amount),tm(x.time)]}/>
 :<Live path="/futures/orders?limit=200" pick={d=>(d.orders||[]).filter(o=>!isOpenStatus(o.status))} row={x=>[x.symbol,`${x.filledQty} | ${x.qty}`,sd(x.side),x.avgPrice||'--',x.price&&+x.price?x.price:'市价',x.type==='limit'?'限价':'市价',st_(x.status),x.orderId,tm(x.createdAt),tm(x.updatedAt)]}/>
const SPOT_ORD=i=>i===2?<Live path="/spot/trades?page=1&pageSize=50" pick={items_} row={x=>[x.symbol,x.side==='buy'?'买入':'卖出',x.price,x.quantity,fmtV(+x.price*+x.quantity),x.fee!=null?fmtFeeStr(x.fee)+' '+(x.feeAsset||''):'--',x.role==='maker'?'Maker':'Taker',tm(x.createdAt)]}/>
 :<Live path={i?'/spot/orders/history?page=1&pageSize=50':'/spot/orders/open?page=1&pageSize=50'} pick={items_} row={x=>[x.symbol,tm(x.createdAt),x.side==='buy'?'买入':'卖出',x.price||'市价',x.filledQuantity,orderQtyText(x),orderAmountText(x,fmtV),'--',x.type==='limit'?'限价':'市价',st_(x.status)]}/>
const CONV_ROWS=()=><Live path="/convert/orders?page=1&pageSize=50" pick={items_} row={x=>[tm(x.createdAt),`${fmtV(x.fromAmount)} ${x.from}`,`${fmtV(x.toAmount)} ${x.to}`,x.rate,st_(x.status)]}/>
const R={funding:{rows:FUND_ROWS,t:'资金记录',tabs:['全部','充值','提现','划转','分发'],fil:i=>[<Sel key={1} k="币种" v="全部"/>,(i===1||i===2)&&<Sel key={2} k="类型" v={i===2?'链上提现':'链上充值'}/>,i===3&&<Sel key={4} k="账户" v="全部"/>,<Sel key={3} k="状态" v="全部"/>,<Dt key={5}/>],cols:i=>i===0?['时间','类型','币种','数量','状态']:i===3?['时间','币种','数量','从','到','状态']:i===4?['时间','币种','数量','类型','备注']:['时间','类型','币种','网络','数量','状态','链上交易']},
futures:{rows:FUT_BILLS,t:'合约记录',fil:[<Sel key={1} k="币种" v="全部"/>,<Sel key={2} k="类型" v="全部" w={200}/>,<Dt key={3}/>],cols:['时间','资产','类型','数量','合约']},
spot:{rows:SPOT_REC,t:'现货记录',fil:[<Sel key={1} k="币种" v="全部"/>,<Sel key={2} k="类型" v="全部" w={200}/>,<Dt key={3}/>],cols:['时间','资产','类型','数量','余额']},
tradfi:{t:'传统金融记录',fil:[<Sel key={1} k="币种" v="全部"/>,<Sel key={2} k="类型" v="全部" w={200}/>,<Dt key={3}/>],cols:['时间','资产','类型','数量','品种']}}
export function Records(){const{k}=useParams();const r=R[k]||R.funding;return <Rec key={k} {...r}/>}
function HideCanceled(){const[on,setOn]=useState(false);return <label className={"lchk"+(on?" on":"")} onClick={()=>setOn(!on)}><i/>隐藏已撤销</label>}
const HC=<HideCanceled key="hc"/>
const O={futures:{rows:FUT_ORD,t:'合约订单',tabs:['历史委托','历史仓位','成交明细','已实现盈亏'],fil:[<Sel key={1} k="合约" v="全部"/>,<Sel key={2} k="委托" v="限价 | 市价"/>,<Sel key={3} k="类型" v="全部"/>,<Sel key={4} k="日期" v="最近 7 天"/>,<Dt key={5} v/>],extra:HC,cols:i=>i===1?['合约','方向','开仓均价','平仓均价','最大持仓','已实现盈亏','开仓时间','平仓时间']:i===2?['合约','方向','成交价','成交数量','手续费','角色','成交时间']:i===3?['合约','类型','金额','时间']:['合约','已成交 | 总量','方向','成交均价','委托价','委托类型','状态','订单号','下单时间','成交/撤销时间']},
spot:{rows:SPOT_ORD,t:'现货订单',tabs:['当前委托','历史委托','成交明细'],fil:i=>[<Sel key={1} k="委托类型" v="限价 | 市价"/>,<Sel key={2} k="交易对" v="全部"/>,i>0&&<Sel key={3} k="日期" v="最近 7 天"/>,i>0&&<Dt key={4} v/>],extra:HC,cols:i=>i===2?['交易对','方向','成交价','成交数量','成交金额','手续费','角色','成交时间']:['交易对','下单时间','方向','委托价','已成交','委托数量','委托金额','止盈/止损','委托类型','全部撤销']},
buy:{t:'买币订单',fil:[<Sel key={1} k="币种" v="全部"/>,<Sel key={2} k="状态" v="全部"/>,<Dt key={3} v/>],cols:['时间','支付','获得','价格','渠道','状态']},
earn:{t:'理财订单',tabs:['理财宝','简单赚币','链上赚币','VIP 私享理财'],sub:i=>i===1?['活期','定期','闪赚']:null,fil:[<Sel key={1} k="币种" v="全部"/>,<Sel key={2} k="订单类型" v="全部"/>,<Sel key={3} k="日期" v="最近 7 天"/>,<Dt key={4} v/>],cols:['币种','产品类型','订单类型','数量','创建时间','完成时间','订单状态']},
convert:{rows:CONV_ROWS,t:'闪兑记录',fil:[<Sel key={1} k="日期" v="最近 7 天"/>,<Dt key={2} v/>],cols:['时间','支付','获得','价格','状态']},
tradfi:{t:'传统金融订单',tabs:['历史委托','成交明细','已实现盈亏'],fil:[<Sel key={1} k="品种" v="全部"/>,<Sel key={2} k="订单类型" v="全部"/>,<Sel key={3} k="日期" v="最近 7 天"/>,<Dt key={4} v/>],extra:HC,cols:['品种','数量','成交均价','成交数量','委托价','方向','订单类型','状态','订单号','下单时间']},
loan:{t:'借贷订单',tabs:['借款记录','还款记录','质押率调整记录','强平记录'],fil:[<Sel key={1} k="订单类型" v="活期"/>,<Sel key={2} k="借款币种" v="全部"/>,<Sel key={3} k="质押币种" v="全部"/>,<Sel key={4} k="状态" v="全部"/>,<Sel key={5} k="借款时间" v="最近 7 天"/>,<Dt key={6} v/>],cols:['借款时间','订单类型','借款币种','初始借款','质押币种','初始质押','小时 / 年化利率','状态']}}
export function OrdersX(){const{k}=useParams();const r=O[k]||O.futures;return <Rec key={k} {...r} live={k==='loan'?<LoanRecords/>:null}/>}
// —— 积分商城（深色，参数取自业务规则） ——
const PTASK=[['+168','合约累计交易任务','当日合约交易量达到不同档位可获得对应积分，每日最多 168 分。',1],['+10','充值任务','每入金 100 USDT 得 1 分，每日最多 10 分。','去充值'],['+80','邀请任务','每邀请 1 位有效用户（累计手续费 ≥100 USDT）得 8 分，每日最多 80 分。','去邀请']]
const TIERS=[['2千',1],['1万',1],['10万',3],['20万',3],['30万',5],['50万',8],['100万',12],['300万',35],['1000万',100]]
const REDEEM=[['3 USDT','抵扣金',10],['6 USDT','抵扣金',20],['10 USDT','体验金',35],['10 USDT','抵扣金',50],['20 USDT','体验金',75],['30 USDT','体验金',100],['75 USDT','抵扣金',200],['100 USDT','体验金',300]]
export function Points(){const n=useNavigate();return <main className="pts"><div className="wrap pth"><div><div className="lime">积分嘉年华 · 第五期（已结束）</div><h1>做任务赚积分，<br/>兑换体验金 / 抵扣金</h1><p className="g">2026-09-23 00:00 — 2026-10-22 23:59 (UTC+0)</p><div className="lrow l"><button className="pill dis" disabled aria-disabled="true">已结束</button><button className="pill sq" aria-label="分享" data-copy={typeof location!=='undefined'?location.href:''}><G n="share" s={14}/></button></div></div><img src={asset("/img/rewards.webp")} alt=""/></div>
<div className="wrap">
<h2 className="pth2">任务</h2>{PTASK.map(([p,t,d,b])=><div className="ptask" key={t}><div className="ptp"><b>{p}</b><small>积分</small></div><div className="ptb"><b>{t}</b><p className="g">{d}</p>{b===1?<><div className="tier">{TIERS.map(([v,s])=><div key={v}><em>+{s}</em><i/><small>{v}</small></div>)}</div><small className="g">今日合约交易量 0 / 2,000 USDT · 今日已得 0 分</small></>:<small className="g">今日进度 0 · 今日已得 0 分</small>}</div><button className="pill white" onClick={()=>n(b===1?'/futures':b==='去充值'?'/deposit':'/invite')}>{b===1?'去交易':b}</button></div>)}
<div className="lrow" style={{marginTop:60}}><h2 className="pth2" style={{margin:0}}>积分兑换奖励</h2><button className="pill dark" onClick={()=>n('/coupons')}>我的兑换记录 ›</button></div><p className="g">我的积分：0</p><div className="rdm">{REDEEM.map(([v,k,p])=><div key={v+k}><span className="coin">₮</span><b>{v}</b><small className="g">{k}</small><em><b>{p}</b> 积分兑换</em><button disabled>已结束</button></div>)}</div>
<h2 className="pth2">活动规则</h2><ol className="prul">{['活动仅限 linbit 已注册用户参与，需先点击“立即报名”，报名后完成的任务才计入积分。','积分仅在本期活动内有效，活动结束后清零，不跨期累计。','体验金、抵扣金将发放至卡券中心，激活时需满足合约账户余额要求。','任务进度每天 UTC 0 点重置，每档奖品每人限兑换一次。','奖励仅限体验金 / 抵扣金，不提供实物奖品，不可折现。','如发现刷量、对敲等违规行为，平台有权取消资格并收回奖励。'].map(x=><li key={x}>{x}</li>)}</ol></div><Footer/></main>}
// —— 头像下拉菜单 ——
export function AvatarMenu(){const p=useProfile();return <div className="avm"><div className="avc">{p.avatar?<img className="ava" src={p.avatar} alt="" style={{width:44,height:44,borderRadius:'50%',objectFit:'cover'}}/>:<div className="ava" style={{width:44,height:44}}><i/></div>}{p.loading?<div aria-busy="true"><b><i className="sk" style={{width:110}}/></b><small><i className="sk" style={{width:90}}/></small><small><i className="sk" style={{width:130}}/></small></div>:<div><b>{p.name}</b><small>UID: {p.uid} <G n="copy" s={12}/></small><small className="lg">{p.acct}</small></div>}{!p.loading&&<span className="avv">▭ {p.kyc?'已认证':'未认证'}</span>}</div>
{[['overview','总览','/setting/dashboard'],['lock','账户与安全','/setting/security'],['kyc','身份认证','/setting/kyc',1],['gift','福利中心','/rewards'],['invite','邀请好友','/invite'],['settings','设置','/setting/settings'],['api','API 管理','/setting/api']].map(([i,t,to,k])=><Link key={t} to={to} className="avi"><span><SideIcon n={i}/></span>{t}{k&&<em>未认证</em>}</Link>)}<a href="/login" className="avo" onClick={async e=>{e.preventDefault();await signOut();location.href='/login'}}>退出登录</a></div>}
