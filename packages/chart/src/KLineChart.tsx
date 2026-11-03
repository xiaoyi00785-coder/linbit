import * as React from "react";
import {
  createChart,
  ColorType,
  type CandlestickData,
  type HistogramData,
  type IChartApi,
  type Time,
  type UTCTimestamp,
} from "lightweight-charts";

/** 单根 K 线数据（time 为秒级 UTC 时间戳）。 */
export interface Candle {
  time: number;
  open: number;
  high: number;
  low: number;
  close: number;
  volume?: number;
}

export interface KLineChartProps {
  data: Candle[];
  /** 是否显示成交量柱，默认显示 */
  volume?: boolean;
  /** 高度（px）；不传则由容器高度决定（autoSize 自适应） */
  height?: number;
  className?: string;
}

function cssVar(name: string, fallback: string): string {
  if (typeof window === "undefined") return fallback;
  const value = getComputedStyle(document.documentElement)
    .getPropertyValue(name)
    .trim();
  return value || fallback;
}

/**
 * 封装 lightweight-charts v4 的响应式 K 线组件（蜡烛 + 成交量）。
 * 颜色读取 tokens.css 中的 CSS 变量（--lb-*），便于整站换肤。
 */
export function KLineChart({
  data,
  volume = true,
  height,
  className,
}: KLineChartProps) {
  const containerRef = React.useRef<HTMLDivElement | null>(null);

  React.useEffect(() => {
    const el = containerRef.current;
    if (!el) return;

    const chart: IChartApi = createChart(el, {
      autoSize: true,
      layout: {
        background: { type: ColorType.Solid, color: cssVar("--lb-bg", "#0b0e15") },
        textColor: cssVar("--lb-text-2", "#9aa7bd"),
        fontSize: 11,
      },
      grid: {
        vertLines: { color: cssVar("--lb-grid", "#141a26") },
        horzLines: { color: cssVar("--lb-grid", "#141a26") },
      },
      rightPriceScale: { borderColor: cssVar("--lb-border", "#232c3d") },
      timeScale: {
        borderColor: cssVar("--lb-border", "#232c3d"),
        timeVisible: true,
      },
      crosshair: { mode: 0 },
    });

    const up = cssVar("--lb-up", "#16c784");
    const down = cssVar("--lb-down", "#ea3943");

    const candleSeries = chart.addCandlestickSeries({
      upColor: up,
      downColor: down,
      wickUpColor: up,
      wickDownColor: down,
      borderVisible: false,
    });
    candleSeries.setData(
      data.map(
        (c): CandlestickData<Time> => ({
          time: c.time as UTCTimestamp,
          open: c.open,
          high: c.high,
          low: c.low,
          close: c.close,
        }),
      ),
    );
    chart
      .priceScale("right")
      .applyOptions({ scaleMargins: { top: 0.05, bottom: volume ? 0.3 : 0.05 } });

    if (volume) {
      const volumeSeries = chart.addHistogramSeries({
        priceScaleId: "volume",
        priceFormat: { type: "volume" },
      });
      chart
        .priceScale("volume")
        .applyOptions({ scaleMargins: { top: 0.75, bottom: 0 } });
      volumeSeries.setData(
        data.map(
          (c): HistogramData<Time> => ({
            time: c.time as UTCTimestamp,
            value: c.volume ?? 0,
            color: c.close >= c.open ? cssVar("--lb-up-vol", "#16603f") : cssVar("--lb-down-vol", "#7a1f27"),
          }),
        ),
      );
    }

    chart.timeScale().fitContent();

    return () => chart.remove();
  }, [data, volume]);

  return (
    <div
      ref={containerRef}
      className={className}
      style={{ width: "100%", height: height ?? "100%" }}
    />
  );
}
