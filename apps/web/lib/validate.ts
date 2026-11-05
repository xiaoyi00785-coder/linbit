/** 表单校验工具：提现地址格式、数量输入（负数/非法字符给出提示，不静默改写） */

const B58 = "[1-9A-HJ-NP-Za-km-z]";
const EVM = /^0x[0-9a-fA-F]{40}$/;
const RULES: [RegExp, RegExp, string][] = [
  [/TRC20|TRON/i, new RegExp(`^T${B58}{33}$`), "TRC20 地址以 T 开头，共 34 位"],
  [/ERC20|BEP20|BSC|Arbitrum|Optimism|Polygon|Base|AVAX|C-Chain/i, EVM, "EVM 地址以 0x 开头，后接 40 位十六进制字符"],
  [/Solana|SOL/i, new RegExp(`^${B58}{32,44}$`), "Solana 地址为 32–44 位 Base58 字符"],
  [/Bitcoin|BTC/i, new RegExp(`^(bc1[02-9ac-hj-np-z]{11,71}|[13]${B58}{25,34})$`), "比特币地址以 1、3 或 bc1 开头"],
];

/** 返回错误文案；空字符串表示通过（空地址也返回空，由调用方决定是否必填） */
export function addressError(network: string, addr: string): string {
  if (!addr) return "";
  if (/\s/.test(addr)) return "地址不能包含空格";
  const r = RULES.find(([n]) => n.test(network));
  if (r) return r[1].test(addr) ? "" : `地址格式不正确：${r[2]}`;
  return addr.length >= 20 ? "" : "地址格式不正确";
}

/** 数量输入校验：允许空；负号、非数字、多个小数点都给提示 */
export function amountError(raw: string, opts: { dp?: number } = {}): string {
  if (raw === "") return "";
  if (/^\s*-/.test(raw)) return "数量不能为负数";
  if (!/^\d*\.?\d*$/.test(raw) || raw === ".") return "请输入有效的数字";
  if (opts.dp != null) { const d = raw.split(".")[1]; if (d && d.length > opts.dp) return `最多 ${opts.dp} 位小数`; }
  if (Number(raw) <= 0) return "数量必须大于 0";
  return "";
}

/** 数量输入框 onChange：只去掉空白，保留用户输入以便提示 */
export const cleanNum = (v: string) => v.replace(/[\s,，]/g, "");
