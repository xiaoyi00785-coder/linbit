"""HTML 模拟预览：与 SwiftUI 同一套 tokens / SVG / 布局数值，实时拉测试网数据，Chrome 渲染 390x844@3x。"""
import json, os, re, subprocess, urllib.request, datetime
from decimal import Decimal
from PIL import Image, ImageDraw, ImageFont

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
T = json.load(open(f"{ROOT}/tokens/tokens.json"))
OUT = f"{ROOT}/previews"; os.makedirs(OUT, exist_ok=True)
API = "https://api.cowa.click"

def get(p):
    try:
        with urllib.request.urlopen(urllib.request.Request(API + p, headers={"User-Agent": "Linbit-iOS/0.1", "Accept": "application/json"}), timeout=15) as r: return json.loads(r.read())
    except Exception as e: return {"_err": str(e)}

fut = get("/futures/market/ticker").get("tickers", [])
spot = get("/market/ticker"); spot = spot if isinstance(spot, list) else []
earn = [e for e in get("/earn/products").get("items", []) if Decimal(e.get("aprMax") or e.get("apr") or "0") > 0]
ann = get("/announcements")
FETCHED = datetime.datetime.now().strftime("%Y-%m-%d %H:%M")

def D(s):
    try: return Decimal(s or "0")
    except Exception: return Decimal(0)

def row_f(t):
    last = D(t.get("lastPrice"))
    if last == 0: last = D(t.get("markPrice"))
    return dict(sym=t["symbol"], base=t["symbol"][:-4], last=last, chg=D(t.get("priceChangePercent24h")), vol=D(t.get("turnover24h")))
FUT = [row_f(t) for t in fut]

def price(d):
    a = abs(d); p = 2 if a >= 1000 else 4 if a >= 1 else 5 if a >= Decimal("0.01") else 8
    s = f"{d:,.{p}f}"
    while p > 2 and s.endswith("0"): s = s[:-1]; p -= 1
    return s
def pct(d): return ("+" if d >= 0 else "") + f"{d:.2f}%"
def volcn(d):
    if d >= 10**8: return f"{d/10**8:.2f}亿"
    if d >= 10**4: return f"{d/10**4:.2f}万"
    return f"{d:.2f}"
def fiat(d): return "¥" + f"{d*Decimal('7.10'):,.2f}"

def svg(name, size, color, dark):
    s = open(f"{ROOT}/icons/{name}.svg").read()
    if dark: s = s.replace('"#fff"', '"#1E1E20"').replace("'#fff'", "'#1E1E20'")
    s = s.replace("currentColor", color)
    s = re.sub(r"<svg ", f'<svg width="{size}" height="{size}" ', s, count=1)
    return s

def page(body, c, dark, tab):
    tabs = [("home","首页"),("market","行情"),("futures","合约"),("spot","现货"),("assets","资产")]
    tb = "".join(f'<div class="ti {"sel" if k==tab else ""}">{svg("tab-"+k,22,c["textPrimary"] if k==tab else c["textSecondary"],dark)}<span>{n}</span></div>' for k,n in tabs)
    css = f"""
*{{box-sizing:border-box;margin:0;padding:0}} body{{width:390px;height:844px;overflow:hidden;background:{c['bg']};color:{c['textPrimary']};
font-family:-apple-system,"PingFang SC","Noto Sans CJK SC","Noto Sans SC",sans-serif;-webkit-font-smoothing:antialiased;position:relative}}
.num{{font-variant-numeric:tabular-nums}} .sb{{height:47px;display:flex;align-items:center;justify-content:space-between;padding:0 30px 0 34px;font-weight:600;font-size:16px}}
.row{{display:flex;align-items:center}} .sp{{flex:1}} .sec{{color:{c['textSecondary']}}} .ter{{color:{c['textTertiary']}}}
.div8{{height:8px;background:{c['fill']}}}
.tabbar{{position:absolute;left:21px;right:21px;bottom:21px;height:61px;border-radius:31px;background:{c['tabBarBg'][:7]}E6;border:1px solid {c['tabBarBorder']};
box-shadow:0 4px 18px rgba(0,0,0,{'.5' if dark else '.08'});display:flex;align-items:center;padding:0 5px;backdrop-filter:blur(20px)}}
.ti{{flex:1;height:51px;border-radius:26px;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:2px;font-size:11px;color:{c['textSecondary']}}}
.ti.sel{{background:{c['tabBarSelected']};color:{c['textPrimary']};font-weight:600}}
.badge{{width:76px;height:31px;border-radius:6px;color:#fff;font-size:15px;font-weight:500;display:flex;align-items:center;justify-content:center;margin-left:12px}}
.chip{{height:24px;padding:0 10px;border-radius:6px;font-size:14px;display:flex;align-items:center;color:{c['textSecondary']}}} .chip.sel{{background:{c['fill']};color:{c['textPrimary']};font-weight:500}}
.mrow{{height:56px;padding:0 16px;display:flex;align-items:center}}
.coin{{width:32px;height:32px;border-radius:16px;margin-right:10px;background:{c['fillStrong']};display:flex;align-items:center;justify-content:center;font-size:14px;font-weight:600;overflow:hidden}}
.coin img{{width:32px;height:32px}}
.ptab{{font-size:18px;color:{c['textSecondary']};margin-right:22px}} .ptab.sel{{color:{c['textPrimary']};font-weight:600}}
.stab{{font-size:15px;color:{c['textSecondary']};margin-right:24px;padding-bottom:9px;position:relative}} .stab.sel{{color:{c['textPrimary']};font-weight:600}}
.stab.sel:after{{content:"";position:absolute;left:50%;transform:translateX(-50%);bottom:0;width:16px;height:3px;border-radius:2px;background:{c['textPrimary']}}}
"""
    sb = f'<div class="sb num"><span>9:41</span><span style="font-size:13px">▮▮▮ ◔ ▭</span></div>'
    return f'<html><head><meta charset="utf-8"><style>{css}</style></head><body>{sb}{body}<div class="tabbar">{tb}</div></body></html>'

def badge(c, d): return f'<div class="badge num" style="background:{c["up"] if d>=0 else c["down"]}">{pct(d)}</div>'
def coin(base): return f'<div class="coin">{base[0]}<img src="https://assets.coincap.io/assets/icons/{base.lower()}@2x.png" onerror="this.remove()" style="position:absolute"></div>'

def home(c, dark):
    I = lambda n, s=22, col=None: svg(n, s, col or c["textPrimary"], dark)
    grid = [("g-chat","聊天室"),("g-gift","福利中心"),("g-earn","理财"),("g-tradfi","TradFi"),("g-copy","跟单"),("g-bot","策略交易"),("g-more","更多")]
    g = "".join(f'<div style="width:25%;display:flex;flex-direction:column;align-items:center;gap:6px;margin-bottom:18px"><div style="width:40px;height:40px;border-radius:10px;background:{c['iconTileBg']};display:flex;align-items:center;justify-content:center">{I(n,24)}</div><span style="font-size:13px">{t}</span></div>' for n,t in grid)
    hot = sorted(FUT, key=lambda r: r["vol"], reverse=True)[:5]
    rows = "".join(f'<div class="mrow"><div><div style="font-size:17px;font-weight:500">{r["sym"]}</div><div class="sec num" style="font-size:13px;margin-top:3px">{volcn(r["vol"])}</div></div><div class="sp"></div><div style="text-align:right"><div class="num" style="font-size:17px;font-weight:500">{price(r["last"])}</div><div class="sec num" style="font-size:12px;margin-top:3px">{fiat(r["last"])}</div></div>{badge(c,r["chg"])}</div>' for r in hot)
    e = earn[0] if earn else None
    etext = f'{e["title"]}，{D(e.get("aprMax") or e["apr"])*100:.2f}%APR' if e else ""
    body = f"""
<div class="row" style="height:44px;padding:0 16px;gap:14px">{svg("avatar",28,c["textPrimary"],dark)}
 <div class="row" style="flex:1;height:31px;border-radius:8px;background:{c['fill']};padding:0 10px;gap:6px">{I("search",15,c["textTertiary"])}<span class="ter" style="font-size:14px">BTC</span></div>
 {I("scan")}{I("headset")}{I("bell")}</div>
<div style="padding:14px 16px 0">
 <div class="row sec" style="font-size:13px;gap:4px">总资产估值 {I("eye",14,c["textSecondary"])}</div>
 <div class="row" style="align-items:baseline;margin-top:6px"><span class="num" style="font-size:40px;font-weight:700">0.00</span>
  <span style="font-size:14px;font-weight:500;margin-left:6px">USDT</span>{I("caret-down",10)}<div class="sp"></div>
  <div style="width:80px;height:32px;border-radius:8px;background:{c['primary']};color:{c['textOnPrimary']};font-size:14px;font-weight:500;display:flex;align-items:center;justify-content:center;align-self:center">充值</div></div>
 <div class="sec num" style="font-size:13px;margin-top:6px">≈ ¥0.00</div></div>
<div class="row" style="margin:18px 16px 0;height:44px;border:1px solid {c['border']};border-radius:8px;padding:0 12px;font-size:13px">完成新手任务，可获得&nbsp;<b style="color:{c['brand']}">215 USDT</b>&nbsp;体验金<div class="sp"></div>{I("chevron-right",14,c["textSecondary"])}</div>
<div class="row" style="margin:12px 16px 0;height:60px;border-radius:10px;background:{c['fill']};padding:0 14px"><div><div style="font-size:15px;font-weight:600">全员红包雨火爆来袭</div><div class="sec" style="font-size:12px;margin-top:3px">惊喜好礼等你拿</div></div><div class="sp"></div>{I("g-gift",40)}</div>
<div class="row" style="flex-wrap:wrap;padding:22px 16px 2px">{g}</div>
<div class="div8"></div>
<div class="row" style="height:104px;padding:16px;position:relative"><div><div class="sec" style="font-size:13px">数字资产 · 全球消费</div><div style="font-size:20px;font-weight:700;margin:6px 0">Linbit Card</div><div style="font-size:12px" class="row">查看详情 {I("arrow-right",12)}</div></div><div class="sp"></div>
 <div style="width:110px;height:70px;position:relative"><div style="position:absolute;left:8px;top:14px;width:74px;height:46px;border-radius:6px;background:{c['brand']};transform:rotate(-12deg)"></div><div style="position:absolute;left:28px;top:8px;width:74px;height:46px;border-radius:6px;background:{c['primary']};transform:rotate(8deg);padding:6px">{svg("logo",16,c["bg"],dark)}</div></div>
 <span class="ter num" style="position:absolute;right:16px;bottom:10px;font-size:10px">1/2</span></div>
<div class="div8"></div>
<div class="row" style="height:48px;padding:0 16px;gap:8px"><span style="font-size:15px;font-weight:600">理财宝</span><span class="sec" style="font-size:13px">{etext}</span><div class="sp"></div><span class="ter num" style="font-size:10px">1/{max(len(earn),1)}</span></div>
<div class="div8"></div>
<div class="row" style="padding:16px 16px 0"><span class="ptab" style="font-size:16px">自选</span><span class="ptab sel" style="font-size:16px">热门</span><span class="ptab" style="font-size:16px">涨幅榜</span><span class="ptab" style="font-size:16px">跌幅榜</span><span class="ptab" style="font-size:16px">成交额榜</span></div>
<div class="row" style="padding:12px 16px;gap:8px"><div class="chip">现货</div><div class="chip sel">合约</div></div>
<div class="row sec" style="font-size:12px;height:24px;padding:0 16px">交易对<div class="sp"></div>最新价<span style="width:88px;text-align:right">涨跌幅</span></div>
{rows}"""
    return page(body, c, dark, "home")

def markets(c, dark):
    I = lambda n, s=22, col=None: svg(n, s, col or c["textPrimary"], dark)
    rows = "".join(f'<div class="mrow">{coin(r["base"])}<div><div><span style="font-size:16px;font-weight:500">{r["base"]}</span><span class="sec" style="font-size:13px"> / USDT</span></div><div class="sec num" style="font-size:13px;margin-top:3px">{volcn(r["vol"])}</div></div><div class="sp"></div><div style="text-align:right"><div class="num" style="font-size:16px;font-weight:500">{price(r["last"])}</div><div class="sec num" style="font-size:12px;margin-top:3px">{fiat(r["last"])}</div></div>{badge(c,r["chg"])}</div>' for r in FUT)
    chips = "".join(f'<div class="chip {"sel" if i==0 else ""}">{n}</div>' for i,n in enumerate(["全部","公链","股票代币","Meme","DeFi","Solana 生态"]))
    srt = lambda t: f'<span class="row" style="gap:2px">{t}{I("sort",10,c["textTertiary"])}</span>'
    body = f"""
<div class="row" style="height:44px;padding:0 16px"><span class="ptab">自选</span><span class="ptab sel">市场</span><span class="ptab">聊天室</span><div class="sp"></div>{I("search")}
 <span style="position:relative;margin-left:18px">{I("more")}<i style="position:absolute;right:-3px;top:-2px;width:6px;height:6px;border-radius:3px;background:{c['badgeDot']}"></i></span></div>
<div style="height:1px;background:{c['divider']}"></div>
<div class="row" style="padding:10px 16px 0"><span class="stab">现货</span><span class="stab sel">合约</span><span class="stab">TradFi</span></div>
<div style="height:1px;background:{c['divider']}"></div>
<div class="row" style="padding:10px 16px;gap:6px;white-space:nowrap;overflow:hidden">{chips}</div>
<div class="row sec" style="font-size:12px;height:30px;padding:0 16px">{srt("币种/成交额")}<div class="sp"></div>{srt("最新价")}<span style="width:88px;display:flex;justify-content:flex-end">{srt("涨跌幅")}</span></div>
{rows}"""
    return page(body, c, dark, "market")

shots = {}
for name, fn in [("home", home), ("markets", markets)]:
    for mode in ["light", "dark"]:
        html = f"{OUT}/_{name}_{mode}.html"; png = f"{OUT}/{name}_{mode}_sim.png"
        open(html, "w").write(fn(T["color"][mode], mode == "dark"))
        subprocess.run(["google-chrome", "--headless=new", "--no-sandbox", "--disable-gpu", "--hide-scrollbars",
                        "--window-size=390,844", "--force-device-scale-factor=3", "--virtual-time-budget=6000",
                        f"--screenshot={png}", f"file://{html}"], check=True, capture_output=True)
        shots[(name, mode)] = png

REF = {"home": "/workspace/ref/1.jpg", "markets": "/workspace/ref/9.jpg"}
fp = next((f for f in ["/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc", "/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc"] if os.path.exists(f)), None)
font = ImageFont.truetype(fp, 42) if fp else ImageFont.load_default()
for (name, mode), png in shots.items():
    a = Image.open(REF[name]).convert("RGB").resize((1170, 2532)); b = Image.open(png).convert("RGB").resize((1170, 2532))
    W = 1170 * 2 + 60; out = Image.new("RGB", (W, 2532 + 110), "#d0d0d0"); d = ImageDraw.Draw(out)
    out.paste(a, (0, 110)); out.paste(b, (1170 + 60, 110))
    d.text((20, 30), f"参考图 {os.path.basename(REF[name])}", fill="#000", font=font)
    d.text((1230, 30), f"Linbit 模拟（{'日间' if mode=='light' else '夜间'}）· 测试网数据 {FETCHED}", fill="#000", font=font)
    out.save(f"{OUT}/{name}_{mode}_compare.png")
print("announcements:", str(ann)[:120]); print(sorted(os.listdir(OUT)))
