import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useParams } from "@tanstack/react-router";
import { Construction } from "lucide-react";
import { findSection } from "./sections";
export function SettingsSectionPage() {
    const { section } = useParams({ from: "/settings/$section" });
    const s = findSection(section);
    if (!s)
        return _jsx("p", { className: "text-muted", children: "Unknown settings section." });
    const Body = s.component;
    return (_jsxs("div", { className: "mx-auto max-w-3xl pb-8", children: [_jsx("h1", { className: "text-2xl font-bold", children: s.label }), _jsx("p", { className: "mt-1 mb-6 text-muted", children: s.description }), Body ? (_jsx(Body, {})) : (_jsxs("div", { className: "flex items-center gap-3 rounded-lg border border-dashed border-border p-6 text-muted", children: [_jsx(Construction, { className: "size-5", "aria-hidden": true }), "This section arrives in milestone ", s.milestone, ". See docs/05-roadmap.md."] }))] }));
}
