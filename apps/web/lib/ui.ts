/** 轻量 UI 反馈：toast 提示、复制到剪贴板。无需 Provider，直接挂到 body。 */
export type ToastKind = "ok" | "err" | "info";
export function toast(msg: string, kind: ToastKind = "ok", ms = 2200) {
  if (typeof document === "undefined") return;
  let box = document.getElementById("lb-toasts");
  if (!box) { box = document.createElement("div"); box.id = "lb-toasts"; document.body.appendChild(box); }
  const el = document.createElement("div");
  el.className = `lb-toast ${kind}`;
  el.innerHTML = `<i>${kind === "ok" ? "✓" : kind === "err" ? "!" : "i"}</i><span></span>`;
  (el.querySelector("span") as HTMLElement).textContent = msg;
  box.appendChild(el);
  setTimeout(() => { el.classList.add("out"); setTimeout(() => el.remove(), 300); }, ms);
}
export async function copyText(text: string, label = "已复制到剪贴板") {
  try {
    if (navigator.clipboard?.writeText) await navigator.clipboard.writeText(text);
    else { const t = document.createElement("textarea"); t.value = text; document.body.appendChild(t); t.select(); document.execCommand("copy"); t.remove(); }
    toast(label);
    return true;
  } catch { toast("复制失败，请手动复制", "err"); return false; }
}
/** 全局委托：任何带 data-copy 的元素点击即复制；带 data-toast 的元素点击即提示。 */
export function installUiDelegates() {
  if (typeof document === "undefined" || (window as any).__lbUi) return;
  (window as any).__lbUi = 1;
  document.addEventListener("click", (e) => {
    const t = (e.target as HTMLElement)?.closest?.("[data-copy],[data-toast]") as HTMLElement | null;
    if (!t) return;
    if (t.dataset.copy) { copyText(t.dataset.copy); t.classList.add("copied"); setTimeout(() => t.classList.remove("copied"), 1200); }
    else if (t.dataset.toast) toast(t.dataset.toast, (t.dataset.kind as ToastKind) || "ok");
  });
}
