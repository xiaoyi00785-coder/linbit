// 主网构建保护：NEXT_PUBLIC_NETWORK=mainnet（或 NETWORK/APP_ENV=mainnet）时禁止任何 mock/演示开关。
const network = (process.env.NEXT_PUBLIC_NETWORK || process.env.NETWORK || process.env.APP_ENV || "").toLowerCase();
if (network === "mainnet") {
  const bad = ["NEXT_PUBLIC_ENABLE_MOCK", "NEXT_PUBLIC_DEMO"].filter((k) => {
    const v = (process.env[k] || "").trim().toLowerCase();
    return v !== "" && v !== "0" && v !== "false" && v !== "off";
  });
  if (bad.length) {
    throw new Error(`mainnet build refused: ${bad.join(", ")} must be unset/0 (mock/demo data is forbidden on mainnet)`);
  }
}

/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  env: { NEXT_PUBLIC_NETWORK: network || "testnet" },
  // 直接消费 workspace 包的 TS 源码，无需预先构建
  transpilePackages: ["@linbit/ui", "@linbit/chart", "@linbit/api-client"],
  // 本地联调：NEXT_PUBLIC_API_BASE=/gw 且 API_PROXY_TARGET=http://localhost:8080 时，
  // 由 Next 同源代理到网关，避免浏览器 CORS（网关当前未返回 CORS 头）
  // 旧/错误链接兜底：合约账户实际页面为 /assets/trading-account；/earn/wealth 无对应产品页 → 理财总览
  async redirects() {
    return [
      { source: "/assets/futures-account", destination: "/assets/trading-account", permanent: false },
      { source: "/earn/wealth", destination: "/earn", permanent: false },
    ];
  },
  async rewrites() {
    const t = process.env.API_PROXY_TARGET;
    return t ? [{ source: "/gw/:path*", destination: `${t}/:path*` }] : [];
  },
  eslint: {
    // 构建时跳过 ESLint（本仓库未配置 eslint）
    ignoreDuringBuilds: true,
  },
};

export default nextConfig;
