import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Search } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { imageUrl } from "@/api/client";
import { searchQuery } from "@/api/queries";
export const groupLabels = { movie: "Movies", show: "Shows", episode: "Episodes", artist: "Artists", album: "Albums", track: "Tracks", video: "Videos" };
export function resultSubtitle(it) {
    switch (it.type) {
        case "episode":
            return `${it.grandparentTitle ?? ""} · S${it.parentTitle?.replace(/\D/g, "") || "?"} E${it.index ?? "?"}`;
        case "album":
            return it.artistCredit ?? it.parentTitle ?? "";
        case "track":
            return [it.artistCredit, it.parentTitle].filter(Boolean).join(" · ");
        default:
            return it.year ? String(it.year) : "";
    }
}
function useDebounced(value, ms) {
    const [v, setV] = useState(value);
    useEffect(() => {
        const t = setTimeout(() => setV(value), ms);
        return () => clearTimeout(t);
    }, [value, ms]);
    return v;
}
/** Header search with instant results grouped by type; Enter opens the full results page. */
export function SearchBox() {
    const navigate = useNavigate();
    const [q, setQ] = useState("");
    const [open, setOpen] = useState(false);
    const debounced = useDebounced(q.trim(), 200);
    const results = useQuery({ ...searchQuery(debounced, 4), placeholderData: (prev) => prev });
    const ref = useRef(null);
    useEffect(() => {
        const onDown = (e) => !ref.current?.contains(e.target) && setOpen(false);
        document.addEventListener("mousedown", onDown);
        return () => document.removeEventListener("mousedown", onDown);
    }, []);
    const go = (it) => {
        setOpen(false);
        setQ("");
        navigate({ to: "/item/$itemId", params: { itemId: String(it.id) } });
    };
    const groups = debounced ? (results.data?.groups ?? []) : [];
    return (_jsxs("div", { className: "relative mx-auto hidden w-full max-w-md md:block", ref: ref, children: [_jsx(Search, { className: "pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-faint", "aria-hidden": true }), _jsx("input", { value: q, onChange: (e) => {
                    setQ(e.target.value);
                    setOpen(true);
                }, onFocus: () => setOpen(true), onKeyDown: (e) => {
                    if (e.key === "Enter" && q.trim()) {
                        setOpen(false);
                        navigate({ to: "/search", search: { q: q.trim() } });
                    }
                    if (e.key === "Escape")
                        setOpen(false);
                }, placeholder: "Search", "aria-label": "Search", className: "h-9 w-full rounded-full border border-border bg-surface-2 pr-4 pl-9 text-sm placeholder:text-faint focus:border-accent focus:outline-none" }), open && debounced && (_jsxs("div", { className: "absolute inset-x-0 z-40 mt-2 max-h-[70vh] overflow-y-auto rounded-lg border border-border bg-surface shadow-2xl", children: [groups.length === 0 && !results.isFetching && _jsxs("p", { className: "px-4 py-6 text-center text-sm text-muted", children: ["No results for \u201C", debounced, "\u201D."] }), groups.map((g) => (_jsxs("div", { className: "py-1", children: [_jsx("div", { className: "px-4 py-1 text-[11px] font-semibold tracking-wider text-faint uppercase", children: groupLabels[g.type] }), g.items.map((it) => (_jsxs("button", { onClick: () => go(it), className: "flex w-full items-center gap-3 px-4 py-1.5 text-left hover:bg-surface-2", children: [_jsx("span", { className: "h-12 w-8 shrink-0 overflow-hidden rounded bg-surface-3", children: it.images?.poster && _jsx("img", { src: imageUrl(it.images.poster, 40), alt: "", className: "size-full object-cover" }) }), _jsxs("span", { className: "min-w-0", children: [_jsx("span", { className: "block truncate text-sm", children: it.title }), _jsx("span", { className: "block truncate text-xs text-muted", children: resultSubtitle(it) })] })] }, it.id)))] }, g.type)))] }))] }));
}
