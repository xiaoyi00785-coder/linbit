import { useQuery } from "@tanstack/react-query";
import { api } from "./api";
import { useAuthStore } from "./store";

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

