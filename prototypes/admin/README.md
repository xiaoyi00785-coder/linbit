# linbit 管理后台（admin 前端）

Vite + React 单页应用，对接 Go admin 服务 `admin/v1` 接口（`services/go/admin`）。

## 构建

```bash
cd prototypes/admin
npm ci && npm run build
```

产物输出到 `dist/`（纯静态文件，`base: './'`，可部署在任意子路径；路由使用 `#/` hash，无需服务端 rewrite）。

## 环境变量（构建时注入）

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `VITE_ADMIN_API_BASE` | `/admin/v1/` | admin API 基址。同域部署用默认相对路径；跨域部署例如 `https://ops-8pnl3m.cowa.click/admin/v1/`（需 admin 服务允许该来源的 CORS） |
| `VITE_ADMIN_ENV_LABEL` | `后台` | 顶栏环境标签，如 `生产` / `测试` |

示例：

```bash
VITE_ADMIN_API_BASE=https://ops-8pnl3m.cowa.click/admin/v1/ VITE_ADMIN_ENV_LABEL=生产 npm run build
```

## 本地开发

```bash
npm ci && npm run dev   # /admin/v1 代理到 http://localhost:8088，可用 ADMIN_DEV_PROXY 覆盖
```

## 鉴权

- 登录：`POST auth/login {username, password, totpCode}`，必须输入 Google 验证码（2FA）。
- token 保存在 `sessionStorage`（关闭标签页即失效），请求头 `Authorization: Bearer <token>`。
- 任意接口返回 401 → 清除 token 并回到登录页；退出调用 `POST auth/logout`。
- 所有写操作弹窗都要求 6 位验证码 + 原因，随请求提交给后端校验。

## 接口覆盖

已接入真实接口：登录 / 当前管理员、用户列表与冻结、用户资产、KYC 审核（含证件查看）、提现两级审核、资产流水、现货交易对、手续费与变更单、奖励补发、卡券、公告、站内信广播、合伙人、管理员、审计日志。

仍为演示数据（页面顶部有提示，后端暂无接口）：数据看板、风险名单、充值记录、钱包管理、现货订单、合约持仓、网格策略、强平与风险、币种与网络、合约参数、活动管理、积分商城、跟单、返佣配置、系统设置。
