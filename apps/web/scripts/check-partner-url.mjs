// node --experimental-strip-types scripts/check-partner-url.mjs
import assert from "node:assert/strict";
import { partnerBase, partnerUrl, PARTNER_URL_DEFAULT } from "../lib/partnerUrl.mjs";
import nextConfig from "../next.config.mjs";

assert.equal(PARTNER_URL_DEFAULT, "https://partner.cowa.click");
// 未设置 / 空 / 非法 -> 默认值（主网无需配置）
for (const v of [undefined, null, "", "   ", "partner.cowa.click", "/partner", "javascript:alert(1)", "http://evil.com", "ftp://x.y", "https://", "https://a b.com"]) {
  assert.equal(partnerBase(v), PARTNER_URL_DEFAULT, JSON.stringify(v));
}
// 合法覆盖；去掉末尾斜杠
assert.equal(partnerBase("https://partner.example.com"), "https://partner.example.com");
assert.equal(partnerBase(" https://partner.example.com/// "), "https://partner.example.com");
assert.equal(partnerBase("http://localhost:3002"), "http://localhost:3002");
assert.equal(partnerBase("http://127.0.0.1:3002/"), "http://127.0.0.1:3002");
// 链接
assert.equal(partnerUrl(undefined, "/"), "https://partner.cowa.click/");
assert.equal(partnerUrl(undefined, "/apply"), "https://partner.cowa.click/apply");
assert.equal(partnerUrl("https://p.example.com/", "apply"), "https://p.example.com/apply");
assert.equal(partnerUrl(undefined), "https://partner.cowa.click/");
// next.config 的重定向：/partner 与 /partner/apply 指向合伙人站点
const rs = await nextConfig.redirects();
const find = (s) => rs.find((r) => r.source === s);
assert.deepEqual(find("/partner"), { source: "/partner", destination: partnerUrl(process.env.NEXT_PUBLIC_PARTNER_URL, "/"), permanent: false });
assert.deepEqual(find("/partner/apply"), { source: "/partner/apply", destination: partnerUrl(process.env.NEXT_PUBLIC_PARTNER_URL, "/apply"), permanent: false });
assert.ok(rs.some((r) => r.source === "/earn/wealth"), "existing redirects are kept");
console.log("partner-url ok");
