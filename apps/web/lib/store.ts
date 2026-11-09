/** 登录态（Zustand + localStorage，键名 lb-auth），替代原型里的 auth.js 事件。
 *  只存 accessToken / refreshToken；过期换票在 rest.refreshAccess，失败则 logout。 */
import { create } from "zustand";
import { persist } from "zustand/middleware";

export interface AuthState {
  accessToken: string | null;
  refreshToken: string | null;
  email: string | null;
  isAuthed: boolean;
  login: (tokens: {
    accessToken: string;
    refreshToken: string;
    email?: string | null;
  }) => void;
  logout: () => void;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      accessToken: null,
      refreshToken: null,
      email: null,
      isAuthed: false,
      login: ({ accessToken, refreshToken, email = null }) =>
        set({ accessToken, refreshToken, email, isAuthed: true }),
      logout: () =>
        set({
          accessToken: null,
          refreshToken: null,
          email: null,
          isAuthed: false,
        }),
    }),
    { name: "lb-auth" },
  ),
);
