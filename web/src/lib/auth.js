import { jsx as _jsx } from "react/jsx-runtime";
import { useQueryClient } from "@tanstack/react-query";
import { createContext, useCallback, useContext, useEffect, useState } from "react";
import { api, session, unwrap } from "@/api/client";
const AuthContext = createContext(null);
export function AuthProvider({ children }) {
    const qc = useQueryClient();
    const [authed, setAuthed] = useState(() => session.token !== null);
    useEffect(() => session.onUnauthorized(() => {
        setAuthed(false);
        qc.clear();
    }), [qc]);
    const signIn = useCallback((token) => {
        session.set(token);
        qc.clear();
        setAuthed(true);
    }, [qc]);
    const signOut = useCallback(async () => {
        try {
            await unwrap(api.POST("/auth/logout"));
        }
        catch {
            /* already invalid — sign out locally regardless */
        }
        session.set(null);
        qc.clear();
        setAuthed(false);
    }, [qc]);
    return _jsx(AuthContext.Provider, { value: { isAuthenticated: authed, signIn, signOut }, children: children });
}
export function useAuth() {
    const ctx = useContext(AuthContext);
    if (!ctx)
        throw new Error("useAuth outside AuthProvider");
    return ctx;
}
