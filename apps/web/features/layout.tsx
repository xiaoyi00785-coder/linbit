"use client";
import type { ComponentType, ReactNode } from "react";
import * as P from "@/legacy/Pages";
import * as A from "@/legacy/App";

/** legacy 布局组件的类型化包装 */
export const Shell = P.Shell as unknown as ComponentType<{ side: ReactNode; children: ReactNode; foot?: boolean }>;
export const Side = P.Side as unknown as ComponentType<{ items: string[][]; extra?: ReactNode }>;
export const ACC = P.ACC as string[][];
export const Footer = A.Footer as unknown as ComponentType<{ light?: boolean }>;
