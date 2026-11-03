// 全部为演示数据（Mock），不含任何真实用户信息
let s = 42; export const rnd = () => (s = (s * 16807) % 2147483647) / 2147483647
export const pick = a => a[Math.floor(rnd() * a.length)]
export const ri = (a, b) => Math.floor(a + rnd() * (b - a + 1))
export const fmt = (n, d = 2) => Number(n).toLocaleString('en-US', { minimumFractionDigits: d, maximumFractionDigits: d })
const pad = n => String(n).padStart(2, '0')
export const ts = (minAgo) => { const d = new Date(Date.UTC(2026, 10, 2, 14, 0) - minAgo * 60000); return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())} ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}` }
export const DEMO = { nick: 'lb_Demo8K2', uid: '80000001', email: 'demo***@example.com', invite: 'LB8DEMO' }
const uid = i => String(80000001 + i)
const nick = i => i === 0 ? DEMO.nick : 'lb_Demo' + (1000 + i * 37).toString(36).toUpperCase()
const mail = i => i === 0 ? DEMO.email : `demo${i}***@example.com`
export const COUNTRIES = ['新加坡', '日本', '韩国', '越南', '泰国', '马来西亚', '菲律宾', '印尼', '巴西', '土耳其']
export const NETS = ['TRC20', 'ERC20', 'BEP20', 'Arbitrum', 'Solana']
// 平台提现手续费 = 0；此处为各网络链上 Gas 实时估算（USDT）
export const NETFEE = { TRC20: 2, ERC20: 10, BEP20: 1, Arbitrum: 1, Solana: 1 }
export const NETNAME = { TRC20: 'TRON', ERC20: 'Ethereum', BEP20: 'BNB Chain', Arbitrum: 'Arbitrum', Solana: 'Solana' }

export const users = Array.from({ length: 86 }, (_, i) => ({
  id: uid(i), uid: uid(i), nick: nick(i), email: mail(i), phone: i % 3 ? `+65 ****${pad(ri(10, 99))}${pad(ri(10, 99))}` : '-',
  country: pick(COUNTRIES), kyc: i === 0 ? 'L2 高级' : pick(['未认证', 'L1 基础', 'L1 基础', 'L2 高级', '审核中']),
  vip: i === 0 ? '合伙人·银牌' : pick(['普通用户', '普通用户', '普通用户', '普通用户', '合伙人·普通', '带单员']),
  status: i === 0 ? '正常' : pick(['正常', '正常', '正常', '正常', '冻结', '限制提现']),
  risk: pick(['低', '低', '低', '中', '高']), asset: +(rnd() * 120000).toFixed(2), reg: ts(ri(600, 200000)),
  last: ts(ri(1, 3000)), inviter: i % 4 === 0 ? '80000001' : '-', tfa: rnd() > .3, agent: i % 9 === 0,
}))

export const kycs = Array.from({ length: 34 }, (_, i) => ({
  id: 'KYC' + (260000 + i), uid: uid(i * 2 + 1), nick: nick(i * 2 + 1), level: pick(['L1 基础', 'L2 高级']),
  doc: pick(['护照', '身份证', '驾驶证']), country: pick(COUNTRIES), name: 'Demo User ' + (i + 1),
  submit: ts(ri(5, 3000)), face: ri(72, 99), ocr: pick(['一致', '一致', '一致', '不一致']), dup: rnd() > .9,
  aml: pick(['无命中', '无命中', '无命中', '疑似命中']), status: i < 14 ? '待审核' : pick(['已通过', '已通过', '已驳回', '复审中']), reviewer: i < 14 ? '-' : pick(['ops_lin', 'ops_wang', 'kyc_chen']),
}))

export const withdraws = Array.from({ length: 58 }, (_, i) => {
  const net = pick(NETS), coin = net === 'Solana' ? pick(['USDT', 'SOL']) : pick(['USDT', 'USDT', 'USDT', 'BTC', 'ETH'])
  const amt = coin === 'USDT' ? ri(12, 20000) : coin === 'BTC' ? +(rnd() * 0.8).toFixed(4) : +(rnd() * 12).toFixed(3)
  const risk = ri(5, 95)
  return { id: 'WD' + (90310000 + i), uid: uid(ri(0, 85)), coin, net, amt, fee: NETFEE[net], addr: 'T' + Math.random().toString(36).slice(2, 8) + '…' + Math.random().toString(36).slice(2, 6),
    risk, level: risk > 70 ? '高' : risk > 40 ? '中' : '低', time: ts(ri(1, 2000)),
    status: i < 16 ? (risk > 40 ? '待复核' : '待初审') : pick(['已完成', '已完成', '已完成', '已驳回', '广播中', '待二审']),
    first: i < 16 ? '-' : 'fin_zhao', second: '-', whitelist: rnd() > .5, newAddr: rnd() > .7 }
})
export const deposits = Array.from({ length: 64 }, (_, i) => { const net = pick(NETS); return {
  id: 'DP' + (70120000 + i), uid: uid(ri(0, 85)), coin: pick(['USDT', 'USDT', 'BTC', 'ETH', 'SOL']), net, amt: +(rnd() * 8000).toFixed(2),
  conf: `${ri(1, 20)}/${net === 'ERC20' ? 12 : 20}`, txid: '0x' + Math.random().toString(16).slice(2, 10) + '…', time: ts(ri(1, 3000)),
  status: pick(['已入账', '已入账', '已入账', '确认中', '待人工', '已退回']) } })

export const ledger = Array.from({ length: 120 }, (_, i) => ({
  id: 'LG' + (5000000 + i), uid: uid(ri(0, 20)), acct: pick(['现货账户', '合约账户', '跟单账户', '理财账户']), coin: pick(['USDT', 'USDT', 'BTC', 'ETH']),
  biz: pick(['充值', '提现', '划转', '现货成交', '手续费', '资金费', '强平', '跟单分润', '活动奖励', '体验金回收']),
  dir: rnd() > .5 ? '借' : '贷', amt: +(rnd() * 3000).toFixed(2), bal: +(rnd() * 50000).toFixed(2), ref: 'REF' + ri(100000, 999999), time: ts(i * 7 + ri(0, 6)) }))

export const SYMS = ['BTC', 'ETH', 'SOL', 'BNB', 'XRP', 'DOGE', 'TON', 'ADA', 'AVAX', 'LINK', 'XAU', 'TSLA', 'NVDA', 'AAPL']
const PX = { BTC: 68420, ETH: 3420, SOL: 162, BNB: 590, XRP: .52, DOGE: .14, TON: 6.1, ADA: .45, AVAX: 31, LINK: 14.2, XAU: 2380, TSLA: 248, NVDA: 122, AAPL: 226 }
export const spotOrders = Array.from({ length: 70 }, (_, i) => { const c = pick(SYMS.slice(0, 10)); return {
  id: 'SO' + (88000000 + i), uid: uid(ri(0, 85)), sym: c + '/USDT', side: rnd() > .5 ? '买入' : '卖出', type: pick(['限价', '市价', '限价', '止盈止损']),
  px: PX[c] * (0.98 + rnd() * .04), qty: +(rnd() * 5).toFixed(4), filled: ri(0, 100), status: pick(['已成交', '部分成交', '挂单中', '已撤销']), time: ts(ri(1, 2000)) } })
export const positions = Array.from({ length: 60 }, (_, i) => { const c = pick(SYMS); const lev = pick([5, 10, 20, 50, 100, 125, 200]); const entry = PX[c]
  const mark = entry * (0.96 + rnd() * .08); return {
  id: 'PS' + (66000000 + i), uid: uid(ri(0, 85)), sym: c + 'USDT', side: rnd() > .5 ? '多' : '空', lev, mode: pick(['全仓', '逐仓']), size: +(rnd() * 40000).toFixed(0),
  entry, mark, upnl: +((rnd() - .45) * 3000).toFixed(2), mr: +(rnd() * 95).toFixed(1), liq: entry * (1 - 0.9 / lev), time: ts(ri(1, 5000)) } })

export const coins = ['USDT', 'BTC', 'ETH', 'SOL', 'BNB', 'XRP', 'DOGE', 'TON', 'ADA', 'TRX', 'LINK', 'AVAX'].map((c, i) => ({
  id: c, coin: c, name: c, nets: c === 'USDT' ? ['TRC20', 'ERC20', 'BEP20', 'Arbitrum', 'Solana'] : [pick(NETS)], dep: i !== 9 || true, wd: i !== 7,
  min: c === 'USDT' ? 12 : +(rnd()).toFixed(3), max: c === 'USDT' ? 20000 : ri(10, 500), conf: ri(12, 64), status: i === 11 ? '待上线' : '已上线', sort: i + 1 }))
export const pairs = Array.from({ length: 40 }, (_, i) => { const c = SYMS[i % 10] + (i >= 10 ? ['', 'X', 'Y', 'Z'][Math.floor(i / 10)] : '')
  return { id: c + '/USDT', sym: c + '/USDT', tick: pick(['0.01', '0.001', '0.0001']), lot: pick(['0.0001', '0.001', '0.1']), minNotional: 5,
    maker: '0.1%', taker: '0.1%', status: i > 36 ? '待上线' : i === 20 ? '暂停交易' : '交易中', zone: pick(['主板', '主板', '创新区', 'Meme']), vol: ri(10, 900000) * 100 } })
export const futures = Array.from({ length: 30 }, (_, i) => { const c = SYMS[i % SYMS.length]; const cls = ['XAU'].includes(c) ? '黄金' : ['TSLA', 'NVDA', 'AAPL'].includes(c) ? '美股' : '加密'
  return { id: c + 'USDT' + (i >= SYMS.length ? '-' + i : ''), sym: c + 'USDT', cls, maxLev: c === 'BTC' || c === 'ETH' ? 200 : cls === '美股' ? 20 : pick([50, 75, 100, 125, 20, 10]),
    maker: '0.05%', taker: '0.05%', fundInt: '8h', fundCap: '±0.75%', oiCap: ri(5, 500) + 'M', status: i === 7 ? '仅减仓' : '交易中' } })
export const riskTiers = [[50e4, 200, .25], [100e4, 150, .35], [250e4, 100, .5], [500e4, 75, .75], [1000e4, 50, 1], [2000e4, 25, 2], [3000e4, 20, 2.5], [5000e4, 10, 5], [8000e4, 6, 8.33], [1e8, 5, 10], [1.5e8, 4, 12.5], [2e8, 1, 50]]
  .map(([cap, lev, mmr], i) => ({ id: i + 1, tier: i + 1, cap, lev, mmr, imr: +(100 / lev).toFixed(2) }))


export const campaigns = [
  ['新人任务 · 215 USDT 礼包', '新人任务', '进行中', 215, 18420, 128900],
  ['合约交易赛 S3', '交易赛', '进行中', 50000, 3120, 31200], ['邀请返佣 · 长期', '邀请返佣', '进行中', 0, 9620, 88210],
  ['黄金合约 0 手续费周', '手续费优惠', '已结束', 20000, 2210, 19820], ['美股合约体验金', '体验金', '待开始', 30000, 0, 0],
  ['签到积分 · 11 月', '积分', '进行中', 0, 40210, 0], ['网格策略挑战', '交易赛', '草稿', 10000, 0, 0]]
  .map(([n, t, s, b, p, u], i) => ({ id: 'CP' + (100 + i), name: n, type: t, status: s, budget: b, part: p, used: u, start: ts(ri(4000, 40000)), end: ts(-ri(1000, 40000)) }))
export const rewards = Array.from({ length: 50 }, (_, i) => ({ id: 'RW' + (300000 + i), uid: uid(ri(0, 85)), kind: pick(['体验金', '手续费抵扣券', '现金券', 'USDT 空投', '积分']),
  amt: pick([5, 10, 20, 50, 100]), src: pick(['新人任务', '合约交易赛 S3', '签到积分 · 11 月', '人工补发']), status: pick(['已发放', '已发放', '已使用', '已过期', '已回收', '风控拦截']), time: ts(ri(1, 9000)) }))
export const pointsItems = [['10 USDT 手续费抵扣券', 1000, 500, 213], ['5 USDT 体验金', 600, 1000, 820], ['50 USDT 合约体验金', 4500, 100, 31], ['linbit 周边卫衣', 12000, 20, 4], ['20 USDT 现金券', 3000, 200, 58]]
  .map(([n, p, st, sold], i) => ({ id: 'PI' + i, name: n, cost: p, stock: st, sold, on: i !== 3 }))
export const traders = Array.from({ length: 28 }, (_, i) => ({ id: 'TR' + (2000 + i), uid: uid(i * 3), nick: nick(i * 3), roi: +((rnd() - .2) * 260).toFixed(2), pnl: +((rnd() - .2) * 90000).toFixed(2),
  aum: ri(1, 900) * 1000, followers: ri(0, 1500), maxF: 1500, share: pick([0, 5, 10, 10, 15, 20, 30]), mdd: +(rnd() * 45).toFixed(1), days: ri(7, 600),
  status: i < 4 ? '申请中' : pick(['带单中', '带单中', '带单中', '暂停带单', '已下架']) }))
export const grids = Array.from({ length: 36 }, (_, i) => ({ id: 'GD' + (40000 + i), uid: uid(ri(0, 85)), sym: pick(SYMS.slice(0, 10)) + '/USDT', mode: pick(['等差', '等比']), n: ri(2, 100),
  inv: ri(10, 20000), pnl: +((rnd() - .3) * 800).toFixed(2), runtime: ri(1, 900) + ' 小时', status: pick(['运行中', '运行中', '已停止', '止盈结束', '止损结束']) }))
export const contents = [
  ['关于 USDT-ERC20 网络维护的公告', '公告', '已发布'], ['新增美股合约 NVDA/TSLA/AAPL', '上新', '已发布'], ['合约交易赛 S3 规则说明', '活动', '已发布'],
  ['资金费率结算时间调整', '公告', '定时发布'], ['如何开启双重验证（2FA）', '帮助中心', '已发布'], ['11 月首页 Banner', 'Banner', '草稿'],
  ['防诈骗提醒：官方不会私信索要验证码', '公告', '已发布'], ['App 6.2 版本更新说明', '公告', '审核中']]
  .map(([t, c, s], i) => ({ id: 'CT' + (500 + i), title: t, cat: c, status: s, lang: pick(['中/英', '中/英/日', '中/英/韩/越']), author: pick(['op_lin', 'mkt_wu', 'cs_he']), pv: ri(0, 90000), time: ts(ri(10, 30000)), top: i < 2 }))
export const admins = [['super_root', '超级管理员', '平台'], ['ops_lin', '运营', '运营部'], ['ops_wang', '运营', '运营部'], ['kyc_chen', 'KYC 审核员', '合规部'], ['fin_zhao', '财务初审', '财务部'],
  ['fin_qian', '财务复审', '财务部'], ['risk_sun', '风控专员', '风控部'], ['cs_he', '客服', '客服部'], ['mkt_wu', '市场', '市场部'], ['audit_li', '审计（只读）', '内审']]
  .map(([u, r, d], i) => ({ id: u, user: u, role: r, dept: d, tfa: true, ip: i ? '10.8.0.' + (10 + i) : '10.8.0.2', last: ts(ri(1, 6000)), status: i === 7 ? '已禁用' : '启用' }))
export const audits = Array.from({ length: 90 }, (_, i) => { const a = pick(admins); return { id: 'AU' + (900000 - i), who: a.user, role: a.role, mod: pick(['用户管理', 'KYC', '提现审核', '币种管理', '合约参数', '手续费', '活动', '内容', '角色权限', '系统设置']),
  act: pick(['查看', '修改', '审核通过', '驳回', '冻结', '导出', '登录']), target: pick(['UID ' + uid(ri(0, 85)), 'BTCUSDT', 'WD' + ri(90310000, 90310057), 'FEE-SPOT', 'CP101']), ip: a.ip,
  risk: pick(['普通', '普通', '普通', '敏感', '高危']), time: ts(i * 13 + ri(0, 10)), result: rnd() > .05 ? '成功' : '失败' } })

// 费率配置（全站统一，无 VIP 等级）
export const feeRates = [
  { id: 'spot-maker', mkt: '现货', side: 'Maker', rate: '0.10%', base: '成交金额', coin: '按收到的币种收取', ver: 'v3' },
  { id: 'spot-taker', mkt: '现货', side: 'Taker', rate: '0.10%', base: '成交金额', coin: '按收到的币种收取', ver: 'v3' },
  { id: 'fut-maker', mkt: 'U 本位合约', side: 'Maker', rate: '0.05%', base: '仓位价值（开仓、平仓各收一次）', coin: 'USDT', ver: 'v3' },
  { id: 'fut-taker', mkt: 'U 本位合约', side: 'Taker', rate: '0.05%', base: '仓位价值（开仓、平仓各收一次）', coin: 'USDT', ver: 'v3' }]
export const feeChanges = [
  ['CR-20261102-03', '合约 Maker/Taker', '0.05% / 0.05%', '0.05% / 0.05%（明确开、平仓各收一次）', 'ops_lin', 'fin_qian', '已生效', ts(180)],
  ['CR-20261102-02', '提现手续费', '1 USDT / 笔', '0（仅收链上 Gas）', 'ops_lin', 'fin_qian', '已生效', ts(200)],
  ['CR-20261102-01', 'VIP 费率表', '6 档 VIP', '取消 VIP 分级，全站统一费率', 'ops_lin', 'super_root', '已生效', ts(230)],
  ['CR-20261101-07', '现货 Maker/Taker', '0.05% / 0.05%', '0.10% / 0.10%', 'ops_wang', 'super_root', '已生效', ts(1500)],
  ['CR-20261031-02', '现货 Taker', '0.10%', '0.12%', 'ops_wang', '-', '已驳回', ts(3200)]]
  .map(([id, item, from, to, by, ap, st, time]) => ({ id, item, from, to, by, ap, st, time }))
// 钱包
export const wallets = [
  ['TRON 热钱包', 'TRC20', '热', 3820410, 2000000, 'T9yD…4kQe', '正常'], ['Ethereum 热钱包', 'ERC20', '热', 812300, 1000000, '0x7a2…91cF', '低于阈值'],
  ['BNB Chain 热钱包', 'BEP20', '热', 2210400, 1000000, '0x3c1…a07B', '正常'], ['Arbitrum 热钱包', 'Arbitrum', '热', 1430000, 800000, '0x5e9…22dA', '正常'],
  ['Solana 热钱包', 'Solana', '热', 620300, 500000, '7xKq…pL2m', '正常'], ['冷钱包 A（多签 2/3）', 'Multi', '冷', 812400000, 0, '多签 · 离线', '正常'],
  ['冷钱包 B（多签 2/3）', 'Multi', '冷', 1021500000, 0, '多签 · 离线', '正常']]
  .map(([n, net, k, bal, th, addr, st]) => ({ id: n, name: n, net, kind: k, bal, th, addr, st }))
export const sweeps = Array.from({ length: 24 }, (_, i) => ({ id: 'SW' + (77000 + i), type: pick(['热→冷 归集', '冷→热 补充', '充值地址归集']), net: pick(NETS), amt: ri(20, 900) * 1000,
  status: i < 2 ? '待签名' : pick(['已完成', '已完成', '已完成', '广播中']), signers: i < 2 ? '1 / 2' : '2 / 2', by: pick(['fin_zhao', 'fin_qian']), time: ts(i * 47 + ri(0, 30)) }))
// 强平
export const liqs = Array.from({ length: 40 }, (_, i) => ({ id: 'LQ' + (610000 + i), uid: uid(ri(0, 85)), sym: pick(SYMS.slice(0, 8)) + 'USDT', side: pick(['多', '空']), lev: pick([10, 20, 50, 100, 125]),
  size: ri(1, 400) * 100, price: +(rnd() * 60000).toFixed(2), loss: +(rnd() * 3000).toFixed(2), ins: +((rnd() - .3) * 200).toFixed(2), type: pick(['强平', '强平', '强平', 'ADL 减仓']), time: ts(i * 11 + ri(0, 9)) }))
// 合伙人
const TIER = ['普通', '银牌', '金牌', '钻石']
export const partners = Array.from({ length: 32 }, (_, i) => { const vol = [ri(1, 49) * 1e5, ri(50, 199) * 1e5, ri(200, 499) * 1e5, ri(500, 1200) * 1e5][i < 18 ? 0 : i < 26 ? 1 : i < 30 ? 2 : 3]
  const t = vol >= 5e7 ? 3 : vol >= 2e7 ? 2 : vol >= 5e6 ? 1 : 0
  return { id: 'PT' + (3000 + i), uid: uid(i * 2 + 4), nick: nick(i * 2 + 4), tier: TIER[t], fut: [30, 40, 45, 50][t], spot: [20, 25, 30, 35][t], vol, invitees: ri(3, 900),
    up: i % 5 === 0 ? '-' : uid(ri(0, 10) * 2 + 4), today: +(rnd() * 3000).toFixed(2), total: +(rnd() * 90000).toFixed(2), status: i < 3 ? '申请中' : i === 7 ? '已冻结' : '正常' } })
// 卡券
export const coupons = [['10 USDT 手续费抵扣券', '手续费抵扣券', 10, 5000, 2310, '领取后 14 天'], ['20 USDT 合约体验金', '体验金', 20, 3000, 2960, '领取后 7 天'], ['5 USDT 现金券', '现金券', 5, 10000, 4120, '领取后 30 天'],
  ['50 USDT 合约体验金', '体验金', 50, 500, 31, '领取后 7 天'], ['100 USDT 手续费抵扣券', '手续费抵扣券', 100, 200, 0, '固定 2026-12-31']]
  .map(([n, k, v, tot, used, exp], i) => ({ id: 'CPN' + (800 + i), name: n, kind: k, val: v, tot, used, exp, status: i === 4 ? '草稿' : '发放中' }))
// 站内通知
export const pushes = [['系统维护通知：11/05 02:00-03:00', '全部用户', '站内信 + 邮件', '已发送', 912340], ['合约手续费说明更新', '合约用户', '站内信', '已发送', 210450],
  ['提现仅收链上 Gas 说明', '全部用户', '站内信 + App 推送', '定时发送', 0], ['KYC 补充材料提醒', '指定 UID 名单', '邮件', '草稿', 0], ['交易赛 S3 获奖通知', '活动获奖用户', '站内信', '已发送', 120]]
  .map(([t, aud, ch, st, n], i) => ({ id: 'NT' + (4400 + i), title: t, aud, ch, st, n, time: ts(i * 300 + 20) }))
