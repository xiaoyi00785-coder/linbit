/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  // 直接消费 workspace 包的 TS 源码，无需预先构建
  transpilePackages: ["@linbit/ui", "@linbit/chart", "@linbit/api-client"],
  // 本地联调：NEXT_PUBLIC_API_BASE=/gw 且 API_PROXY_TARGET=http://localhost:8080 时，
  // 由 Next 同源代理到网关，避免浏览器 CORS（网关当前未返回 CORS 头）
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
