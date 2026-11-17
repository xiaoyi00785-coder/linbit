"use client";
/** 全站浮层：对话框、底部抽屉、下拉、币种选择、选择器、提示、筛选抽屉、数字键盘。 */
import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { toast } from "@/lib/ui";

export { toast };

function gate(node: React.ReactNode) {
  if (typeof document === "undefined") return null;
  return createPortal(node, document.body);
}

function Mask({ align, dismissible = true, label, onClose, children }: { align: "center" | "bottom"; dismissible?: boolean; label: string; onClose: () => void; children: React.ReactNode }) {
  return gate(
    <div className={"ov-mask fx-modal " + align} role="presentation" data-dismissible={dismissible ? undefined : "false"} onClick={onClose}>
      <div className={align === "center" ? "ov-dialog" : "ov-sheet"} role="dialog" aria-modal="true" aria-label={label} onClick={(e) => e.stopPropagation()}>
        {children}
      </div>
    </div>,
  );
}

export function Dialog({ title, onClose, children, foot, dismissible = true }: { title: string; onClose: () => void; children: React.ReactNode; foot?: React.ReactNode; dismissible?: boolean }) {
  return (
    <Mask align="center" label={title} onClose={onClose} dismissible={dismissible}>
      <div className="ov-h"><b>{title}</b><button type="button" className="ov-x tk-x" aria-label="关闭" onClick={onClose}>✕</button></div>
      {children}
      {foot && <div className="ov-foot">{foot}</div>}
    </Mask>
  );
}

export function BottomSheet({ title, onClose, children, foot, dismissible = true }: { title: string; onClose: () => void; children: React.ReactNode; foot?: React.ReactNode; dismissible?: boolean }) {
  return (
    <Mask align="bottom" label={title} onClose={onClose} dismissible={dismissible}>
      <div className="ov-grip" aria-hidden="true" />
      <div className="ov-h"><b>{title}</b><button type="button" className="ov-x tk-x" aria-label="关闭" onClick={onClose}>✕</button></div>
      {children}
      {foot && <div className="ov-foot">{foot}</div>}
    </Mask>
  );
}

export function Picker({ open, title, options, value, onClose, onPick }: { open: boolean; title: string; options: string[]; value: string; onClose: () => void; onPick: (v: string) => void }) {
  if (!open) return null;
  return (
    <BottomSheet title={title} onClose={onClose}>
      <div className="ov-list" role="listbox" aria-label={title}>
        {options.map((x) => (
          <button type="button" key={x} role="option" aria-selected={x === value} className={"ov-it" + (x === value ? " on" : "")} onClick={() => onPick(x)}>{x}</button>
        ))}
      </div>
    </BottomSheet>
  );
}

export function CoinSelector({ open, title = "选择币种", coins, value, onClose, onPick }: { open: boolean; title?: string; coins: string[]; value?: string; onClose: () => void; onPick: (v: string) => void }) {
  const [q, setQ] = useState("");
  useEffect(() => { if (open) setQ(""); }, [open]);
  if (!open) return null;
  const list = ["全部", ...coins.filter((c) => !q || c.toUpperCase().includes(q.toUpperCase()))];
  return (
    <BottomSheet title={title} onClose={onClose}>
      <input className="ov-search" placeholder="搜索" aria-label="搜索币种" value={q} onChange={(e) => setQ(e.target.value.toUpperCase())} />
      <div className="ov-list" role="listbox" aria-label={title}>
        {list.length === 0 && <div className="ov-empty">没有匹配的币种</div>}
        {list.map((x) => (
          <button type="button" key={x} role="option" aria-selected={(x === "全部" ? !value : x === value)} className={"ov-it" + ((x === "全部" ? !value : x === value) ? " on" : "")} onClick={() => onPick(x === "全部" ? "" : x)}>{x}</button>
        ))}
      </div>
    </BottomSheet>
  );
}

export function FilterSheet({ open, title = "筛选", groups, onClose, onApply }: { open: boolean; title?: string; groups: { key: string; label: string; value: string; options: string[] }[]; onClose: () => void; onApply: (next: Record<string, string>) => void }) {
  const [draft, setDraft] = useState<Record<string, string>>({});
  useEffect(() => {
    if (open) setDraft(Object.fromEntries(groups.map((g) => [g.key, g.value])));
    // 只在打开时抄一份当前值
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  if (!open) return null;
  return (
    <BottomSheet title={title} onClose={onClose} foot={<>
      <button type="button" className="ov-b2" onClick={() => setDraft(Object.fromEntries(groups.map((g) => [g.key, g.options[0]])))}>重置</button>
      <button type="button" className="ov-b1" onClick={() => onApply(draft)}>确定</button>
    </>}>
      {groups.map((g) => (
        <div key={g.key} className="ov-group">
          <div className="ov-lab">{g.label}</div>
          <div className="ov-chips">
            {g.options.map((x) => (
              <button type="button" key={x} className={"ov-chip" + (draft[g.key] === x ? " on" : "")} onClick={() => setDraft((d) => ({ ...d, [g.key]: x }))}>{x}</button>
            ))}
          </div>
        </div>
      ))}
    </BottomSheet>
  );
}

const KEYS = ["1", "2", "3", "4", "5", "6", "7", "8", "9", ".", "0", "del"];
export function NumericKeypad({ open, title = "输入数量", value, onClose, onChange, onConfirm }: { open: boolean; title?: string; value: string; onClose: () => void; onChange: (v: string) => void; onConfirm: () => void }) {
  if (!open) return null;
  const press = (k: string) => {
    if (k === "del") return onChange(value.slice(0, -1));
    if (k === "." && value.includes(".")) return;
    const next = (value === "0" && k !== "." ? "" : value) + k;
    onChange(next.replace(/^0+(?=\d)/, ""));
  };
  return (
    <BottomSheet title={title} onClose={onClose} foot={<button type="button" className="ov-b1" onClick={onConfirm}>确定</button>}>
      <div className="ov-amt" aria-live="polite">{value || "0"}</div>
      <div className="ov-keys">
        {KEYS.map((k) => (
          <button type="button" key={k} onClick={() => press(k)}>{k === "del" ? "⌫" : k}</button>
        ))}
      </div>
    </BottomSheet>
  );
}
