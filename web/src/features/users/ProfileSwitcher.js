import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Lock } from "lucide-react";
import { useState } from "react";
import { api, deviceInfo, unwrap } from "@/api/client";
import { meQuery, profilesQuery } from "@/api/queries";
import { PinPad } from "@/components/PinPad";
import { Alert, Button, Dialog, Input, Spinner } from "@/components/ui";
import { useAuth } from "@/lib/auth";
import { Avatar } from "./Avatar";
/** Plex Home-style profile picker with an on-screen PIN pad. */
export function ProfileSwitcher({ onClose }) {
    const { signIn } = useAuth();
    const profiles = useQuery(profilesQuery);
    const me = useQuery(meQuery);
    const [target, setTarget] = useState(null);
    const [password, setPassword] = useState("");
    const sw = useMutation({
        mutationFn: (p) => unwrap(api.POST("/profiles/{userId}/switch", { params: { path: { userId: p.id } }, body: { pin: p.pin, password: p.password, device: deviceInfo() } })),
        onSuccess: (res) => {
            signIn(res.token);
            onClose();
        },
    });
    // Admins open managed profiles directly.
    const needs = (p) => (me.data?.isAdmin && p.isManaged ? "none" : p.requires);
    const choose = (p) => {
        if (p.id === me.data?.id)
            return onClose();
        if (needs(p) === "none")
            return sw.mutate({ id: p.id });
        setTarget(p);
    };
    return (_jsxs(Dialog, { open: true, onClose: onClose, title: target ? `Enter ${target.displayName}'s ${needs(target) === "pin" ? "PIN" : "password"}` : "Switch profile", children: [sw.isError && (_jsx("div", { className: "mb-4", children: _jsx(Alert, { tone: "error", children: sw.error.message }) })), !target ? (profiles.isPending ? (_jsx(Spinner, {})) : (_jsx("ul", { className: "grid grid-cols-3 gap-4", children: profiles.data?.map((p) => (_jsx("li", { children: _jsxs("button", { onClick: () => choose(p), className: "group flex w-full flex-col items-center gap-2 rounded-lg p-2 hover:bg-surface-2", disabled: sw.isPending, children: [_jsx(Avatar, { name: p.displayName, url: p.avatarUrl, className: "size-16 text-2xl group-hover:ring-2 group-hover:ring-accent", children: needs(p) !== "none" && _jsx(Lock, { className: "absolute -right-0.5 -bottom-0.5 size-5 rounded-full bg-surface p-1 text-muted", "aria-label": "Locked" }) }), _jsx("span", { className: "max-w-full truncate text-sm", children: p.displayName }), p.id === me.data?.id && _jsx("span", { className: "text-[11px] text-accent", children: "Current" })] }) }, p.id))) }))) : needs(target) === "pin" ? (_jsxs("div", { className: "flex flex-col items-center gap-5", children: [_jsx(PinPad, { disabled: sw.isPending, resetKey: sw.failureCount, onComplete: (pin) => sw.mutate({ id: target.id, pin }) }), _jsx(Button, { variant: "ghost", size: "sm", onClick: () => setTarget(null), children: "Back" })] })) : (_jsxs("form", { className: "space-y-4", onSubmit: (e) => {
                    e.preventDefault();
                    sw.mutate({ id: target.id, password });
                }, children: [_jsx(Input, { type: "password", autoFocus: true, autoComplete: "current-password", value: password, onChange: (e) => setPassword(e.target.value), "aria-label": "Password" }), _jsxs("div", { className: "flex justify-between", children: [_jsx(Button, { type: "button", variant: "ghost", onClick: () => setTarget(null), children: "Back" }), _jsx(Button, { type: "submit", variant: "primary", loading: sw.isPending, children: "Switch" })] })] }))] }));
}
