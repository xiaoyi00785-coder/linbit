import { proxyGw, type GwEnv } from "@/lib/gwProxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

// 本地开发默认网关 localhost:8080（与原 rewrite 行为一致）
const env = (): GwEnv => ({ ...(process.env as GwEnv), API_PROXY_TARGET: process.env.API_PROXY_TARGET || "http://localhost:8080" });
type Ctx = { params: { path: string[] } };
const h = (req: Request, { params }: Ctx) => proxyGw(req, params.path ?? [], env());
export { h as GET, h as POST, h as PUT, h as PATCH, h as DELETE, h as HEAD, h as OPTIONS };
