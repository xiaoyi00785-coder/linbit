/** 让 Node 解析本仓库中无后缀的相对 .ts import（仅供 check 脚本）。 */
import { pathToFileURL } from "node:url";
export async function resolve(specifier, context, nextResolve) {
  if (specifier.startsWith(".") && !/\.[cm]?[jt]sx?$/.test(specifier)) {
    try { return await nextResolve(specifier + ".ts", context); } catch { /* fall through */ }
  }
  return nextResolve(specifier, context);
}
