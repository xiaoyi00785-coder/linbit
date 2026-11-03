/** @type {import('next').NextConfig} */
const target = process.env.API_PROXY_TARGET || "http://localhost:8080";
export default {
  reactStrictMode: true,
  transpilePackages: ["@linbit/ui"],
  eslint: { ignoreDuringBuilds: true },
  // 与主站相同：/gw/* 同源转发到网关，避免跨域
  async rewrites() { return [{ source: "/gw/:path*", destination: `${target}/:path*` }]; },
};
