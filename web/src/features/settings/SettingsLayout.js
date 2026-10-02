import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useQuery } from "@tanstack/react-query";
import { Link, Outlet } from "@tanstack/react-router";
import { meQuery } from "@/api/queries";
import { Alert, Spinner } from "@/components/ui";
import { settingsGroups } from "./sections";
/** Plex-style settings: grouped section nav on the left, the selected section on the right (ADM-8). */
export function SettingsLayout() {
    const me = useQuery(meQuery);
    if (me.isPending)
        return _jsx(Spinner, {});
    if (!me.data?.isAdmin)
        return (_jsx("div", { className: "p-8", children: _jsx(Alert, { tone: "error", children: "Only administrators can change server settings." }) }));
    return (_jsxs("div", { className: "flex min-h-full flex-col md:flex-row", children: [_jsx("nav", { "aria-label": "Settings", className: "shrink-0 border-b border-border bg-surface/50 p-3 md:sticky md:top-0 md:h-[calc(100vh-3.5rem)] md:w-56 md:overflow-y-auto md:border-r md:border-b-0", children: settingsGroups.map((g) => (_jsxs("div", { className: "mb-4", children: [_jsx("div", { className: "mb-1 px-3 text-xs font-semibold tracking-wider text-faint uppercase", children: g.label }), _jsx("ul", { className: "flex flex-wrap gap-1 md:block md:space-y-0.5", children: g.sections.map((s) => (_jsx("li", { children: _jsxs(Link, { to: "/settings/$section", params: { section: s.id }, className: "flex items-center gap-2.5 rounded-md px-3 py-1.5 text-sm text-muted hover:bg-surface-2 hover:text-text", activeProps: { className: "bg-surface-2 !text-text font-medium" }, children: [_jsx(s.icon, { className: "size-4", "aria-hidden": true }), s.label, s.milestone && _jsx("span", { className: "ml-auto text-[10px] text-faint", children: s.milestone })] }) }, s.id))) })] }, g.label))) }), _jsx("div", { className: "min-w-0 flex-1 px-6 pt-6 lg:px-8 lg:pt-8", children: _jsx(Outlet, {}) })] }));
}
