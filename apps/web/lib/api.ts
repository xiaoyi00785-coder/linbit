import { createApiClient, DEFAULT_BASE_URL } from "@linbit/api-client";
import { useAuthStore } from "./store";

/** API 基地址：可通过 NEXT_PUBLIC_API_BASE 覆盖，默认本地开发网关。 */
export const API_BASE = process.env.NEXT_PUBLIC_API_BASE ?? DEFAULT_BASE_URL;

/** 全局 API 客户端，自动附加当前登录态的 Bearer 令牌。 */
export const api = createApiClient({
  baseUrl: API_BASE,
  getAccessToken: () => useAuthStore.getState().accessToken,
  // 与登录时提交的 deviceId 一致（lb_device_id），让设备列表能标出当前设备
  getDeviceId: () => (typeof window === "undefined" ? null : localStorage.getItem("lb_device_id")),
  // 401 → 刷新令牌后重试一次（与 rest.ts 共用同一次刷新；动态 import 避免循环依赖）
  onUnauthorized: () => import("./rest").then((m) => m.refreshAccess()),
});
