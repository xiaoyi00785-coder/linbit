/** 登录后的 react-query。余额在 isAuthed 为 false 时不请求，避免公开页 401。 */
import { useQuery } from "@tanstack/react-query";
import { api } from "./api";
import { useAuthStore } from "./store";

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

