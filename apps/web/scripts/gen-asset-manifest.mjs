// 生成静态资源内容哈希清单：public/coins/* 与 public/img/* → lib/asset-manifest.json
// 资源 URL 加 ?v=<hash>，文件内容变化即换 URL，绕过 CDN / 浏览器长缓存。由 predev / prebuild 自动执行。
import { createHash } from "node:crypto";
import { readdirSync, readFileSync, writeFileSync, existsSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const out = {};
for (const dir of ["coins", "img"]) {
  const abs = join(root, "public", dir);
  if (!existsSync(abs)) continue;
  for (const f of readdirSync(abs).sort()) {
    if (!/\.(svg|webp|png|jpe?g|gif|avif)$/i.test(f)) continue;
    out[`/${dir}/${f}`] = createHash("sha256").update(readFileSync(join(abs, f))).digest("hex").slice(0, 8);
  }
}
const file = join(root, "lib", "asset-manifest.json");
const json = JSON.stringify(out, null, 0).replace(/,"/g, ',\n"') + "\n";
if (!existsSync(file) || readFileSync(file, "utf8") !== json) writeFileSync(file, json);
console.log(`asset-manifest: ${Object.keys(out).length} files`);
