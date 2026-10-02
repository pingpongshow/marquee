import { useQueryClient } from "@tanstack/react-query";
import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { api, session, unwrap } from "@/api/client";

type AuthState = {
  isAuthenticated: boolean;
  signIn: (token: string) => void;
  signOut: () => Promise<void>;
};

const AuthContext = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient();
  const [authed, setAuthed] = useState(() => session.token !== null);

  useEffect(
    () =>
      session.onUnauthorized(() => {
        setAuthed(false);
        qc.clear();
      }),
    [qc],
  );

  const signIn = useCallback(
    (token: string) => {
      session.set(token);
      qc.clear();
      setAuthed(true);
    },
    [qc],
  );

  const signOut = useCallback(async () => {
    try {
      await unwrap(api.POST("/auth/logout"));
    } catch {
      /* already invalid — sign out locally regardless */
    }
    session.set(null);
    qc.clear();
    setAuthed(false);
  }, [qc]);

  return <AuthContext.Provider value={{ isAuthenticated: authed, signIn, signOut }}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth outside AuthProvider");
  return ctx;
}
