import { redirect } from "next/navigation";

/** /earn/flexible 无对应页面，统一到简单赚币 */
export default function Page() {
  redirect("/earn/simple");
}
