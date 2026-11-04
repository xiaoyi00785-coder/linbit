// node --experimental-strip-types scripts/check-safe-redirect.mjs
import assert from "node:assert/strict";
import { safeRedirectFrom } from "../lib/safeRedirect.ts";
const O = "https://linbit.example";
const q = (raw) => "?redirect=" + raw; // raw 为已编码的查询值，模拟浏览器地址栏
const bad = ["//evil.com", "/\\evil.com", "/%09/evil.com", "/%0a/evil.com", "/%5Cevil.com", "https://evil.com", "javascript:alert(1)", "%2F%2Fevil.com", "/%0d/evil.com", ""];
for (const b of bad) { const got = safeRedirectFrom(q(b), O); assert.equal(got, "", `${b} -> ${got}`); console.log(`✓ ${JSON.stringify(b)} -> ""`); }
const ok = [["/earn/vip%23vapply", "/earn/vip#vapply"], [encodeURIComponent("/earn/vip#vapply"), "/earn/vip#vapply"], [encodeURIComponent("/support/tickets?id=2"), "/support/tickets?id=2"]];
for (const [raw, want] of ok) { const got = safeRedirectFrom(q(raw), O); assert.equal(got, want); console.log(`✓ ${JSON.stringify(raw)} -> ${JSON.stringify(got)}`); }
console.log("✓ safeRedirect 检查通过");
