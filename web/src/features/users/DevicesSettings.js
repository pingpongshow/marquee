import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useQuery } from "@tanstack/react-query";
import { Globe, LogOut, Monitor, Smartphone, Tablet, Tv } from "lucide-react";
import { devicesQuery, useRevokeDevice } from "@/api/queries";
import { Alert, Button, Card, Spinner } from "@/components/ui";
const icons = { web: Globe, ios: Smartphone, android: Smartphone, ipados: Tablet, tvos: Tv, androidtv: Tv };
function ago(iso) {
    const s = (Date.now() - new Date(iso).getTime()) / 1000;
    if (s < 120)
        return "Active now";
    if (s < 3600)
        return `${Math.round(s / 60)} min ago`;
    if (s < 86400)
        return `${Math.round(s / 3600)} hr ago`;
    return new Date(iso).toLocaleDateString();
}
export function DevicesSettings() {
    const devices = useQuery(devicesQuery);
    const revoke = useRevokeDevice();
    return (_jsxs(Card, { title: "Signed-in devices", description: "Signing a device out requires it to log in again.", children: [devices.isPending && _jsx(Spinner, {}), revoke.isError && _jsx(Alert, { tone: "error", children: revoke.error.message }), _jsx("ul", { className: "-my-2 divide-y divide-border", children: devices.data?.map((d) => {
                    const Icon = icons[d.platform] ?? Monitor;
                    return (_jsxs("li", { className: "flex items-center gap-4 py-3", children: [_jsx(Icon, { className: "size-6 shrink-0 text-muted", "aria-hidden": true }), _jsxs("div", { className: "min-w-0 flex-1", children: [_jsxs("div", { className: "flex items-center gap-2", children: [_jsx("span", { className: "truncate font-medium", children: d.name }), d.current && _jsx("span", { className: "rounded bg-accent/15 px-1.5 py-0.5 text-[11px] font-medium text-accent", children: "This device" })] }), _jsxs("div", { className: "truncate text-xs text-muted", children: [d.userName, " \u00B7 ", d.product ?? d.platform, d.version && d.version !== "dev" ? ` ${d.version}` : "", " \u00B7 ", ago(d.lastSeenAt), d.lastIp ? ` · ${d.lastIp}` : ""] })] }), !d.current && (_jsxs(Button, { size: "sm", variant: "ghost", onClick: () => revoke.mutate(d.id), "aria-label": `Sign out ${d.name}`, children: [_jsx(LogOut, { className: "size-4" }), " Sign out"] }))] }, d.id));
                }) })] }));
}
