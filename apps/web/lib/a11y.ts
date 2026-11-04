import type { KeyboardEvent } from "react";
/** 键盘激活：Enter / 空格触发，等价于点击 */
export const onActivate = (fn: () => void) => (e: KeyboardEvent) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); fn(); } };
/** 选项卡：role=tab + tabIndex + aria-selected；Enter/空格选中，左右方向键在同组选项卡间移动焦点 */
export const tabProps = (on: boolean, fn: () => void) => ({
  role: "tab" as const, tabIndex: on ? 0 : -1, "aria-selected": on,
  onKeyDown: (e: KeyboardEvent<HTMLElement>) => {
    if (e.key === "ArrowRight" || e.key === "ArrowLeft") {
      const L = [...(e.currentTarget.parentElement?.querySelectorAll<HTMLElement>('[role="tab"]') ?? [])];
      const i = L.indexOf(e.currentTarget); const n = L[(i + (e.key === "ArrowRight" ? 1 : -1) + L.length) % L.length];
      e.preventDefault(); n?.focus(); n?.click();
    } else onActivate(fn)(e);
  },
});
/** 非 button 元素的按钮语义 */
export const btnProps = (fn: () => void, label?: string) => ({ role: "button" as const, tabIndex: 0, onKeyDown: onActivate(fn), ...(label ? { "aria-label": label } : {}) });
