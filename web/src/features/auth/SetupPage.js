import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { api, deviceInfo, unwrap } from "@/api/client";
import { systemInfoQuery } from "@/api/queries";
import { Alert, Button, Field, Input } from "@/components/ui";
import { useAuth } from "@/lib/auth";
import { AuthLayout } from "./AuthLayout";
/** First-run setup (ADM-1): name the server and create the admin account. */
export function SetupPage() {
    const { signIn } = useAuth();
    const qc = useQueryClient();
    const navigate = useNavigate();
    const [serverName, setServerName] = useState("Marquee");
    const [username, setUsername] = useState("");
    const [password, setPassword] = useState("");
    const [confirm, setConfirm] = useState("");
    const mismatch = confirm.length > 0 && confirm !== password;
    const setup = useMutation({
        mutationFn: () => unwrap(api.POST("/setup", { body: { serverName, username, password, device: deviceInfo() } })),
        onSuccess: async (res) => {
            signIn(res.token);
            await qc.invalidateQueries({ queryKey: systemInfoQuery.queryKey });
            navigate({ to: "/settings/$section", params: { section: "libraries" } });
        },
    });
    const submit = (e) => {
        e.preventDefault();
        if (!mismatch)
            setup.mutate();
    };
    return (_jsx(AuthLayout, { title: "Welcome to Marquee", subtitle: "Let\u2019s set up your server. You\u2019ll add libraries next.", children: _jsxs("form", { onSubmit: submit, className: "space-y-4", children: [setup.isError && _jsx(Alert, { tone: "error", children: setup.error.message }), _jsx(Field, { label: "Server name", help: "Shown to every app that connects.", children: (id) => _jsx(Input, { id: id, required: true, maxLength: 64, value: serverName, onChange: (e) => setServerName(e.target.value) }) }), _jsx(Field, { label: "Admin username", children: (id) => _jsx(Input, { id: id, autoComplete: "username", required: true, maxLength: 64, value: username, onChange: (e) => setUsername(e.target.value) }) }), _jsx(Field, { label: "Password", help: "At least 8 characters.", children: (id) => _jsx(Input, { id: id, type: "password", autoComplete: "new-password", required: true, minLength: 8, value: password, onChange: (e) => setPassword(e.target.value) }) }), _jsx(Field, { label: "Confirm password", error: mismatch ? "Passwords don’t match" : undefined, children: (id) => _jsx(Input, { id: id, type: "password", autoComplete: "new-password", required: true, value: confirm, onChange: (e) => setConfirm(e.target.value) }) }), _jsx(Button, { variant: "primary", type: "submit", className: "w-full", loading: setup.isPending, disabled: mismatch, children: "Create server" })] }) }));
}
