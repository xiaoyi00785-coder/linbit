"use client";
/** 营销配置、定期最高 APR、储备证明。拿不到接口时不写死金额与年化。 */
import { useEffect, useState } from "react";
import type { components } from "@linbit/api-client";
import { api } from "./api";
import { maxApr, onSale } from "./earn";

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
  return maxApr(data?.items ?? []);
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

/** 储备证明：GET /market/proof-of-reserves。未发布时 available=false，调用方显示空态，不写死储备数字。 */
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
  return (data?.items ?? []).filter(onSale);
});
/** GET /earn/products（理财总览：推荐与全部产品均以此为准，勿写死 APR） */
export const useEarnProducts = () => useLoad(loadEarnProducts);
