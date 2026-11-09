/** /gw/* 的服务端入口，全部方法交给 lib/gwProxy。
 *  runtime=nodejs、dynamic=force-dynamic，避免把带身份头的响应静态缓存。
 *  lib/gwProxy.ts 必须与 apps/partner/lib/gwProxy.ts 逐字相同，check-gw-proxy.mjs 会比这两份文件。 */
import { proxyGw } from "@/lib/gwProxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Ctx = { params: { path: string[] } };
const h = (req: Request, { params }: Ctx) => proxyGw(req, params.path ?? []);
export { h as GET, h as POST, h as PUT, h as PATCH, h as DELETE, h as HEAD, h as OPTIONS };
