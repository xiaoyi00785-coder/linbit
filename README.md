# Linbit 交易平台（Monorepo）

Linbit 交易所的前端、后台、移动端和后端在同一个仓库里。

- **apps/web** — Next.js（App Router）交易所网站。
- **apps/partner** — 合伙人后台。
- **prototypes/admin** — Vite 管理后台。
- **packages/ui**、**packages/chart** — 共享 UI 和 K 线。
- **packages/api-client** — 由 `docs/api/openapi.yaml` 生成的类型和 SDK。`src/schema.ts` 是生成文件，不要手改。
- **docs/api** — OpenAPI 契约和 WebSocket 协议。契约文件保持原样，改契约要单独走生成流程。
- **services/go** — 账户、网关、资产、钱包、订单、合约、行情等 Go 服务。
- **services/rust/matching-engine** — 现货撮合。价格和数量在引擎里是 8 位小数的定点整数。
- **mobile/ios**、**mobile/android** — 客户端。`Core/Generated` 和 `core/generated` 由 OpenAPI Generator 产出，不要手改。

Node 20、pnpm 10。后端各 Go module 和 Rust crate 分开构建，见 `services/README.md`。

## 目录结构

```
linbit/
├─ apps/web、apps/partner
├─ prototypes/admin
├─ packages/ui、api-client、chart
├─ docs/api/openapi.yaml、websocket.md
├─ services/go/*、services/rust/matching-engine
├─ mobile/ios、mobile/android
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

路由在 `apps/web/app`。现货是 `/spot` 和 `/trade`，合约是 `/futures`，资产是 `/assets`。行情页依赖浏览器里的图表，用客户端渲染。

## 设计令牌

颜色在 `packages/ui/src/styles/tokens.css`。当前主色是 `--lb-primary: #abe127`。Tailwind 通过 `packages/ui/tailwind-preset.cjs` 映射成 `lb-*` 类，换色改变量即可。

## 后端

服务划分、端口和现货/钱包链路见 [`services/README.md`](./services/README.md)。没有单独的 `risk` 进程。
