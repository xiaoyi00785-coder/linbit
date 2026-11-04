// Apple App Site Association：通行密钥（webcredentials）与通用链接（applinks）。
// 由运行时环境变量驱动：APPLE_TEAM_ID（10 位）+ IOS_BUNDLE_ID；任一缺失则 404。
// Apple 要求：路径无扩展名、200 直出（不得重定向）、Content-Type: application/json。
export const dynamic = "force-dynamic";

export function GET(): Response {
  const team = (process.env.APPLE_TEAM_ID || "").trim();
  const bundles = (process.env.IOS_BUNDLE_ID || "").split(",").map((s) => s.trim()).filter(Boolean);
  if (!/^[A-Z0-9]{10}$/.test(team) || bundles.length === 0) {
    return new Response(JSON.stringify({ error: "not configured" }), {
      status: 404,
      headers: { "Content-Type": "application/json", "Cache-Control": "no-store" },
    });
  }
  const appIDs = bundles.map((b) => `${team}.${b}`);
  const body = {
    applinks: { details: [{ appIDs, components: [{ "/": "*" }] }] },
    webcredentials: { apps: appIDs },
  };
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json", "Cache-Control": "public, max-age=3600" },
  });
}
