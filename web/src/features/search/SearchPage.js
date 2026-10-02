import { jsxs as _jsxs, jsx as _jsx } from "react/jsx-runtime";
import { useQuery } from "@tanstack/react-query";
import { Link, useSearch } from "@tanstack/react-router";
import { searchQuery } from "@/api/queries";
import { Spinner } from "@/components/ui";
import { Poster } from "../browse/Poster";
import { groupLabels, resultSubtitle } from "./SearchBox";
export function SearchPage() {
    const { q } = useSearch({ from: "/search" });
    const results = useQuery(searchQuery(q, 50));
    return (_jsxs("div", { className: "p-6 lg:p-8", children: [_jsxs("h1", { className: "mb-6 text-2xl font-bold", children: ["Results for \u201C", q, "\u201D"] }), results.isPending && _jsx(Spinner, {}), results.data?.groups.length === 0 && _jsx("p", { className: "text-muted", children: "Nothing found." }), results.data?.groups.map((g) => (_jsxs("section", { className: "mb-10", children: [_jsx("h2", { className: "mb-4 text-lg font-semibold", children: groupLabels[g.type] }), _jsx("ul", { className: "grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-x-4 gap-y-6", children: g.items.map((it) => (_jsx("li", { children: _jsxs(Link, { to: "/item/$itemId", params: { itemId: String(it.id) }, className: "group block", children: [_jsx(Poster, { item: it, shape: it.type === "episode" ? "wide" : it.type === "artist" || it.type === "album" || it.type === "track" ? "square" : "poster", className: "group-hover:ring-2 group-hover:ring-accent" }), _jsx("div", { className: "mt-2 truncate text-sm font-medium", children: it.title }), _jsx("div", { className: "truncate text-xs text-muted", children: resultSubtitle(it) })] }) }, it.id))) })] }, g.type)))] }));
}
