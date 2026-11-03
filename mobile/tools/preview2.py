"""阶段二页面 HTML 模拟：与 SwiftUI 同 tokens/尺寸，日/夜对照（参考图 | 日间 | 夜间）。数据为 mock（测试网现货价格为 0、需登录接口无测试账号）。"""
import json, os, subprocess
from PIL import Image, ImageDraw, ImageFont
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
T = json.load(open(f"{ROOT}/tokens/tokens.json")); OUT = f"{ROOT}/previews"; os.makedirs(OUT, exist_ok=True)

def css(c, dark):
    return f"""*{{box-sizing:border-box;margin:0;padding:0}} body{{width:390px;height:844px;overflow:hidden;background:{c['bg']};color:{c['textPrimary']};
font-family:-apple-system,"PingFang SC","Noto Sans CJK SC",sans-serif;position:relative;font-size:14px}}
.sb{{height:47px;display:flex;align-items:center;justify-content:space-between;padding:0 30px 0 34px;font-weight:600;font-size:16px}}
.row{{display:flex;align-items:center}} .sp{{flex:1}} .sec{{color:{c['textSecondary']}}} .ter{{color:{c['textTertiary']}}} .num{{font-variant-numeric:tabular-nums}}
.pad{{padding:0 16px}} .h1{{font-size:28px;font-weight:700}} .lab{{font-size:14px;font-weight:500;margin:22px 0 8px}}
.inp{{height:48px;border-radius:10px;background:{c['fill']};display:flex;align-items:center;padding:0 14px;color:{c['textTertiary']};font-size:15px}}
.btn{{height:48px;border-radius:24px;background:{c['primary']};color:{c['textOnPrimary']};display:flex;align-items:center;justify-content:center;font-size:16px;font-weight:600}}
.btn2{{height:48px;border-radius:24px;border:1px solid {c['border']};display:flex;align-items:center;justify-content:center;font-size:15px;font-weight:500;gap:8px}}
.lime{{color:{c['brand']}}} .dash{{border-bottom:1px dashed {c['textSecondary']}}}
.nav{{height:44px;display:flex;align-items:center;justify-content:center;font-size:17px;font-weight:600;position:relative}} .nav i{{position:absolute;left:16px;font-style:normal;font-size:22px}}
.mask{{position:absolute;inset:0;background:{c['mask'][:7]}{'A0' if dark else '66'}}}
.sheet{{position:absolute;left:0;right:0;bottom:0;background:{c['bgElevated']};border-radius:16px 16px 0 0;padding:8px 16px 34px}}
.grab{{width:36px;height:4px;border-radius:2px;background:{c['fillStrong']};margin:0 auto 14px}}
.card{{border:1px solid {c['border']};border-radius:12px;padding:14px;margin-bottom:10px}}
.tag{{font-size:10px;padding:1px 5px;border-radius:3px}}
.tabbar{{position:absolute;left:21px;right:21px;bottom:21px;height:61px;border-radius:31px;background:{c['tabBarBg'][:7]};border:1px solid {c['tabBarBorder']};display:flex;align-items:center;padding:0 5px}}
.ti{{flex:1;height:51px;border-radius:26px;display:flex;align-items:center;justify-content:center;font-size:11px;color:{c['textSecondary']}}} .ti.sel{{background:{c['tabBarSelected']};color:{c['textPrimary']};font-weight:600}}
.box{{height:44px;border-radius:8px;background:{c['fill']};display:flex;align-items:center;padding:0 10px;margin-bottom:10px}}
.up{{color:{c['up']}}} .dn{{color:{c['down']}}} .coin{{width:32px;height:32px;border-radius:16px;background:{c['fillStrong']};display:flex;align-items:center;justify-content:center;font-weight:600;margin-right:12px}}
"""
SB = '<div class="sb num"><span>9:41</span><span style="font-size:13px">▮▮▮ ◔ ▭</span></div>'
def tabbar(sel): return '<div class="tabbar">' + "".join(f'<div class="ti {"sel" if n==sel else ""}">{n}</div>' for n in ["首页","行情","合约","现货","资产"]) + '</div>'
TOP = '<div class="row pad" style="height:44px;font-size:20px"><span>⌄</span><div class="sp"></div><span>🎧</span></div>'
SOCIAL = lambda: '<div class="btn2" style="margin-top:12px">🔑 使用通行密钥继续</div><div class="btn2" style="margin-top:12px"> 通过 Apple 继续</div><div class="btn2" style="margin-top:12px">G 通过 Google 继续</div>'
OR = lambda c: f'<div class="row ter" style="margin:20px 0 8px;gap:10px;font-size:13px"><div class="sp" style="height:1px;background:{c["divider"]}"></div>或者<div class="sp" style="height:1px;background:{c["divider"]}"></div></div>'

def login(c, d): return f"""{TOP}<div class="pad" style="margin-top:20px"><div class="h1">登录</div><div class="sec" style="margin-top:8px">立即登录，每日登录领红包🧧</div>
<div class="lab">邮箱/手机号码</div><div class="inp">邮箱/手机号码 (不含国际区号)</div><div class="btn" style="margin-top:28px">登录</div>{OR(c)}{SOCIAL()}
<div style="text-align:center;margin-top:28px" class="lime">创建账户</div></div>"""
def register(c, d): return f"""{TOP}<div class="pad" style="margin-top:20px"><div class="h1">创建帐户</div><div class="sec" style="margin-top:8px">完成注册可获得最高 15,200 USDT</div>
<div class="lab">邮箱/手机号码</div><div class="inp" style="border:1px solid {c['down']};color:{c['textPrimary']}">🇨🇳 +86 ▾&nbsp;&nbsp;1380013</div><div class="dn" style="font-size:12px;margin-top:6px">手机号格式不正确</div>
<div class="lab">邀请码 ▾</div><div class="row" style="gap:8px;font-size:13px;margin-top:14px"><span style="width:16px;height:16px;border:1px solid {c['textTertiary']};border-radius:4px"></span><span>我已阅读并同意 Linbit 的<span class="lime">服务条款</span>与<span class="lime">隐私政策</span></span></div>
<div class="btn" style="margin-top:24px">注册</div>{OR(c)}<div class="btn2" style="margin-top:12px"> 通过 Apple 继续</div><div class="btn2" style="margin-top:12px">G 通过 Google 继续</div>
<div style="text-align:center;margin-top:24px"><span class="sec">已有账户，</span><span class="lime">立即登录</span></div></div>"""
def retention(c, d): return register(c, d) + f"""<div class="mask"></div><div class="sheet" style="text-align:center"><div class="grab"></div><div style="font-size:64px">🎁</div>
<div style="font-size:20px;font-weight:700;margin-top:10px">完成注册最高领 <span class="lime">15,200 USDT</span></div><div class="sec" style="margin-top:8px">1分钟即可完成注册</div>
<div class="row" style="gap:12px;margin-top:24px"><div class="btn2 sp">退出</div><div class="btn sp">继续</div></div></div>"""
def password(c, d): return f"""<div class="row pad" style="height:44px;font-size:22px"><span>←</span><div class="sp"></div><span style="font-size:20px">🎧</span></div><div class="pad" style="margin-top:20px"><div class="h1">请输入密码</div><div class="sec" style="margin-top:8px">ya***@gmail.com</div>
<div class="lab">密码</div><div class="inp" style="border:1px solid {c['textPrimary']};color:{c['textPrimary']}">••••••••<div class="sp"></div><span class="ter">👁</span></div><div class="btn" style="margin-top:28px">下一步</div>
<div style="margin-top:20px"><span class="dash">忘记密码?</span></div></div>"""
def verify(c, d):
    boxes = "".join(f'<div style="width:48px;height:56px;border-radius:10px;background:{c["fill"]};display:flex;align-items:center;justify-content:center;font-size:22px;font-weight:600;{"border:1.5px solid "+c["brand"] if i==3 else ""}">{"385"[i] if i<3 else ""}</div>' for i in range(6))
    return f"""<div class="row pad" style="height:44px;font-size:22px"><span>←</span><div class="sp"></div><span style="font-size:20px">🎧</span></div><div class="pad" style="margin-top:20px"><div class="h1">验证邮箱</div><div class="sec" style="margin-top:8px">请输入邮箱 ya***@gmail.com 收到的6位验证码</div>
<div class="row" style="justify-content:space-between;margin-top:28px">{boxes}</div><div class="row" style="margin-top:14px;font-size:13px"><span class="lime">粘贴</span><div class="sp"></div><span class="ter">重新发送(58s)</span></div>
<div class="btn" style="margin-top:28px;opacity:.4">确认</div><div style="margin-top:20px;font-size:13px"><span class="dash">收不到验证码?</span></div></div>"""
def usercenter(c, d):
    rows = "".join(f'<div class="row" style="height:54px;border-bottom:1px solid {c["divider"]}"><span>{k}</span><div class="sp"></div><span class="sec">{v}</span><span class="ter">&nbsp;›</span></div>' for k,v in [("UID","20918372 ⧉"),("昵称","Yaeko"),("身份认证","去认证"),("账户安全",""),("切换账号","")])
    return f"""<div class="row pad" style="height:44px;font-size:22px"><span>←</span><div class="sp"></div><span style="font-size:18px">⇄</span></div><div style="text-align:center;margin-top:10px"><div style="width:72px;height:72px;border-radius:36px;background:{c['fillStrong']};margin:0 auto;position:relative"><span style="position:absolute;right:-2px;bottom:-2px;font-size:16px">📷</span></div>
<div style="font-size:18px;font-weight:600;margin-top:10px">Yaeko</div><div class="sec" style="font-size:13px;margin-top:4px">ya***@gmail.com</div></div>
<div class="row pad" style="margin-top:20px;gap:22px;font-size:15px"><b>个人资料</b><span class="sec">偏好设置</span><span class="sec">通知设置</span></div><div class="pad" style="margin-top:6px">{rows}<div class="dn" style="text-align:center;margin-top:30px">退出当前账户</div></div>"""
def switch(c, d): return f"""<div class="nav"><i>←</i>账号切换<span style="position:absolute;right:16px;font-size:16px">✎</span></div><div class="pad" style="margin-top:16px">
<div style="border:1.5px solid {c['brand']};border-radius:12px;padding:16px;position:relative" class="row"><div class="coin" style="width:40px;height:40px"></div><div><b>主账户</b><div class="sec" style="font-size:12px;margin-top:4px">ya***@gmail.com</div></div>
<span style="position:absolute;right:0;top:0;background:{c['brand']};color:#111;font-size:11px;padding:3px 8px;border-radius:0 11px 0 8px">当前登录</span></div>
<div class="btn2" style="margin-top:16px">+ 添加账号</div></div>"""
def totp(c, d): return f"""<div class="nav"><i>←</i>设置身份验证器</div><div class="pad"><div style="background:#E8743B1A;color:#E8743B;border-radius:8px;padding:10px 12px;font-size:12px">⚠ 绑定身份验证器后6小时内将无法提现</div>
<div style="margin-top:18px"><b>1</b>&nbsp; 下载并安装身份验证器 App</div><div style="margin-top:18px"><b>2</b>&nbsp; 扫描二维码或复制密钥</div>
<div style="width:150px;height:150px;margin:14px auto;background:repeating-linear-gradient(90deg,#000 0 6px,#fff 6px 12px),#fff;border:8px solid #fff;outline:1px solid {c['border']}"></div>
<div class="inp" style="color:{c['textPrimary']};font-size:13px">JBSWY3DPEHPK3PXP<div class="sp"></div>⧉</div><div style="margin-top:18px"><b>3</b>&nbsp; 输入 6 位验证码</div><div class="inp" style="margin-top:8px">请输入验证码</div><div class="btn" style="margin-top:22px;opacity:.4">确认</div></div>"""

def spot(c, d):
    asks = "".join(f'<div class="row num" style="height:22px;font-size:12px;background:linear-gradient(to left,{c["downTint"]} {w}%,transparent {w}%)"><span class="dn">{p}</span><div class="sp"></div><span>{q}</span></div>' for p,q,w in [("81,077.55","413.4955",90),("81,077.13","1.79K",20),("81,077.12","1.44K",25),("81,076.26","445.91943",60),("81,076.25","45.27K",100)])
    bids = "".join(f'<div class="row num" style="height:22px;font-size:12px;background:linear-gradient(to left,{c["upTint"]} {w}%,transparent {w}%)"><span class="up">{p}</span><div class="sp"></div><span>{q}</span></div>' for p,q,w in [("81,076.24","72.70K",100),("81,076.14","535.10252",50),("81,075.29","1.68K",30),("81,074.52","1.15K",25),("81,074.14","105.39638",15)])
    return f"""<div class="row pad" style="height:44px;gap:20px;font-size:18px"><b>现货</b><span class="sec">策略</span><span class="sec">理财</span><span class="sec">闪兑</span></div>
<div class="row pad"><div><b style="font-size:18px">BTC/USDT ▾</b> <span class="up">•</span><div class="up num" style="font-size:12px">+0.51%</div></div><div class="sp"></div><span style="font-size:17px">☀ &nbsp; ⫼ &nbsp; ⋯</span></div>
<div class="row pad" style="align-items:flex-start;gap:12px;margin-top:8px"><div class="sp">
<div class="row" style="height:32px;border-radius:8px;background:{c['fill']};margin-bottom:10px"><div class="sp" style="height:32px;border-radius:8px;background:{c['up']};color:#fff;display:flex;align-items:center;justify-content:center;font-weight:600">买入</div><div class="sp" style="text-align:center">卖出</div></div>
<div class="box" style="height:34px">ⓘ 限价单<div class="sp"></div>▾</div><div class="box"><div><div class="sec" style="font-size:11px">价格</div><span class="num">81076.25</span></div><div class="sp"></div><span class="sec">USDT</span></div>
<div class="box"><span class="sec">数量</span><div class="sp"></div><span class="sec">BTC</span></div><div class="row" style="justify-content:space-between;margin:6px 0 14px">{"".join('<span style="width:8px;height:8px;transform:rotate(45deg);background:'+(c['textPrimary'] if i==0 else c['fillStrong'])+'"></span>' for i in range(5))}</div>
<div class="box"><span class="sec">交易额</span><div class="sp"></div><span class="sec">USDT</span></div><div class="row" style="gap:6px;font-size:13px"><span style="width:14px;height:14px;border:1px solid {c['textTertiary']};border-radius:3px"></span>止盈/止损</div>
<div class="row num" style="font-size:12px;margin-top:8px"><span class="sec">可用</span><div class="sp"></div>0.00 USDT ⊞</div><div class="row num" style="font-size:12px;margin-top:6px"><span class="sec">可买</span><div class="sp"></div>0.00000 BTC</div><div class="row num" style="font-size:12px;margin-top:6px"><span class="sec">预估手续费</span><div class="sp"></div>0.00</div>
<div class="btn" style="background:{c['up']};color:#fff;height:40px;border-radius:8px;margin-top:10px;font-size:15px">买入 BTC</div></div>
<div style="width:138px"><div class="row sec" style="font-size:11px;margin-bottom:6px"><span>价格<br>(USDT)</span><div class="sp"></div><span style="text-align:right">数量<br>(BTC)</span></div>{asks}
<div style="padding:6px 0"><b class="num" style="font-size:17px">81,076.24</b><div class="sec num" style="font-size:11px">≈ ¥575,641.30</div></div>{bids}
<div class="row num" style="font-size:10px;gap:4px;margin-top:8px"><span class="up">B 61%</span><div class="sp" style="height:3px;background:linear-gradient(90deg,{c['up']} 61%,{c['down']} 61%)"></div><span class="dn">39% S</span></div></div></div>
<div class="row pad" style="margin-top:20px;gap:18px"><b>委托(0) ▾</b><span class="sec">交易机器人(0)</span><div class="sp"></div>🕘</div>
<div class="row pad" style="margin-top:14px;font-size:13px"><span>☐ 只看当前</span><div class="sp"></div><span class="ter" style="background:{c['fill']};padding:3px 10px;border-radius:4px;font-size:12px">全部撤销</span></div>
<div style="text-align:center;margin-top:20px;font-size:13px">📂<div style="margin:8px 0">划转资金，开始现货交易</div><span style="background:{c['fill']};padding:6px 22px;border-radius:8px">充值</span>&nbsp;&nbsp;<span style="background:{c['fill']};padding:6px 22px;border-radius:8px">划转</span></div>{tabbar("现货")}"""
def kindsheet(c, d): return spot(c, d) + f"""<div class="mask"></div><div class="sheet"><div class="grab"></div><b style="font-size:17px">委托类型</b>
{"".join(f'<div class="row" style="height:58px;gap:14px"><span style="width:20px">{i}</span><div><div style="font-size:15px">{t}</div><div class="ter" style="font-size:11px">{s}</div></div><div class="sp"></div>{"◉" if t=="限价单" else ""}</div>' for i,t,s in [("⤵","限价单","以指定价格或更优价格成交"),("⚡","市价单","以当前市场最优价格立即成交"),("⟳","计划委托","价格达到触发价后按设定价格下单"),("Ⓜ","只做 Maker","仅作为挂单方成交，否则自动撤销")])}</div>"""
def confirm(c, d): return spot(c, d) + f"""<div class="mask"></div><div class="sheet"><div class="grab"></div><b style="font-size:17px">买入 BTC/USDT</b><div style="height:12px"></div>
{"".join(f'<div class="row" style="height:30px"><span class="sec">{k}</span><div class="sp"></div><span class="num {cl}">{v}</span></div>' for k,v,cl in [("委托类型","限价单",""),("方向","买入","up"),("委托价格","81076.25 USDT",""),("委托数量","0.01000 BTC",""),("交易额","810.76 USDT",""),("预估手续费","0.0000 BTC","")])}
<div style="margin:10px 0;font-size:13px">☐ 不再提示（可在偏好设置中恢复）</div><div class="btn" style="background:{c['up']};color:#fff">确认买入</div></div>"""
def history(c, d): return f"""<div class="nav"><i>←</i>历史记录</div><div class="row pad" style="gap:22px;font-size:15px;margin-top:6px"><b style="border-bottom:3px solid {c['textPrimary']};padding-bottom:8px">当前委托</b><span class="sec" style="padding-bottom:8px">历史委托</span><span class="sec" style="padding-bottom:8px">成交明细</span></div>
<div class="row pad sec" style="font-size:12px;margin-top:12px;gap:16px"><span>币对 ▾</span><span>委托类型 ▾</span><div class="sp"></div><span class="ter" style="background:{c['fill']};padding:3px 10px;border-radius:4px">全部撤销</span></div>
<div style="text-align:center;margin-top:220px;font-size:40px">📂<div class="sec" style="font-size:12px;margin-top:8px">暂无数据</div></div>"""
def assets(c, d):
    rows = "".join(f'<div class="row pad" style="height:60px"><div class="coin">{a[0]}</div><div><div style="font-size:15px;font-weight:500">{a}</div><div class="sec" style="font-size:12px">{n}</div></div><div class="sp"></div><div style="text-align:right"><div class="num" style="font-weight:500">{v}</div><div class="sec num" style="font-size:12px">≈ ¥{f}</div></div></div>' for a,n,v,f in [("USDT","USDT","1,250.00","8,875.00"),("BTC","BTC","0.0120","6,903.79"),("ETH","ETH","0.5000","9,940.00"),("SOL","SOL","0.00","0.00")])
    acts = "".join(f'<div style="flex:1;text-align:center;font-size:12px"><div style="width:40px;height:40px;border-radius:10px;background:{c["fill"]};margin:0 auto 8px;display:flex;align-items:center;justify-content:center">{i}</div>{t}</div>' for i,t in [("↙","充值"),("▭","快捷买币"),("↗","提现"),("⇄","划转")])
    return f"""<div class="row pad" style="height:44px;gap:18px;font-size:18px"><span class="sec">总览</span><b>资金</b><span class="sec">合约</span><span class="sec">现货</span><span class="sec">理财</span><span class="sec">策略</span><span class="sec">TradFi</span></div><div style="height:1px;background:{c['divider']}"></div>
<div class="pad" style="margin-top:20px"><div class="row sec" style="font-size:13px">总资产估值 👁<div class="sp"></div><span style="color:{c['textPrimary']}">🗎</span></div><div style="margin-top:6px"><b class="num" style="font-size:32px">2,483.66</b> USDT ▾</div><div class="sec num" style="font-size:12px">≈ ¥17,634.00</div>
<div class="row num" style="font-size:12px;margin-top:8px;gap:8px"><span class="sec">可用余额</span>1,250.00 USDT</div></div><div class="row" style="margin-top:20px;padding:0 8px">{acts}</div>
<div style="height:8px;background:{c['fill']};margin-top:20px"></div><div class="row pad" style="height:56px"><b style="font-size:17px">资产</b><div class="sp"></div><span class="sec">⌕ &nbsp; ⚟</span></div>{rows}{tabbar("资产")}"""
def withdraw(c, d): return assets(c, d) + f"""<div class="mask"></div><div class="sheet"><div class="grab"></div><b style="font-size:17px">提现</b><div style="height:14px"></div>
<div class="card row" style="padding:16px;gap:14px"><span>↗</span><div><b>链上提币</b><div class="sec" style="font-size:12px;margin-top:3px">往钱包或交易所转出数字货币</div></div></div>
<div class="card row" style="padding:16px;gap:14px;position:relative"><span>⇄</span><div><b>内部转账</b><div class="sec" style="font-size:12px;margin-top:3px">立即向其他 Linbit 用户转出数字货币</div></div><span class="tag lime" style="position:absolute;right:10px;top:10px;border:1px solid {c['brand']}">0 手续费</span></div></div>"""
def deposit(c, d): return f"""<div class="nav"><i>←</i>充值<span style="position:absolute;right:16px;font-size:16px">🗎</span></div><div class="pad"><div class="inp" style="color:{c['textPrimary']};height:44px"><span class="coin" style="width:22px;height:22px;margin-right:8px">T</span>USDT<div class="sp"></div>▾</div>
<div style="width:190px;height:190px;margin:16px auto;background:#fff;border-radius:12px;padding:11px"><div style="width:168px;height:168px;background:repeating-conic-gradient(#000 0 25%,#fff 0 50%) 0 0/14px 14px"></div></div>
<div style="background:{c['fill']};border-radius:12px;padding:14px"><div class="sec" style="font-size:11px">网络</div><div class="row" style="font-weight:500;margin-top:4px">Tron (TRC20)<div class="sp"></div>⇄</div><div style="height:1px;background:{c['divider']};margin:14px 0"></div>
<div class="sec" style="font-size:11px">充值地址</div><div class="row" style="margin-top:4px;gap:12px"><span class="num" style="font-weight:500;word-break:break-all">TMaaP1EZXC9C93NsQJGL18ZpcxhnU8BDUd</span><span class="btn" style="height:28px;border-radius:8px;padding:0 14px;font-size:12px">复制</span></div></div>
{"".join(f'<div class="row" style="margin-top:12px;font-size:13px"><span class="sec">{k}</span><div class="sp"></div><b style="font-weight:500">{v}</b></div>' for k,v in [("充值后自动划转至","资金账户 ▾"),("最小充值","1 USDT"),("充值确认","10次网络确认")])}
<div class="ter" style="font-size:11px;margin-top:12px">* 请勿向上述地址充值 USDT 以外的资产，否则资产将不可找回</div></div><div class="btn" style="position:absolute;left:16px;right:16px;bottom:34px">保存并分享地址</div>"""
def network(c, d):
    cards = "".join(f'<div class="card"><div class="row" style="gap:6px"><b>{n}</b>{b}</div><div class="sec num" style="font-size:12px;margin-top:6px">手续费 {f} USDT</div><div class="sec" style="font-size:12px;margin-top:4px">预计到账时间 ≈ {m} 分钟</div></div>' for n,b,f,m in [("Arbitrum One",f'<span class="tag lime" style="background:{c["brand"]}1F">最低</span><span class="tag lime" style="background:{c["brand"]}1F">推荐</span>',"0.1","2"),("BNB Smart Chain (BEP20)","","0.3","3"),("Tron (TRC20)","","1","3"),("Ethereum (ERC20)","","3","5")])
    return deposit(c, d) + f"""<div class="mask"></div><div class="sheet"><div class="grab"></div><div class="row"><b style="font-size:17px">选择网络</b><div class="sp"></div>✕</div>
<div class="sec" style="font-size:12px;margin:12px 0">ⓘ 请确保您选择的网络与提币平台使用的网络一致，否则可能导致资产丢失。</div>{cards}</div>"""
def transfer(c, d): return f"""<div class="nav"><i>←</i>划转<span style="position:absolute;right:16px;font-size:16px">🗎</span></div><div class="pad" style="position:relative">
<div class="inp" style="height:60px;color:{c['textPrimary']};margin-bottom:8px"><span class="ter" style="font-size:12px;margin-right:12px">从</span><div><div style="font-size:14px;font-weight:500">资金账户</div><div class="ter" style="font-size:11px">USDT</div></div><div class="sp"></div>▾</div>
<div class="inp" style="height:60px;color:{c['textPrimary']}"><span class="ter" style="font-size:12px;margin-right:12px">到</span><div><div style="font-size:14px;font-weight:500">合约账户</div><div class="ter" style="font-size:11px">USDT</div></div><div class="sp"></div>▾</div>
<div style="position:absolute;left:50%;top:53px;transform:translateX(-50%);width:30px;height:30px;border-radius:15px;background:{c['bg']};border:1px solid {c['border']};display:flex;align-items:center;justify-content:center;font-size:12px">⇅</div>
<div class="lab" style="font-weight:400;font-size:13px">币种</div><div class="inp" style="color:{c['textPrimary']}"><span class="coin" style="width:22px;height:22px;margin-right:8px">T</span>USDT&nbsp;<span class="ter" style="font-size:12px">Tether USDT</span><div class="sp"></div>▾</div>
<div class="lab" style="font-weight:400;font-size:13px">划转数量</div><div class="inp">0<div class="sp"></div><span style="color:{c['textPrimary']}">USDT</span>&nbsp;&nbsp;<span class="lime">最大</span></div>
<div class="row num" style="font-size:12px;margin-top:10px"><span class="sec">可用</span><div class="sp"></div>1,250.00 USDT</div></div><div class="btn" style="position:absolute;left:16px;right:16px;bottom:34px;opacity:.4">确认划转</div>"""

PAGES = [("login",login,"/workspace/ref2/3.jpg"),("register_error",register,"/workspace/ref2/5.jpg"),("register_retention",retention,"/workspace/ref2/8.jpg"),
 ("password",password,"/workspace/ref2/10.jpg"),("verify",verify,"/workspace/ref2/11.jpg"),("user_center",usercenter,"/workspace/ref2/1.jpg"),
 ("account_switch",switch,"/workspace/ref2/2.jpg"),("totp",totp,"/workspace/ref2/15.jpg"),("spot",spot,"/workspace/ref/28.jpg"),
 ("spot_order_type",kindsheet,"/workspace/ref/19.jpg"),("spot_confirm",confirm,"/workspace/ref/28.jpg"),("spot_history",history,"/workspace/ref/23.jpg"),
 ("assets_funding",assets,"/workspace/ref/42.jpg"),("assets_withdraw_sheet",withdraw,"/workspace/ref2/16.jpg"),("deposit",deposit,"/workspace/ref/37.jpg"),
 ("deposit_network",network,"/workspace/ref2/18.jpg"),("transfer",transfer,"/workspace/ref/46.jpg")]
fp = next((f for f in ["/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc","/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc"] if os.path.exists(f)), None)
font = ImageFont.truetype(fp, 42) if fp else ImageFont.load_default()
for name, fn, ref in PAGES:
    pngs = []
    for mode in ["light","dark"]:
        c = T["color"][mode]; html = f"{OUT}/_{name}_{mode}.html"; png = f"{OUT}/{name}_{mode}_sim.png"
        open(html,"w").write(f'<html><head><meta charset="utf-8"><style>{css(c, mode=="dark")}</style></head><body>{SB}{fn(c, mode=="dark")}</body></html>')
        subprocess.run(["google-chrome","--headless=new","--no-sandbox","--disable-gpu","--hide-scrollbars","--window-size=390,844",
                        "--force-device-scale-factor=3",f"--screenshot={png}",f"file://{html}"],check=True,capture_output=True)
        pngs.append(png)
    W=1170; out = Image.new("RGB",(W*3+120,2532+110),"#d0d0d0"); d = ImageDraw.Draw(out)
    for i,(p,lab) in enumerate([(ref,f"参考图 {ref.split('/')[-2]}/{os.path.basename(ref)}"),(pngs[0],"Linbit 日间"),(pngs[1],"Linbit 夜间")]):
        out.paste(Image.open(p).convert("RGB").resize((W,2532)),(i*(W+60),110)); d.text((i*(W+60)+20,30),lab,fill="#000",font=font)
    out.save(f"{OUT}/{name}_compare.png")
    print(name)
