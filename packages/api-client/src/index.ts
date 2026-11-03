import createClient, { type Client, type Middleware } from "openapi-fetch";
import type { components, paths } from "./schema";

export type { paths, components } from "./schema";

/** 所有在 openapi.yaml `components.schemas` 中定义的数据模型。 */
export type Schemas = components["schemas"];

// 常用模型的便捷别名，供 UI 直接引用。
export type Ticker = Schemas["Ticker"];
export type Symbol = Schemas["Symbol"];
export type Depth = Schemas["Depth"];
export type Kline = Schemas["Kline"];
export type KlineInterval = Schemas["KlineInterval"];
export type Order = Schemas["Order"];
export type OrderSide = Schemas["OrderSide"];
export type OrderType = Schemas["OrderType"];
export type OrderStatus = Schemas["OrderStatus"];
export type Trade = Schemas["Trade"];
export type Balance = Schemas["Balance"];
export type Account = Schemas["Account"];
export type AuthTokens = Schemas["AuthTokens"];
export type ApiError = Schemas["Error"];

export type LinbitClient = Client<paths>;

export interface CreateApiClientOptions {
  /** API 基地址，默认为本地 Prism mock。 */
  baseUrl?: string;
  /** 返回当前 accessToken，用于自动附加 Authorization 头。 */
  getAccessToken?: () => string | null | undefined;
  /** 返回本机设备 ID，自动附加 X-Device-Id 头（设备列表据此标记当前设备）。 */
  getDeviceId?: () => string | null | undefined;
}

/** 默认指向本地 Prism mock server。 */
export const DEFAULT_BASE_URL = "http://localhost:4010";

/**
 * 创建一个类型安全的 Linbit API 客户端（基于 openapi-fetch）。
 * 若提供 getAccessToken，则会自动在每个请求上附加 Bearer 令牌。
 */
export function createApiClient(
  options: CreateApiClientOptions = {},
): LinbitClient {
  const { baseUrl = DEFAULT_BASE_URL, getAccessToken, getDeviceId } = options;
  const client = createClient<paths>({ baseUrl });

  if (getAccessToken) {
    const authMiddleware: Middleware = {
      onRequest({ request }) {
        const token = getAccessToken();
        if (token) {
          request.headers.set("Authorization", `Bearer ${token}`);
        }
        const dev = getDeviceId?.();
        if (dev) {
          request.headers.set("X-Device-Id", dev);
        }
        return request;
      },
    };
    client.use(authMiddleware);
  }

  return client;
}
