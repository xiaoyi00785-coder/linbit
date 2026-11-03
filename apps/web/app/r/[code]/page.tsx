import { redirect } from "next/navigation";

// Invite short link: https://www.cowa.click/r/{code} → /register?inviteCode={code}
export default function InviteRedirect({ params }: { params: { code: string } }) {
  let code = params.code;
  try {
    code = decodeURIComponent(code);
  } catch {}
  redirect(`/register?inviteCode=${encodeURIComponent(code.trim().slice(0, 32))}`);
}
