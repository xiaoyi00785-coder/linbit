# Linbit 交易平台（Monorepo）

Linbit 加密货币交易所的前端与 API 契约单一仓库（pnpm monorepo）。

- **apps/web** — Next.js 14（App Router）+ TypeScript + Tailwind + Zustand + TanStack Query 的交易所前端。
- **packages/ui** — React 组件库 + Tailwind preset + 设计令牌（CSS 变量，蓝色系品牌色、深色底）。
- **packages/api-client** — 由 `docs/api/openapi.yaml` 生成的类型 + `openapi-fetch` SDK。
- **packages/chart** — 基于 lightweight-charts v4 的 K 线组件。
- **docs/api** — OpenAPI 3.0.3 契约 + WebSocket 协议文档（API 单一事实来源）。
- **services** — 未来后端服务（Go + Rust）的规划与占位。

要求：**Node 20、pnpm 10**。

## 目录结构

```
linbit-platform/
├─ apps/
│  └─ web/                 # Next.js 前端
├─ packages/
│  ├─ ui/                  # 组件库 + tailwind preset + tokens.css
│  ├─ api-client/          # openapi-typescript 类型 + openapi-fetch SDK
│  └─ chart/               # KLineChart（lightweight-charts v4）
├─ docs/api/
│  ├─ openapi.yaml         # REST 契约（Prism mock / 代码生成的来源）
│  └─ websocket.md         # WebSocket 协议
├─ services/               # 后端服务规划（Go 业务服务 + Rust 撮合引擎）
└─ pnpm-workspace.yaml
```

## 快速开始

### 1. 安装依赖

```bash
pnpm install
```

### 2. 启动 Mock Server（端口 4010）

使用 `@stoplight/prism-cli` 按 `docs/api/openapi.yaml` 的 `examples` 返回模拟数据：

```bash
pnpm mock
```

Mock 运行在 `http://localhost:4010`，即前端默认的 `baseURL`。

### 3. 启动前端（端口 3000）

另开一个终端：

```bash
pnpm dev
```

打开 http://localhost:3000 。前端通过 `@linbit/api-client` 调用 mock。
如需指向其它后端，设置环境变量（见 `apps/web/.env.example`）：

```bash
NEXT_PUBLIC_API_BASE=http://localhost:4010
```

### 4. 构建

```bash
pnpm build
```

会按依赖顺序构建所有 workspace 包与前端。

### 5. 重新生成 API 类型

修改 `docs/api/openapi.yaml` 后，重新生成 `@linbit/api-client` 的类型（生成文件已提交到仓库）：

```bash
pnpm gen:api
```

## 根脚本

| 脚本 | 说明 |
| --- | --- |
| `pnpm dev` | 启动前端 `@linbit/web`（http://localhost:3000） |
| `pnpm build` | 递归构建所有包 |
| `pnpm mock` | Prism 按 openapi.yaml 启动 mock（http://localhost:4010） |
| `pnpm gen:api` | 重新生成 api-client 类型 |
| `pnpm typecheck` | 递归类型检查 |

## 页面

| 路由 | 说明 |
| --- | --- |
| `/` | 首页（Hero、热门交易对、安全介绍） |
| `/markets` | 行情列表（搜索、排序） |
| `/trade/[symbol]` | 现货交易（K 线 + 盘口 + 下单 + 当前委托，`ssr:false` 客户端渲染） |
| `/login`、`/register` | 登录 / 注册 |
| `/assets` | 资产余额（需登录） |

## 设计令牌与换肤

所有颜色定义在 `packages/ui/src/styles/tokens.css`，以 CSS 变量提供（品牌色为蓝色
`--lb-primary: #2f6bff`，深色底）。Tailwind 通过 `packages/ui/tailwind-preset.cjs` 把这些变量
映射成 `lb-*` 颜色类。**换肤只需改变量，无需改类名。**

---

## 从原型（/workspace/linbit）迁移到 apps/web

原型是一个 **React 18 + Vite** 的 JSX 项目：`react-router` 的 **hash 路由**、单个
`src/style.css`、`src/data.js` 提供模拟数据。迁移到本仓库 `apps/web` 的步骤如下。

### 1. 路由：hash 路由 → App Router

原型用 `<HashRouter>` + `<Routes>`（见 `src/App.jsx`、`src/main.jsx`）。按文件系统路由映射到
`apps/web/app/`：

| 原型路由（hash） | App Router 文件 |
| --- | --- |
| `#/` | `app/page.tsx` |
| `#/markets` | `app/markets/page.tsx` |
| `#/spot`、`#/trade`、`#/futures` | `app/trade/[symbol]/page.tsx`（现货；合约可后续加 `app/futures/...`） |
| `#/login` | `app/login/page.tsx` |
| `#/register` | `app/register/page.tsx` |
| `#/assets` | `app/assets/page.tsx` |
| `#/deposit`、`#/withdraw`、`#/orders`、`#/account/*` 等 | 后续按需在 `app/` 下补齐同名目录 |

- 全局布局（Header/Footer）从原型的 `App.jsx`/`Nav.jsx` 迁到 `app/layout.tsx` + `components/SiteNav.tsx`。
- `<Link to>` → `next/link` 的 `<Link href>`；`useNavigate()` → `useRouter()`（`next/navigation`）；
  `useLocation()` → `usePathname()`。
- 需要浏览器 API / 图表的页面（行情、交易）用 `next/dynamic` 的 `ssr:false` 只在客户端渲染，
  交互组件加 `"use client"`。

### 2. 组件：拆进 packages/ui

原型把大量 UI 揉在 `App.jsx` / `Pages.jsx` / `Pages2.jsx` / `Nav.jsx` 内。抽取可复用的基础件到
`packages/ui`（已内置 `Button`、`Input`、`Card`、`Tabs`、`Modal`、`Nav`、`TextLogo`）：

- 原型的 `<Logo/>`（内联 SVG + 文案 `LINBIT`）→ 用 `TextLogo`（文字 logo `Linbit`）。
- 原型的按钮/输入/卡片/标签页等 → 替换为 `@linbit/ui` 对应组件，className 改用 `lb-*` 令牌色。
- 页面级业务组件（如订单簿、下单面板）留在 `apps/web`，基础件下沉到 `packages/ui`。

### 3. 样式：style.css → tokens.css（并改蓝色）

原型用单个 `src/style.css`，品牌色是 lime 绿 `#abe127`、CSS 变量名为 `--primary` / `--up` /
`--down` 等。迁移：

- 把颜色变量迁到 `packages/ui/src/styles/tokens.css`，统一前缀为 `--lb-*`，
  **品牌色改为蓝色 `--lb-primary: #2f6bff`**（深色底保持）。
- 组件里不再写死颜色，改用 Tailwind 的 `lb-*` 类（`bg-lb-primary`、`text-lb-up` 等），
  由 `tailwind-preset.cjs` 映射到变量。
- 其余布局类样式逐步用 Tailwind 工具类替代，`app/globals.css` 只保留少量全局规则。

### 4. 模拟数据：data.js → api-client

原型从 `src/data.js` 读取写死的 `coins` 数组与 `candles()` 函数。迁移：

- 行情 / K 线 / 盘口 / 账户数据全部改为通过 `@linbit/api-client` 调用（见 `apps/web/lib/queries.ts`
  中的 `useTickers` / `useKlines` / `useDepth` / `useBalances` 等 TanStack Query 封装）。
- 开发期后端用 `pnpm mock`（Prism 按 `openapi.yaml` 的 examples 返回）。
- `candles()` 仅作为 K 线为空时的占位（见 `apps/web/lib/demo.ts`），正式数据来自 `/market/klines`。
- 登录态从原型基于 `localStorage` 的 `auth.js` 迁到 `apps/web/lib/store.ts`（Zustand + 持久化），
  token 由 api-client 中间件自动附加为 `Authorization: Bearer`。

### 5. 行情 / 交易页：ssr:false

这两类页面依赖图表库（lightweight-charts）与浏览器环境，统一用 `next/dynamic` 的 `ssr:false`
在客户端渲染（参考 `app/trade/[symbol]/page.tsx` 的写法：外层 server 包装 + `dynamic(..., { ssr:false })`
加载内部 `"use client"` 组件）。

---

## 后端规划

见 [`services/README.md`](./services/README.md)：Go 业务服务（gateway、account、kyc、wallet、asset、
order、market-data、admin、risk）与 Rust 撮合引擎（matching-engine）。对外契约以
`docs/api/openapi.yaml` 为准。
