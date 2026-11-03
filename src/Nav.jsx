import { Link, NavLink } from 'react-router-dom'
import { useState, useEffect } from 'react'
import { useParams, useLocation } from 'react-router-dom'
import { QRCodeSVG } from 'qrcode.react'
import { coins } from './data'
import { useAuth, setAuthed } from './auth'
const I=({k})=><span className="ddi">{k}</span>
const M={
'买币':{w:256,items:[['💳','法币购买','多种支付方式，快速入金',1],['⬇','充值','链上充值，到账迅速'],['▣','linbit 卡','用数字资产全球消费']]},
'交易':{w:256,items:[['⇄','现货交易','轻松买卖主流数字资产'],['▦','现货网格','自动低买高卖的交易机器人'],['↻','定投','按计划周期自动买入'],['⇆','闪兑','一键兑换，零手续费']]},
'合约':{w:536,groups:[['交易',[['U','U本位永续','USDT 结算的永续合约',1],['▤','股票合约','覆盖美股与黄金等真实资产合约'],['◫','模拟交易','用模拟资金零风险体验合约'],['◉','传统金融','用 USDT 连接全球资产']]],['探索',[['◎','传统金融概览','三分钟了解传统资产交易']]]],coins:1},
'理财':{w:256,items:[['◔','理财总览','多样产品助力资产增长'],['◑','活期理财','随存随取，灵活收益'],['◕','定期理财','锁定期限，稳健增长'],['⛓','链上理财','参与链上质押获取收益']]},
'活动中心':{w:256,items:[['🎁','新人福利','完成任务领取新人礼包'],['🏆','交易大赛','赛季排名赢取丰厚奖励'],['👥','邀请返佣','邀请好友，共享手续费返佣']]},
'更多':{w:770,cols:[['合约数据',[['⚖','仓位档位','快速了解仓位档位规则'],['⌂','逐仓参数','查看逐仓交易对参数'],['∿','标记价格历史','查看标记价格历史走势'],['▥','指数价格','合约价格指数历史'],['％','资金费率历史','获取历史资金费率数据'],['⛨','保险基金','查看风险保障基金余额']]],['学院',[['📖','linbit 学院','学习区块链与数字资产知识','NEW']],'机构',[['⌘','API','开发者文档与接口支持']]],['帮助',[['?','帮助中心','查看帮助文档'],['📣','公告中心','查看平台重要公告']]]]}}
const R={'新人福利':'/rewards','交易大赛':'/rewards','邀请返佣':'/invite','API':'/api','跟单交易':'/copy','合约跟单':'/copy','法币购买':'/buy','充值':'/deposit','闪兑':'/convert','理财总览':'/earn','活期理财':'/earn/simple','定期理财':'/earn/bao','链上理财':'/earn/onchain','定投':'/strategy','现货网格':'/strategy','现货交易':'/spot','U本位永续':'/futures','股票合约':'/futures','模拟交易':'/sim','传统金融':'/futures','指数价格':'/futures','资金费率历史':'/futures'}
const path=t=>R[t]||'/p/'+encodeURIComponent(t)
const Item=([k,t,d,hot])=><Link className="dit" key={t} to={path(t)}><I k={k}/><div><b>{t}{hot===1&&' 🔥'}{hot==='NEW'&&<em className="new">NEW</em>}</b><small>{d}</small></div></Link>
function Panel({m}){if(m.cols)return <div className="dd" style={{width:m.w}}><div className="ddcols">{m.cols.map((c,i)=><div key={i}>{c.map((x,j)=>typeof x==='string'?<h5 key={j}>{x}</h5>:x.map(Item))}</div>)}</div></div>
if(m.groups)return <div className="dd nop" style={{width:m.w}}><div className="ddsplit"><div>{m.groups.map(([g,l])=><div key={g}><h6>{g}</h6>{l.map(Item)}</div>)}</div><div className="ddcoins"><div className="dds">⌕ 搜索币种</div>{coins.slice(0,9).map(c=><Link to="/futures" key={c.s}><i className="ico" style={{background:c.col,width:20,height:20,fontSize:10}}>{c.s[0]}</i>{c.s}USDT</Link>)}</div></div></div>
return <div className="dd" style={{width:m.w}}>{m.items.map(Item)}</div>}
const LANGS=['简体中文','English','繁體中文','Tiếng Việt','Bahasa Indonesia','Español','Русский','Português','Français','Українська',"O'zbekcha",'Polski','Türkçe','Italiano','日本語','한국어','Deutsch','ไทย','العربية','Ελληνικά','Español (Latinoamérica)','Português (Brasil)','Azərbaycan','Қазақша','Slovenčina','Slovenščina','Čeština','Română','Magyar','Svenska','Dansk','Nederlands','Bahasa Melayu','Filipino','हिन्दी','עברית']
const CUR=['USD','CNY','EUR','JPY','KRW','GBP','HKD','VND','RUB','TRY','INR','BRL']
function LangModal({close}){const[t,setT]=useState(0);const[sel,setSel]=useState(0);return <div className="mask" onClick={close}><div className="lmodal" onClick={e=>e.stopPropagation()}><div className="lmh"><div className="lt">{['语言与地区','计价货币'].map((x,i)=><b key={x} className={t===i?'on':''} onClick={()=>{setT(i);setSel(0)}}>{x}</b>)}</div><span className="x" onClick={close}>✕</span></div><div className="lgrid">{(t?CUR:LANGS).map((x,i)=><span key={x} className={sel===i?'on':''} onClick={()=>setSel(i)}>{x}</span>)}</div></div></div>}
export function Header(){const auth=useAuth();const[o,setO]=useState(null);const loc=useLocation();useEffect(()=>setO(null),[loc.pathname]);const[lang,setLang]=useState(false);const nav=[['/','买币'],['/markets','行情'],['/spot','交易'],['/futures','合约'],['/','理财'],['/','活动中心'],['/','更多']]
return <header className="hdr"><Link to="/" className="logo"><svg width="22" height="22" viewBox="0 0 22 22"><rect x="2" y="2" width="18" height="18" rx="5" fill="none" style={{stroke:'var(--primary)'}} strokeWidth="3"/><rect x="8" y="8" width="6" height="6" rx="1.5" style={{fill:'var(--primary)'}}/></svg>LINBIT</Link>
<nav>{nav.map(([to,l])=><div key={l} className={'ni'+(o===l?' open':'')} onMouseEnter={()=>M[l]&&setO(l)} onMouseLeave={()=>setO(null)}><NavLink end to={to}>{l}{M[l]&&<em>{o===l?'▴':'▾'}</em>}</NavLink>{o===l&&<div onClick={()=>setO(null)}><Panel m={M[l]}/></div>}</div>)}</nav>
<div className="sp"/>{auth?<><Link to="/deposit" className="pill white">充值</Link><span className="sep"/><Link to="/assets" className="hib" title="资产">▣</Link><Link to="/account" className="hib ava-s" title="个人中心"><i/></Link><Link to="/orders" className="hib" title="订单">≣</Link></>:<><Link to="/login" className="pill dark">登录</Link><Link to="/register" className="pill white">注册</Link><span className="sep"/></>}
<div className={'ni ic'+(o==='dl'?' open':'')} onMouseEnter={()=>setO('dl')} onMouseLeave={()=>setO(null)}><span className="hib">⤓</span>{o==='dl'&&<div className="dd qrpop"><p>扫码下载<br/>linbit App</p><div className="qr"><QRCodeSVG value="https://linbit.example.com/" size={120} marginSize={2} bgColor="transparent" fgColor="currentColor"/></div><button>更多平台</button></div>}</div>
<span className="hib" onClick={()=>setLang(true)}>◍</span>{lang&&<LangModal close={()=>setLang(false)}/>}</header>}
export function Cookie(){const[s,setS]=useState(()=>!localStorage.getItem('lb_ck'));if(!s)return null;const ok=()=>{localStorage.setItem('lb_ck',1);setS(false)}
return <div className="cookie"><h4>Cookie 设置</h4><p>我们使用 Cookie 来改善你的浏览体验并提供个性化服务。你可以随时调整偏好，详见 <a>Cookie 政策</a>。</p><button className="cw" onClick={ok}>全部接受</button><button className="cd" onClick={ok}>全部拒绝</button><a className="cs">Cookie 偏好设置</a></div>}
export function Service(){const[o,setO]=useState(false);return <><button className="svc" onClick={()=>setO(!o)} title="在线客服"><svg width="22" height="22" viewBox="0 0 24 24" fill="none" style={{stroke:'var(--on-primary)'}} strokeWidth="2"><path d="M4 13v-1a8 8 0 0116 0v1"/><rect x="3" y="13" width="4" height="6" rx="1.5"/><rect x="17" y="13" width="4" height="6" rx="1.5"/><path d="M19 19c0 2-2 3-5 3"/></svg></button>{o&&<div className="svcp"><b>在线客服</b><p>你好！有什么可以帮你？</p><input placeholder="输入你的问题…"/></div>}</>}

const ALL=Object.values(M).flatMap(m=>m.items||(m.groups||[]).flatMap(g=>g[1]).concat((m.cols||[]).flat().filter(x=>Array.isArray(x)).flat()))
export function Page(){const{t}=useParams();const it=ALL.find(x=>x[1]===t)||['◇',t,'该功能即将上线'];return <main className="pg"><div className="wrap"><div className="pgh"><span className="pgi">{it[0]}</span><div><h1>{it[1]}</h1><p>{it[2]}</p></div></div><div className="pgg">{['功能介绍','使用指南','常见问题'].map(x=><div className="box pgc" key={x}><h3>{x}</h3><p>「{it[1]}」相关内容正在建设中，敬请期待。</p></div>)}</div><Link to="/register" className="pill white pgb">立即注册</Link></div></main>}

export function DemoToggle(){const a=useAuth();return <button className="demo" onClick={()=>setAuthed(!a)}>演示：{a?'已登录':'未登录'} ⇄</button>}
