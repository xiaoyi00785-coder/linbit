import manifest from "./asset-manifest.json";
const M = manifest as Record<string, string>;
/** 给 public 下的静态资源加内容哈希版本号（见 scripts/gen-asset-manifest.mjs）；清单缺项时原样返回 */
export function asset(path: string): string {
  const v = M[path];
  return v ? `${path}?v=${v}` : path;
}
