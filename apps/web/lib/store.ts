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

/**
 * 认证状态（Zustand + localStorage 持久化）。
 * 替代原型里基于 localStorage 事件的 auth.js。
 */
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
