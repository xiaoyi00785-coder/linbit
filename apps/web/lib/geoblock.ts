// 网关对受限国家/地区返回 451（REGION_BLOCKED）：任何请求命中即跳转 /blocked。
let installed = false;
export function installGeoBlockHandler() {
  if (installed || typeof window === "undefined") return;
  installed = true;
  const orig = window.fetch.bind(window);
  window.fetch = async (...args: Parameters<typeof fetch>) => {
    const r = await orig(...args);
    if (r.status === 451 && !window.location.pathname.startsWith("/blocked")) window.location.replace("/blocked");
    return r;
  };
}
