/** @type {import('next').NextConfig} */
// 主网构建禁止开启 mock/demo（与 apps/web 相同）
const truthy = (v) => ["1", "true", "yes", "on"].includes(String(v ?? "").toLowerCase());
const network = String(process.env.NEXT_PUBLIC_NETWORK || process.env.NETWORK || process.env.APP_ENV || "").toLowerCase();
if (network === "mainnet") {
  for (const k of ["NEXT_PUBLIC_ENABLE_MOCK", "NEXT_PUBLIC_DEMO"]) {
    if (truthy(process.env[k])) throw new Error(`mainnet build refused: ${k} must be unset`);
  }
}
export default {
  reactStrictMode: true,
  transpilePackages: ["@linbit/ui"],
  eslint: { ignoreDuringBuilds: true },
  // /gw/* 由 app/gw/[...path]/route.ts 在服务端转发到网关（lib/gwProxy.ts，与主站相同）
};
