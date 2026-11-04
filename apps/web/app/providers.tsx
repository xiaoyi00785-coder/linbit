"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import { installGeoBlockHandler } from "@/lib/geoblock";
import { TransferHost } from "@/components/Transfer";
import { OverlayManager } from "@/components/OverlayManager";

export function Providers({ children }: { children: React.ReactNode }) {
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

  return <QueryClientProvider client={client}>{children}<TransferHost /><OverlayManager /></QueryClientProvider>;
}
