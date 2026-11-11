import { redirect } from "next/navigation";

// 邀请短链 https://www.cowa.click/r/{code} → /register?inviteCode={code}；解码后 trim，最长 32 字符。
export default function InviteRedirect({ params }: { params: { code: string } }) {
  let code = params.code;
  try {
    code = decodeURIComponent(code);
  } catch {}
  redirect(`/register?inviteCode=${encodeURIComponent(code.trim().slice(0, 32))}`);
}
