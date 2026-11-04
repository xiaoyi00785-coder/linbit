/** 站内客服工单（/support/*，见 openapi Support）。所有工单内容一律按纯文本渲染，不做链接识别。 */
import { API_BASE } from "./api";
import { rest, rget, refreshAccess, type RestResult } from "./rest";
import { useAuthStore } from "./store";
import type { components } from "@linbit/api-client";

export type Ticket = components["schemas"]["SupportTicket"];
export type TicketList = components["schemas"]["SupportTicketList"];
export type TicketCategory = components["schemas"]["SupportTicketCategory"];
export type TicketAttachment = components["schemas"]["SupportAttachment"];

export const CATEGORIES: [TicketCategory, string][] = [["account", "账户"], ["deposit_withdraw", "充值与提现"], ["trade", "交易"], ["kyc", "身份认证"], ["security", "账户安全"], ["other", "其他"]];
export const catName = (c?: string) => CATEGORIES.find(([k]) => k === c)?.[1] || c || "--";
export const STATUS: Record<string, [string, string]> = { open: ["待处理", ""], replied: ["客服已回复", "ok"], closed: ["已关闭", "mut"] };
export const ATTACH_MAX = 3;
export const ATTACH_BYTES = 5 * 1024 * 1024;
export const ATTACH_TYPES = ["image/jpeg", "image/png", "image/webp"];

const ERR: Record<string, string> = {
  RATE_LIMITED: "提交过于频繁，请稍后再试", TICKET_CLOSED: "工单已关闭，如有新问题请重新提交", ATTACHMENT_INVALID: "附件无效或已被使用",
  FILE_TOO_LARGE: "图片不能超过 5 MB", UNSUPPORTED_MEDIA_TYPE: "仅支持 JPG / PNG / WEBP 图片", NOT_FOUND: "工单不存在",
};
export const ticketErr = (r: RestResult<unknown>) => (r.code && ERR[r.code]) || r.error || "操作失败";

export const listTickets = (page = 1, status = "") => rget<TicketList>(`/support/tickets?page=${page}&pageSize=20${status ? "&status=" + status : ""}`);
export const getTicket = (id: string) => rget<Ticket>("/support/tickets/" + encodeURIComponent(id));
export const createTicket = (b: { category: TicketCategory; subject: string; content: string; attachments: string[] }) => rest<Ticket>("POST", "/support/tickets", b);
export const replyTicket = (id: string, content: string, attachments: string[]) => rest<Ticket>("POST", `/support/tickets/${encodeURIComponent(id)}/messages`, { content, attachments });
export const closeTicket = (id: string) => rest<Ticket>("POST", `/support/tickets/${encodeURIComponent(id)}/close`, {});

async function authed(path: string, init: RequestInit = {}, retried = false): Promise<Response> {
  const tok = useAuthStore.getState().accessToken;
  const r = await fetch(API_BASE + path, { ...init, headers: { ...(init.headers || {}), ...(tok ? { Authorization: `Bearer ${tok}` } : {}) } });
  if (r.status === 401 && tok && !retried && (await refreshAccess())) return authed(path, init, true);
  return r;
}

export async function uploadAttachment(f: File): Promise<RestResult<TicketAttachment>> {
  if (!ATTACH_TYPES.includes(f.type)) return { ok: false, code: "UNSUPPORTED_MEDIA_TYPE", error: ERR.UNSUPPORTED_MEDIA_TYPE, source: "api" };
  if (f.size > ATTACH_BYTES) return { ok: false, code: "FILE_TOO_LARGE", error: ERR.FILE_TOO_LARGE, source: "api" };
  const fd = new FormData(); fd.append("file", f);
  try {
    const r = await authed("/support/attachments", { method: "POST", body: fd });
    const j = await r.json().catch(() => ({}));
    return r.ok ? { ok: true, data: j, status: r.status, source: "api" } : { ok: false, code: j?.code, error: j?.message || `HTTP ${r.status}`, status: r.status, source: "api" };
  } catch { return { ok: false, error: "服务暂不可用", source: "demo" }; }
}

/** 附件是私有资源：带令牌取回后转成 blob URL（<img> 无法携带 Bearer）。 */
export async function attachmentBlobURL(id: string): Promise<string | null> {
  try { const r = await authed("/support/attachments/" + encodeURIComponent(id)); return r.ok ? URL.createObjectURL(await r.blob()) : null; } catch { return null; }
}
