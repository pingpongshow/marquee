import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { Clapperboard } from "lucide-react";
export function AuthLayout({ title, subtitle, children }) {
    return (_jsx("div", { className: "flex min-h-full items-center justify-center bg-[radial-gradient(ellipse_at_top,_#2a2418_0%,_#121316_60%)] p-4", children: _jsxs("div", { className: "w-full max-w-sm", children: [_jsxs("div", { className: "mb-8 flex flex-col items-center gap-3 text-center", children: [_jsx(Clapperboard, { className: "size-12 text-accent", "aria-hidden": true }), _jsx("h1", { className: "text-2xl font-bold", children: title }), subtitle && _jsx("p", { className: "text-sm text-muted", children: subtitle })] }), _jsx("div", { className: "rounded-xl border border-border bg-surface p-6 shadow-xl", children: children })] }) }));
}
