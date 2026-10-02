import { jsx as _jsx, Fragment as _Fragment, jsxs as _jsxs } from "react/jsx-runtime";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronRight, FolderPlus } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { librariesQuery, meQuery } from "@/api/queries";
import { Button, Spinner } from "@/components/ui";
import { Poster } from "../browse/Poster";
import { subtitleFor } from "../browse/format";
function HubItem({ it, wide }) {
    // Continue Watching episodes open the player directly, like Plex.
    const playable = (it.type === "episode" || it.type === "movie" || it.type === "video") && wide;
    const title = it.type === "episode" ? it.grandparentTitle : it.title;
    const sub = it.type === "episode" ? `${it.parentTitle ?? ""} · E${it.index ?? ""}${it.title ? ` · ${it.title}` : ""}` : subtitleFor(it);
    const body = (_jsxs(_Fragment, { children: [_jsx(Poster, { item: it, shape: wide ? "wide" : it.type === "album" || it.type === "artist" ? "square" : "poster", width: wide ? 320 : 180, className: "group-hover:ring-2 group-hover:ring-accent" }), _jsx("div", { className: "mt-2 truncate text-sm font-medium", children: title }), _jsx("div", { className: "truncate text-xs text-muted", children: sub })] }));
    return (_jsx("li", { className: wide ? "w-72 shrink-0" : "w-36 shrink-0", children: playable ? (_jsx(Link, { to: "/play/$itemId", params: { itemId: String(it.id) }, search: { t: undefined }, className: "group block", children: body })) : (_jsx(Link, { to: "/item/$itemId", params: { itemId: String(it.id) }, className: "group block", children: body })) }));
}
export function HomePage() {
    const libraries = useQuery(librariesQuery);
    const me = useQuery(meQuery);
    const hubs = useQuery({ queryKey: ["items", "hubs"], queryFn: () => unwrap(api.GET("/hubs/home")) });
    if (libraries.isPending || hubs.isPending)
        return _jsx(Spinner, {});
    if (!libraries.data?.length) {
        return (_jsxs("div", { className: "flex h-full flex-col items-center justify-center gap-4 p-8 text-center", children: [_jsx(FolderPlus, { className: "size-14 text-faint", "aria-hidden": true }), _jsx("h1", { className: "text-xl font-semibold", children: "Your server has no libraries yet" }), _jsx("p", { className: "max-w-md text-muted", children: "Add your movie, TV, anime and music folders and Marquee will organise them here." }), me.data?.isAdmin && (_jsx(Link, { to: "/settings/$section", params: { section: "libraries" }, children: _jsx(Button, { variant: "primary", children: "Add a library" }) }))] }));
    }
    return (_jsxs("div", { className: "space-y-10 py-6 lg:py-8", children: [hubs.data?.map((hub) => {
                const wide = hub.id === "continue-watching";
                return (_jsxs("section", { children: [_jsxs("div", { className: "mb-3 flex items-center gap-2 px-6 lg:px-8", children: [_jsx("h2", { className: "text-lg font-semibold", children: hub.title }), hub.libraryId && (_jsx(Link, { to: "/library/$libraryId", params: { libraryId: String(hub.libraryId) }, className: "text-muted hover:text-text", "aria-label": `Open ${hub.title}`, children: _jsx(ChevronRight, { className: "size-5" }) }))] }), _jsx("ul", { className: "flex gap-4 overflow-x-auto px-6 pb-2 lg:px-8", children: hub.items.map((it) => (_jsx(HubItem, { it: it, wide: wide }, it.id))) })] }, hub.id));
            }), hubs.data?.length === 0 && _jsx("p", { className: "px-8 text-muted", children: "Nothing here yet. Libraries are still being scanned." })] }));
}
