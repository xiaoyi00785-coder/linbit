import { useQuery } from "@tanstack/react-query";
import type { KlineInterval, Ticker } from "@linbit/api-client";
import { api } from "./api";
import { useAuthStore } from "./store";

/** 交易对列表。 */
export function useSymbols() {
  return useQuery({
    queryKey: ["symbols"],
    queryFn: async () => {
      const { data, error } = await api.GET("/market/symbols");
      if (error) throw error;
      return data ?? [];
    },
    staleTime: 60_000,
  });
}

/** 24h 行情 ticker（全部交易对）。 */
export function useTickers() {
  return useQuery({
    queryKey: ["tickers"],
    queryFn: async () => {
      const { data, error } = await api.GET("/market/ticker", {
        params: { query: {} },
      });
      if (error) throw error;
      // 不带 symbol 返回数组；带 symbol 返回单对象（c5143eb）
      return (Array.isArray(data) ? data : data ? [data] : []) as Ticker[];
    },
    refetchInterval: 5_000,
    refetchIntervalInBackground: false,
  });
}

/** 单个交易对的 K 线。 */
export function useKlines(symbol: string, interval: KlineInterval = "15m") {
  return useQuery({
    queryKey: ["klines", symbol, interval],
    queryFn: async () => {
      const { data, error } = await api.GET("/market/klines", {
        params: { query: { symbol, interval, limit: 160 } },
      });
      if (error) throw error;
      return data ?? [];
    },
    refetchInterval: 15_000,
  });
}

/** 订单簿深度。 */
export function useDepth(symbol: string) {
  return useQuery({
    queryKey: ["depth", symbol],
    queryFn: async () => {
      const { data, error } = await api.GET("/market/depth", {
        params: { query: { symbol, limit: 20 } },
      });
      if (error) throw error;
      return data;
    },
    refetchInterval: 3_000,
  });
}

/** 资产余额（需登录）。 */
export function useBalances() {
  const isAuthed = useAuthStore((s) => s.isAuthed);
  return useQuery({
    queryKey: ["balances"],
    enabled: isAuthed,
    queryFn: async () => {
      const { data, error } = await api.GET("/assets/balances", {
        params: { query: {} },
      });
      if (error) throw error;
      return data;
    },
  });
}

/** 当前委托（需登录）。 */
export function useOpenOrders(symbol?: string) {
  const isAuthed = useAuthStore((s) => s.isAuthed);
  return useQuery({
    queryKey: ["open-orders", symbol ?? "all"],
    enabled: isAuthed,
    queryFn: async () => {
      const { data, error } = await api.GET("/spot/orders/open", {
        params: { query: symbol ? { symbol } : {} },
      });
      if (error) throw error;
      return data;
    },
    refetchInterval: 5_000,
  });
}
