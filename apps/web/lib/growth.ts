"use client";
import { useEffect, useState } from "react";
import type { components } from "@linbit/api-client";
import { api } from "./api";

export type GrowthConfig = components["schemas"]["GrowthConfig"];
export type ProofOfReserves = components["schemas"]["ProofOfReserves"];

/** 小数 → 百分比文本（0.1 → "10%"，0.088 → "8.8%"）；无值返回 fallback */
export const pct = (v: string | number | undefined | null, fb = "--") => {
  const n = Number(v);
  if (v === undefined || v === null || v === "" || !Number.isFinite(n)) return fb;
  return `${+(n * 100).toFixed(2)}%`;
};

function cached<T>(load: () => Promise<T | null>) {
  let p: Promise<T | null> | null = null;
  return () => (p ??= load().catch(() => null));
}

const loadGrowth = cached(async () => {
  const { data } = await api.GET("/growth/config");
  return data ?? null;
});

const loadMaxFixedApr = cached(async () => {
  const { data } = await api.GET("/earn/products", { params: { query: { kind: "fixed" } } });
  const aprs = (data?.items ?? []).map((p) => Number(p.aprMax || p.apr)).filter((n) => Number.isFinite(n) && n > 0);
  return aprs.length ? Math.max(...aprs) : null;
});

function useLoad<T>(load: () => Promise<T | null>) {
  const [v, setV] = useState<T | null>(null);
  useEffect(() => {
    let on = true;
    load().then((x) => on && setV(x));
    return () => { on = false; };
  }, [load]);
  return v;
}

/** GET /growth/config（营销数字以此为准，勿写死） */
export const useGrowthConfig = () => useLoad(loadGrowth);
/** 定期理财最高 APR（小数），来自 GET /earn/products?kind=fixed */
export const useMaxFixedApr = () => useLoad(loadMaxFixedApr);

/** GET /market/proof-of-reserves */
export function useProofOfReserves() {
  const [v, setV] = useState<ProofOfReserves | null | undefined>(undefined);
  useEffect(() => {
    let on = true;
    api.GET("/market/proof-of-reserves").then(({ data }) => on && setV(data ?? null), () => on && setV(null));
    return () => { on = false; };
  }, []);
  return v;
}

const loadEarnProducts = cached(async () => {
  const { data } = await api.GET("/earn/products", {});
  return (data?.items ?? []).filter((p) => !p.status || p.status === "online" || p.status === "active");
});
/** GET /earn/products（理财总览：推荐与全部产品均以此为准，勿写死 APR） */
export const useEarnProducts = () => useLoad(loadEarnProducts);
