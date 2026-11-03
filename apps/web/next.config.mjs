/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  // 直接消费 workspace 包的 TS 源码，无需预先构建
  transpilePackages: ["@linbit/ui", "@linbit/chart", "@linbit/api-client"],
  eslint: {
    // 构建时跳过 ESLint（本仓库未配置 eslint）
    ignoreDuringBuilds: true,
  },
};

export default nextConfig;
