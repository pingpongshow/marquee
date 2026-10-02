import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Lock } from "lucide-react";
import { useState } from "react";
import { api, deviceInfo, unwrap } from "@/api/client";
import { PinPad } from "@/components/PinPad";
import { Alert, Button, Field, Input, Spinner } from "@/components/ui";
import { useAuth } from "@/lib/auth";
import { Avatar } from "@/features/users/Avatar";
import { AuthLayout } from "./AuthLayout";
function PasswordForm({ onBack }) {
    const { signIn } = useAuth();
    const [username, setUsername] = useState("");
    const [password, setPassword] = useState("");
    const login = useMutation({
        mutationFn: () => unwrap(api.POST("/auth/login", { body: { username, password, device: deviceInfo() } })),
        onSuccess: (res) => signIn(res.token),
    });
    const submit = (e) => {
        e.preventDefault();
        login.mutate();
    };
    return (_jsxs("form", { onSubmit: submit, className: "space-y-4", children: [login.isError && _jsx(Alert, { tone: "error", children: login.error.message }), _jsx(Field, { label: "Username", children: (id) => _jsx(Input, { id: id, autoComplete: "username", autoFocus: true, required: true, value: username, onChange: (e) => setUsername(e.target.value) }) }), _jsx(Field, { label: "Password", children: (id) => _jsx(Input, { id: id, type: "password", autoComplete: "current-password", required: true, value: password, onChange: (e) => setPassword(e.target.value) }) }), _jsx(Button, { variant: "primary", type: "submit", className: "w-full", loading: login.isPending, children: "Sign in" }), onBack && (_jsx("button", { type: "button", onClick: onBack, className: "w-full text-center text-sm text-muted hover:text-text", children: "Back to profiles" }))] }));
}
/** Plex-style "Who's watching?": choose a profile, then its PIN (or password). */
function ProfilePicker({ profiles, onUsePassword }) {
    const { signIn } = useAuth();
    const [chosen, setChosen] = useState(null);
    const [password, setPassword] = useState("");
    const [attempt, setAttempt] = useState(0);
    const login = useMutation({
        mutationFn: (p) => unwrap(api.POST("/auth/pin", { body: { ...p, device: deviceInfo() } })),
        onSuccess: (res) => signIn(res.token),
        onError: () => setAttempt((n) => n + 1),
    });
    const choose = (p) => {
        login.reset();
        if (p.requires === "none")
            login.mutate({ userId: p.id });
        else
            setChosen(p);
    };
    if (!chosen)
        return (_jsxs("div", { className: "space-y-6", children: [login.isError && _jsx(Alert, { tone: "error", children: login.error.message }), _jsx("ul", { className: "grid grid-cols-3 gap-4", children: profiles.map((p) => (_jsx("li", { children: _jsxs("button", { onClick: () => choose(p), disabled: login.isPending, className: "group flex w-full flex-col items-center gap-2 rounded-lg p-2 hover:bg-surface-2", children: [_jsx(Avatar, { name: p.displayName, url: p.avatarUrl, className: "size-16 text-2xl group-hover:ring-2 group-hover:ring-accent", children: p.requires !== "none" && _jsx(Lock, { className: "absolute -right-0.5 -bottom-0.5 size-5 rounded-full bg-surface p-1 text-muted", "aria-label": "Locked" }) }), _jsx("span", { className: "max-w-full truncate text-sm", children: p.displayName })] }) }, p.id))) }), _jsx("button", { onClick: onUsePassword, className: "w-full text-center text-sm text-muted hover:text-text", children: "Sign in with username and password" })] }));
    return (_jsxs("div", { className: "space-y-5", children: [_jsxs("div", { className: "text-center", children: [_jsx(Avatar, { name: chosen.displayName, url: chosen.avatarUrl, className: "mx-auto mb-2 size-16 text-2xl" }), _jsx("div", { className: "font-medium", children: chosen.displayName })] }), login.isError && _jsx(Alert, { tone: "error", children: login.error.message }), chosen.requires === "pin" ? (_jsx(PinPad, { disabled: login.isPending, resetKey: attempt, onComplete: (pin) => login.mutate({ userId: chosen.id, pin }) })) : (_jsxs("form", { className: "space-y-4", onSubmit: (e) => {
                    e.preventDefault();
                    login.mutate({ userId: chosen.id, password });
                }, children: [_jsx(Input, { type: "password", "aria-label": "Password", placeholder: "Password", autoFocus: true, autoComplete: "current-password", value: password, onChange: (e) => setPassword(e.target.value) }), _jsx(Button, { variant: "primary", type: "submit", className: "w-full", loading: login.isPending, children: "Sign in" })] })), login.isPending && _jsx(Spinner, { label: "Signing in" }), _jsx("button", { onClick: () => (setChosen(null), setPassword(""), login.reset()), className: "w-full text-center text-sm text-muted hover:text-text", children: "Choose a different profile" })] }));
}
export function LoginPage({ serverName, pinSignIn }) {
    const [usePassword, setUsePassword] = useState(false);
    const profiles = useQuery({
        queryKey: ["auth", "profiles"],
        queryFn: () => unwrap(api.GET("/auth/profiles")),
        enabled: pinSignIn,
    });
    const showPicker = pinSignIn && !usePassword && (profiles.data?.length ?? 0) > 0;
    return (_jsx(AuthLayout, { title: showPicker ? "Who's watching?" : `Sign in to ${serverName}`, subtitle: showPicker ? serverName : undefined, children: pinSignIn && profiles.isPending ? (_jsx(Spinner, {})) : showPicker ? (_jsx(ProfilePicker, { profiles: profiles.data, onUsePassword: () => setUsePassword(true) })) : (_jsx(PasswordForm, { onBack: pinSignIn && (profiles.data?.length ?? 0) > 0 ? () => setUsePassword(false) : undefined })) }));
}
