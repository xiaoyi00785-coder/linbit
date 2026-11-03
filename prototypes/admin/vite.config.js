import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'
// 本地开发：`npm run dev` 时把 /admin/v1 代理到本地 admin 服务（可用 ADMIN_DEV_PROXY 覆盖）
export default defineConfig({
  plugins: [react()],
  base: './',
  server: { proxy: { '/admin/v1': { target: process.env.ADMIN_DEV_PROXY || 'http://localhost:8088', changeOrigin: true } } },
})
