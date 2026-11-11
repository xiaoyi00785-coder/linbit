"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { BALANCES_EVT } from "@/lib/rest";
import { installGeoBlockHandler } from "@/lib/geoblock";
import { TransferHost } from "@/components/Transfer";
import { OverlayManager } from "@/components/OverlayManager";

export function Providers({ children }: { children: React.ReactNode }) {
  // 任意接口返回 451（REGION_BLOCKED）时整页跳到 /blocked，只安装一次
  installGeoBlockHandler();
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            staleTime: 5_000,
            retry: 1,
            refetchOnWindowFocus: false,
          },
        },
      }),
  );

  // 余额变化广播：react-query 中的余额缓存一并失效重取
  useEffect(() => {
    const f = () => { client.invalidateQueries({ queryKey: ["balances"], exact: true }); };
    window.addEventListener(BALANCES_EVT, f);
    return () => window.removeEventListener(BALANCES_EVT, f);
  }, [client]);

  return <QueryClientProvider client={client}>{children}<TransferHost /><OverlayManager /></QueryClientProvider>;
}
