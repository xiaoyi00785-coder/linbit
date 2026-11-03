// @ts-nocheck
"use client";
import {useHideSmall,filterSmall} from '@/lib/hideSmall'
import { openTransfer } from "@/components/Transfer";
import { goto, copyTxt } from "@/lib/ui";
import { useOverview } from "@/lib/wallet";
import {G,SOCIAL} from "@/components/Glyph";
import { MenuIcon } from "../components/MenuIcon";
import { SideIcon } from "../components/SideIcon";
import { toast } from '@/lib/ui'
import { openM, Dropdown, DateRange, Refresh } from '@/features/Kit'
import { CoinIcon } from '@/components/CoinIcon'
import { LoanRecords } from '@/features/Loan'
import { NavLink, Link, useNavigate, useLocation, useParams } from '@/lib/router'
import { useState } from 'react'
import { Footer } from './App'
import { MOCK } from './auth'
import { useProfile } from './Pages'
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
const fmtV=v=>{const n=Number(v||0);return n.toLocaleString('en-US',{maximumFractionDigits:n>=1?2:8})}
// 估值来自 GET /assets/overview；all=全部账户，acct=单账户；接口未上线时显示 0
const Val=({t='资产估值',children,set,acct,all})=>{const ov=useOverview();const d=ov.data;const v=all?d?.totalValueUsdt:acct?d?.accounts?.find(a=>a.account===acct)?.totalValueUsdt:0;return <div className="lcard v"><div className="lg">{t}(USDT) <Eye/>{set&&<span className="vset"><G n="gear" s={14}/></span>}</div><div className="big">{fmtV(v)} <small>USDT ▾</small></div><div className="lg">≈ ${fmtV(v)}</div>{children}</div>}
const TH=({c,s})=><div className="lth t2">{c.map((x,i)=><span key={x}>{x}{s&&i>0&&i<c.length-1&&<em className="srt">⇅</em>}</span>)}</div>
const Srch=()=>{const[h,set]=useHideSmall();return <><span className="sp"/><span className="lsearch"><G n="search" s={14}/> 搜索币种</span><label className={"lchk"+(h?" on":"")} role="checkbox" aria-checked={h} onClick={()=>set(!h)}><i/>隐藏小于 1 USD 的资产</label></>}
const URow=({v,usd=0,f})=>{const n=useNavigate();const[h]=useHideSmall();if(f&&h&&usd<1)return <Empty/>;return <div className="ltr"><span><U/> USDT</span>{v.map((x,i)=><span key={i} className={x.lime?'lime2 act':''}>{x.lime?x.lime.map(l=><a key={l} onClick={()=>l==='划转'?openTransfer():n(l==='交易'?'/futures':'/assets/history/funding')}>{l}</a>):x==='u'?<>0<small className="lg d">≈ $0</small></>:x}</span>)}</div>}
export function AOverview(){const n=useNavigate();const[t,setT]=useState(0);const[hs]=useHideSmall();return <AS><Val all><div className="lg">今日盈亏 <b className="lk">0 USDT</b></div><div className="lbtns l"><button className="lb dark" onClick={()=>n('/deposit')}>充值</button><button className="lb" onClick={()=>n('/withdraw')}>提现</button><button className="lb" onClick={()=>openTransfer()}>划转</button></div><span className="vchev">⌄</span></Val>
<div className="lcard"><h3>我的资产</h3><div className="ltabs">{['币种','账户'].map((x,i)=><b key={x} className={t===i?'on':''} onClick={()=>setT(i)}>{x}</b>)}<Srch/></div>{t?<><TH c={['账户','资产估值','占比']}/>{filterSmall(ACCTS.slice(1),()=>0,hs).map(([to,,a])=><div className="ltr" key={a}><span>{a}</span><span>0 USDT</span><span>0%</span></div>)}</>:<><TH c={['币种','数量','价格','占比']}/><Empty/></>}</div>
<div className="lcard"><div className="lrow"><h3>近期充值记录</h3><Link to="/assets/history/funding" className="lmore">全部 ›</Link></div><TH c={['时间','类型','数量','状态']}/><Empty/></div></AS>}
export function AFunding(){return <AS><Head t="资金账户" btns={[['充值','/deposit'],['提现','/withdraw'],['划转']]}/><Val set acct="funding"/><div className="lcard"><div className="ltabs"><h3>资产明细</h3><Srch/></div><TH s c={['资产','账户余额','可用','冻结','折合','操作']}/><Empty/></div></AS>}
export function AFutures(){return <AS><Head t="合约账户" btns={[['充值','/deposit'],['划转'],['合约记录','/assets/history/futures'],['订单明细','/orders/futures']]}/><Val t="总资产" acct="futures"><div className="lrow2"><span className="lg">今日盈亏 <b className="lk">0 ≈ $0 ›</b></span><span className="lg">未实现盈亏 <b className="lk">0 USDT</b></span></div></Val>
<div className="lcard"><div className="ltabs"><h3>合约资产明细</h3><Srch/></div><TH c={['资产','可用','账户余额','仓位保证金','委托占用','未实现盈亏','体验金','抵扣金','操作']}/><URow f v={['u','u','0','0','0','0','0',{lime:['交易','划转']}]}/></div></AS>}
export function ASpot(){return <AS><Head t="现货账户" btns={[['充值','/deposit'],['提现','/withdraw'],['划转'],['现货记录','/assets/history/spot']]}/><Val acct="spot"><div className="lg" style={{marginTop:6}}>今日盈亏 <b className="lk" style={{display:'inline'}}>0 USDT</b></div><div className="vaf">{['可用','冻结'].map(x=><div key={x}><span className="lg">{x}</span><b>0 USDT</b><small className="lg">≈ $0</small></div>)}</div></Val><div className="lcard"><div className="ltabs"><h3>资产明细</h3><Srch/></div><TH s c={['资产','账户余额','可用','冻结','折合','操作']}/><Empty/></div></AS>}
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
function Rec({t,tabs,fil,cols,extra,live,sub}){const[i,setI]=useState(0);const[si,setSi]=useState(0);if(live)return <AS><h2 className="lh2">{t}</h2>{live}</AS>;const f=typeof fil==='function'?fil(i):fil;const c=typeof cols==='function'?cols(i):cols;return <AS><h2 className="lh2">{t}</h2><div className="lcard rec">{tabs&&<div className="ltabs">{tabs.map((x,j)=><b key={x} className={i===j?'on':''} onClick={()=>{setI(j);setSi(0)}}>{x}</b>)}</div>}{sub&&sub(i)&&<div className="lsub">{sub(i).map((x,j)=><b key={x} className={si===j?'on':''} onClick={()=>setSi(j)}>{x}</b>)}</div>}<div className="lfil">{f}<span className="sp"/>{extra}<Refresh cls="lb sm g rfb"/></div><div className="lth t2 sm">{c.map(x=><span key={x}>{x}</span>)}</div><Empty/></div></AS>}
const R={funding:{t:'资金记录',tabs:['充值','提现','划转','分发'],fil:i=>[<Sel key={1} k="币种" v="全部"/>,i<2&&<Sel key={2} k="类型" v={i?'链上提现':'链上充值'}/>,i===2&&<Sel key={4} k="账户" v="全部"/>,<Sel key={3} k="状态" v="全部"/>,<Dt key={5}/>],cols:i=>i===2?['时间','币种','数量','从','到','状态']:i===3?['时间','币种','数量','类型','备注']:['时间','类型','币种','网络','数量','状态','链上交易']},
futures:{t:'合约记录',fil:[<Sel key={1} k="币种" v="全部"/>,<Sel key={2} k="类型" v="全部" w={200}/>,<Dt key={3}/>],cols:['时间','资产','类型','数量','合约']},
spot:{t:'现货记录',fil:[<Sel key={1} k="币种" v="全部"/>,<Sel key={2} k="类型" v="全部" w={200}/>,<Dt key={3}/>],cols:['时间','资产','类型','数量','余额']},
tradfi:{t:'传统金融记录',fil:[<Sel key={1} k="币种" v="全部"/>,<Sel key={2} k="类型" v="全部" w={200}/>,<Dt key={3}/>],cols:['时间','资产','类型','数量','品种']}}
export function Records(){const{k}=useParams();const r=R[k]||R.funding;return <Rec key={k} {...r}/>}
function HideCanceled(){const[on,setOn]=useState(false);return <label className={"lchk"+(on?" on":"")} onClick={()=>setOn(!on)}><i/>隐藏已撤销</label>}
const HC=<HideCanceled key="hc"/>
const O={futures:{t:'合约订单',tabs:['历史委托','历史仓位','成交明细','已实现盈亏'],fil:[<Sel key={1} k="合约" v="全部"/>,<Sel key={2} k="委托" v="限价 | 市价"/>,<Sel key={3} k="类型" v="全部"/>,<Sel key={4} k="日期" v="最近 7 天"/>,<Dt key={5} v/>],extra:HC,cols:i=>i===1?['合约','方向','开仓均价','平仓均价','最大持仓','已实现盈亏','开仓时间','平仓时间']:i===2?['合约','方向','成交价','成交数量','手续费','角色','成交时间']:i===3?['合约','类型','金额','时间']:['合约','已成交 | 总量','方向','成交均价','委托价','委托类型','状态','订单号','下单时间','成交/撤销时间']},
spot:{t:'现货订单',tabs:['当前委托','历史委托','成交明细'],fil:i=>[<Sel key={1} k="委托类型" v="限价 | 市价"/>,<Sel key={2} k="交易对" v="全部"/>,i>0&&<Sel key={3} k="日期" v="最近 7 天"/>,i>0&&<Dt key={4} v/>],extra:HC,cols:['交易对','下单时间','方向','委托价','已成交','委托数量','委托金额','止盈/止损','委托类型','全部撤销']},
buy:{t:'买币订单',fil:[<Sel key={1} k="币种" v="全部"/>,<Sel key={2} k="状态" v="全部"/>,<Dt key={3} v/>],cols:['时间','支付','获得','价格','渠道','状态']},
earn:{t:'理财订单',tabs:['理财宝','简单赚币','链上赚币','VIP 私享理财'],sub:i=>i===1?['活期','定期','闪赚']:null,fil:[<Sel key={1} k="币种" v="全部"/>,<Sel key={2} k="订单类型" v="全部"/>,<Sel key={3} k="日期" v="最近 7 天"/>,<Dt key={4} v/>],cols:['币种','产品类型','订单类型','数量','创建时间','完成时间','订单状态']},
convert:{t:'闪兑记录',fil:[<Sel key={1} k="日期" v="最近 7 天"/>,<Dt key={2} v/>],cols:['时间','支付','获得','价格','状态']},
tradfi:{t:'传统金融订单',tabs:['历史委托','成交明细','已实现盈亏'],fil:[<Sel key={1} k="品种" v="全部"/>,<Sel key={2} k="订单类型" v="全部"/>,<Sel key={3} k="日期" v="最近 7 天"/>,<Dt key={4} v/>],extra:HC,cols:['品种','数量','成交均价','成交数量','委托价','方向','订单类型','状态','订单号','下单时间']},
loan:{t:'借贷订单',tabs:['借款记录','还款记录','质押率调整记录','强平记录'],fil:[<Sel key={1} k="订单类型" v="活期"/>,<Sel key={2} k="借款币种" v="全部"/>,<Sel key={3} k="质押币种" v="全部"/>,<Sel key={4} k="状态" v="全部"/>,<Sel key={5} k="借款时间" v="最近 7 天"/>,<Dt key={6} v/>],cols:['借款时间','订单类型','借款币种','初始借款','质押币种','初始质押','小时 / 年化利率','状态']}}
export function OrdersX(){const{k}=useParams();const r=O[k]||O.futures;return <Rec key={k} {...r} live={k==='loan'?<LoanRecords/>:null}/>}
// —— 积分商城（深色，参数取自业务规则） ——
const PTASK=[['+168','合约累计交易任务','当日合约交易量达到不同档位可获得对应积分，每日最多 168 分。',1],['+10','充值任务','每入金 100 USDT 得 1 分，每日最多 10 分。','去充值'],['+80','邀请任务','每邀请 1 位有效用户（累计手续费 ≥100 USDT）得 8 分，每日最多 80 分。','去邀请']]
const TIERS=[['2千',1],['1万',1],['10万',3],['20万',3],['30万',5],['50万',8],['100万',12],['300万',35],['1000万',100]]
const REDEEM=[['3 USDT','抵扣金',10],['6 USDT','抵扣金',20],['10 USDT','体验金',35],['10 USDT','抵扣金',50],['20 USDT','体验金',75],['30 USDT','体验金',100],['75 USDT','抵扣金',200],['100 USDT','体验金',300]]
export function Points(){const n=useNavigate();return <main className="pts"><div className="wrap pth"><div><div className="lime">积分嘉年华 · 第五期（已结束）</div><h1>做任务赚积分，<br/>兑换体验金 / 抵扣金</h1><p className="g">2026-09-23 00:00 — 2026-10-22 23:59 (UTC+0)</p><div className="lrow l"><button className="pill dis" disabled aria-disabled="true">已结束</button><button className="pill sq" aria-label="分享" data-copy={typeof location!=='undefined'?location.href:''}><G n="share" s={14}/></button></div></div><img src="/img/rewards.webp" alt=""/></div>
<div className="wrap">
<h2 className="pth2">任务</h2>{PTASK.map(([p,t,d,b])=><div className="ptask" key={t}><div className="ptp"><b>{p}</b><small>积分</small></div><div className="ptb"><b>{t}</b><p className="g">{d}</p>{b===1?<><div className="tier">{TIERS.map(([v,s])=><div key={v}><em>+{s}</em><i/><small>{v}</small></div>)}</div><small className="g">今日合约交易量 0 / 2,000 USDT · 今日已得 0 分</small></>:<small className="g">今日进度 0 · 今日已得 0 分</small>}</div><button className="pill white" onClick={()=>n(b===1?'/futures':b==='去充值'?'/deposit':'/invite')}>{b===1?'去交易':b}</button></div>)}
<div className="lrow" style={{marginTop:60}}><h2 className="pth2" style={{margin:0}}>积分兑换奖励</h2><button className="pill dark" onClick={()=>n('/coupons')}>我的兑换记录 ›</button></div><p className="g">我的积分：0</p><div className="rdm">{REDEEM.map(([v,k,p])=><div key={v+k}><span className="coin">₮</span><b>{v}</b><small className="g">{k}</small><em><b>{p}</b> 积分兑换</em><button disabled>已结束</button></div>)}</div>
<h2 className="pth2">活动规则</h2><ol className="prul">{['活动仅限 linbit 已注册用户参与，需先点击“立即报名”，报名后完成的任务才计入积分。','积分仅在本期活动内有效，活动结束后清零，不跨期累计。','体验金、抵扣金将发放至卡券中心，激活时需满足合约账户余额要求。','任务进度每天 UTC 0 点重置，每档奖品每人限兑换一次。','奖励仅限体验金 / 抵扣金，不提供实物奖品，不可折现。','如发现刷量、对敲等违规行为，平台有权取消资格并收回奖励。'].map(x=><li key={x}>{x}</li>)}</ol></div><Footer/></main>}
// —— 头像下拉菜单 ——
export function AvatarMenu(){const p=useProfile();return <div className="avm"><div className="avc">{p.avatar?<img className="ava" src={p.avatar} alt="" style={{width:44,height:44,borderRadius:'50%',objectFit:'cover'}}/>:<div className="ava" style={{width:44,height:44}}><i/></div>}<div><b>{p.name}</b><small>UID: {p.uid} <G n="copy" s={12}/></small><small className="lg">{p.acct}</small></div><span className="avv">▭ {p.kyc?'已认证':'未认证'}</span></div>
{[['overview','总览','/setting/dashboard'],['lock','账户与安全','/setting/security'],['kyc','身份认证','/setting/kyc',1],['gift','福利中心','/rewards'],['invite','邀请好友','/invite'],['settings','设置','/setting/settings'],['api','API 管理','/setting/api']].map(([i,t,to,k])=><Link key={t} to={to} className="avi"><span><SideIcon n={i}/></span>{t}{k&&<em>未认证</em>}</Link>)}<Link to="/login" className="avo">退出登录</Link></div>}
