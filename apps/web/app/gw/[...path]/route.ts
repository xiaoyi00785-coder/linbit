import { proxyGw } from "@/lib/gwProxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Ctx = { params: { path: string[] } };
const h = (req: Request, { params }: Ctx) => proxyGw(req, params.path ?? []);
export { h as GET, h as POST, h as PUT, h as PATCH, h as DELETE, h as HEAD, h as OPTIONS };
