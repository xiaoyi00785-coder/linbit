/** @type {import('next').NextConfig} */
// 主网构建禁止开启 mock/demo（与 apps/web 相同）
const truthy = (v) => ["1", "true", "yes", "on"].includes(String(v ?? "").toLowerCase());
const network = String(process.env.NEXT_PUBLIC_NETWORK || process.env.NETWORK || process.env.APP_ENV || "").toLowerCase();
if (network === "mainnet") {
  for (const k of ["NEXT_PUBLIC_ENABLE_MOCK", "NEXT_PUBLIC_DEMO"]) {
    if (truthy(process.env[k])) throw new Error(`mainnet build refused: ${k} must be unset`);
  }
}
const target = process.env.API_PROXY_TARGET || "http://localhost:8080";
export default {
  reactStrictMode: true,
  transpilePackages: ["@linbit/ui"],
  eslint: { ignoreDuringBuilds: true },
  // 与主站相同：/gw/* 同源转发到网关，避免跨域
  async rewrites() { return [{ source: "/gw/:path*", destination: `${target}/:path*` }]; },
};
