import { useState } from 'react'
import { PageHead, Tabs } from '../ui.jsx'
import { ConfigCard } from './Common.jsx'
import { SocialSettings } from './Social.jsx'
const SEC = { withdrawLockHours: '改密/解绑后禁止提现（小时，0=关闭）', fundPasswordForWithdraw: '提现需资金密码', fundPasswordForInternalTransfer: '内部转账需资金密码',
  fundPasswordMandatory: '未设资金密码禁止出金', fundPasswordMaxAttempts: '资金密码最大错误次数', fundPasswordLockMinutes: '资金密码锁定（分钟）', lockAppliesToInternalTransfer: '冷却同样限制内部转账' }
export function Settings() {
  const [t, setT] = useState('sec')
  return <><PageHead title="系统设置" desc="用户安全策略、资产与内部转账配置、社区链接与 App 功能开关；所有变更需 2FA 并写入审计" />
    <div className="card"><Tabs value={t} onChange={setT} items={[['sec', '用户安全'], ['asset', '资产设置'], ['xfer', '内部转账'], ['social', '社区链接'], ['app', 'App 功能开关']]} /></div>
    {t === 'sec' && <ConfigCard path="security-settings" title="用户安全策略" labels={SEC} />}
    {t === 'asset' && <ConfigCard path="asset-settings" title="资产服务设置" />}
    {t === 'xfer' && <ConfigCard path="internal-transfer/config" title="内部转账配置" />}
    {t === 'social' && <SocialSettings />}
    {t === 'app' && <ConfigCard path="app/features" field="features" title="App 功能开关" />}</>
}

export { Admins, Audit } from './Live.jsx'
