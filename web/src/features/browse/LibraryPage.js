import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useState } from "react";
import { libraryItemsQuery, librariesQuery } from "@/api/queries";
import { Alert, Button, Select, Spinner } from "@/components/ui";
import { Poster } from "./Poster";
import { subtitleFor } from "./format";
const PAGE = 120;
export function LibraryPage() {
    const { libraryId } = useParams({ from: "/library/$libraryId" });
    const id = Number(libraryId);
    const [sort, setSort] = useState("title");
    const [limit, setLimit] = useState(PAGE);
    const lib = useQuery(librariesQuery).data?.find((l) => l.id === id);
    const items = useQuery({ ...libraryItemsQuery(id, sort, 0, limit), placeholderData: (prev) => prev });
    const shape = lib?.type === "music" ? "square" : lib?.type === "videos" ? "wide" : "poster";
    return (_jsxs("div", { className: "p-6 lg:p-8", children: [_jsxs("div", { className: "mb-6 flex flex-wrap items-center gap-4", children: [_jsx("h1", { className: "text-2xl font-bold", children: lib?.name ?? "Library" }), items.data && _jsxs("span", { className: "text-sm text-muted", children: [items.data.total.toLocaleString(), " items"] }), _jsx("div", { className: "ml-auto w-48", children: _jsxs(Select, { "aria-label": "Sort by", value: sort, onChange: (e) => setSort(e.target.value), children: [_jsx("option", { value: "title", children: "Title" }), _jsx("option", { value: "-added", children: "Recently added" }), _jsx("option", { value: "-year", children: "Year (newest)" }), _jsx("option", { value: "year", children: "Year (oldest)" })] }) })] }), items.isPending && _jsx(Spinner, {}), items.isError && _jsx(Alert, { tone: "error", children: items.error.message }), items.data?.total === 0 && (_jsxs("p", { className: "text-muted", children: ["Nothing here yet. ", lib?.scanStatus !== "idle" ? "A scan is in progress." : "Scan the library from Settings → Libraries."] })), _jsx("ul", { className: shape === "wide" ? "grid grid-cols-[repeat(auto-fill,minmax(240px,1fr))] gap-x-4 gap-y-6" : "grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-x-4 gap-y-6", children: items.data?.items.map((it) => (_jsx("li", { children: _jsxs(Link, { to: "/item/$itemId", params: { itemId: String(it.id) }, className: "group block", children: [_jsx(Poster, { item: it, shape: shape, className: "transition-transform group-hover:scale-[1.03] group-hover:ring-2 group-hover:ring-accent" }), _jsx("div", { className: "mt-2 truncate text-sm font-medium", title: it.title, children: it.title }), _jsx("div", { className: "truncate text-xs text-muted", children: subtitleFor(it) })] }) }, it.id))) }), items.data && items.data.items.length < items.data.total && (_jsx("div", { className: "mt-8 flex justify-center", children: _jsx(Button, { onClick: () => setLimit((l) => l + PAGE), loading: items.isFetching, children: "Show more" }) }))] }));
}
