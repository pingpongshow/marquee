import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { AlertTriangle, ChevronRight } from "lucide-react";
import { imageUrl, personPhotoUrl } from "@/api/client";
import { itemChildrenQuery, itemQuery, meQuery } from "@/api/queries";
import { Alert, Spinner } from "@/components/ui";
import { useMusic } from "../player/MusicPlayer";
import { ItemActions } from "./ItemActions";
import { PlayButtons } from "./PlayButtons";
import { Poster } from "./Poster";
import { formatBytes, formatDuration, formatTrackTime, languageName, subtitleFor } from "./format";
function Breadcrumbs({ item }) {
    const crumbs = [];
    if (item.grandparentId)
        crumbs.push({ id: item.grandparentId, title: item.grandparentTitle ?? "" });
    if (item.parentId)
        crumbs.push({ id: item.parentId, title: item.parentTitle ?? "" });
    if (!crumbs.length)
        return null;
    return (_jsx("nav", { "aria-label": "Breadcrumb", className: "mb-2 flex items-center gap-1 text-sm text-muted", children: crumbs.map((c) => (_jsxs("span", { className: "flex items-center gap-1", children: [_jsx(Link, { to: "/item/$itemId", params: { itemId: String(c.id) }, className: "hover:text-text", children: c.title }), _jsx(ChevronRight, { className: "size-4", "aria-hidden": true })] }, c.id))) }));
}
function streamLabel(s) {
    if (s.kind === "video")
        return [s.height ? `${s.height}p` : "", s.codec.toUpperCase(), s.bitDepth === 10 ? "10-bit" : "", s.frameRate ? `${s.frameRate} fps` : ""].filter(Boolean).join(" · ");
    if (s.kind === "audio")
        return [languageName(s.language), s.codec.toUpperCase(), s.channelLayout ?? (s.channels ? `${s.channels} ch` : ""), s.title ?? ""].filter(Boolean).join(" · ");
    return [languageName(s.language), s.codec.toUpperCase(), s.forced ? "Forced" : "", s.hearingImpaired ? "SDH" : "", s.external ? "External" : "", s.title ?? ""].filter(Boolean).join(" · ");
}
function MediaInfo({ item, isAdmin }) {
    if (!item.versions.length)
        return null;
    return (_jsxs("details", { className: "mt-8 rounded-lg border border-border bg-surface", children: [_jsx("summary", { className: "cursor-pointer px-5 py-3 text-sm font-medium", children: "Media info" }), _jsx("div", { className: "space-y-5 border-t border-border px-5 py-4 text-sm", children: item.versions.map((v, vi) => (_jsxs("div", { className: "space-y-3", children: [item.versions.length > 1 && _jsx("div", { className: "font-semibold", children: v.label || `Version ${vi + 1}` }), v.files.map((f) => (_jsxs("div", { className: "space-y-2", children: [isAdmin && f.path && _jsx("div", { className: "font-mono text-xs break-all text-muted", children: f.path }), _jsxs("div", { className: "text-muted", children: [[f.container?.toUpperCase(), f.width && f.height ? `${f.width}×${f.height}` : "", f.bitrateKbps ? `${(f.bitrateKbps / 1000).toFixed(1)} Mbps` : "", formatBytes(f.size), f.hdrFormat?.toUpperCase().replace("_", " ")]
                                            .filter(Boolean)
                                            .join(" · "), !f.available && _jsx("span", { className: "ml-2 text-danger", children: "File unavailable" })] }), _jsx("ul", { className: "space-y-1", children: f.streams.map((s) => (_jsxs("li", { className: "flex gap-3", children: [_jsx("span", { className: "w-20 shrink-0 text-xs tracking-wide text-faint uppercase", children: s.kind }), _jsx("span", { children: streamLabel(s) })] }, s.id))) })] }, f.id)))] }, v.id))) })] }));
}
function RatingBadges({ item }) {
    const r = item.ratings;
    const badges = [];
    if (r?.imdb)
        badges.push({ label: "IMDb", value: r.imdb.toFixed(1), title: r.imdbVotes ? `${r.imdbVotes.toLocaleString()} votes` : "IMDb rating" });
    if (r?.rottenTomatoes != null)
        badges.push({ label: r.rottenTomatoes >= 60 ? "🍅" : "🤢", value: `${r.rottenTomatoes}%`, title: "Rotten Tomatoes critics" });
    if (r?.metacritic != null)
        badges.push({ label: "Metacritic", value: String(r.metacritic), title: "Metacritic" });
    if (item.audienceRating)
        badges.push({ label: "TMDB", value: item.audienceRating.toFixed(1), title: "TMDB user score" });
    if (!badges.length)
        return null;
    return (_jsx("div", { className: "mt-3 flex flex-wrap gap-2", children: badges.map((b) => (_jsxs("span", { title: b.title, className: "inline-flex items-center gap-1.5 rounded-md bg-surface-2/80 px-2 py-1 text-sm", children: [_jsx("span", { className: "text-xs font-semibold text-muted", children: b.label }), _jsx("span", { className: "font-semibold", children: b.value })] }, b.label))) }));
}
function Cast({ credits }) {
    const people = credits.filter((c) => c.role === "actor").slice(0, 20);
    const crew = credits.filter((c) => c.role === "director" || c.role === "creator" || c.role === "writer");
    if (!people.length && !crew.length)
        return null;
    return (_jsxs("section", { className: "mt-10", children: [crew.length > 0 && (_jsx("div", { className: "mb-4 flex flex-wrap gap-x-8 gap-y-1 text-sm", children: ["director", "creator", "writer"].map((role) => {
                    const names = crew.filter((c) => c.role === role).map((c) => c.name);
                    return names.length ? (_jsxs("div", { children: [_jsx("span", { className: "text-faint capitalize", children: role === "writer" ? "Writers" : role + (names.length > 1 ? "s" : "") }), " ", _jsx("span", { className: "text-text", children: names.slice(0, 4).join(", ") })] }, role)) : null;
                }) })), people.length > 0 && (_jsxs(_Fragment, { children: [_jsx("h2", { className: "mb-4 text-lg font-semibold", children: "Cast" }), _jsx("ul", { className: "flex gap-4 overflow-x-auto pb-2", children: people.map((c) => (_jsxs("li", { className: "w-24 shrink-0 text-center", children: [_jsx("div", { className: "mx-auto mb-2 size-24 overflow-hidden rounded-full bg-surface-3", children: c.hasPhoto && _jsx("img", { src: personPhotoUrl(c.personId, 96), alt: "", loading: "lazy", className: "size-full object-cover" }) }), _jsx("div", { className: "truncate text-sm font-medium", title: c.name, children: c.name }), c.character && _jsx("div", { className: "truncate text-xs text-muted", title: c.character, children: c.character })] }, c.personId))) })] }))] }));
}
function Children({ item }) {
    const music = useMusic();
    const children = useQuery({ ...itemChildrenQuery(item.id), enabled: item.childCount > 0 });
    if (!children.data?.items.length)
        return null;
    const list = children.data.items;
    const heading = { show: "Seasons", season: "Episodes", artist: "Albums", album: "Tracks" }[item.type] ?? "Contents";
    // Episodes and tracks are rows; seasons and albums are poster cards.
    const rows = item.type === "season" || item.type === "album";
    return (_jsxs("section", { className: "mt-10", children: [_jsx("h2", { className: "mb-4 text-lg font-semibold", children: heading }), rows ? (_jsx("ol", { className: "divide-y divide-border rounded-lg border border-border bg-surface", children: list.map((c, i) => (_jsx("li", { children: _jsxs(Link, { to: "/item/$itemId", params: { itemId: String(c.id) }, onClick: (e) => {
                            if (c.type === "track") {
                                e.preventDefault(); // tracks play instead of opening a page
                                music.play(list.filter((t) => t.type === "track"), list.filter((t) => t.type === "track").indexOf(c));
                            }
                        }, className: i === music.index && music.queue[music.index]?.id === c.id ? "flex items-center gap-4 px-4 py-3 text-accent hover:bg-surface-2" : "flex items-center gap-4 px-4 py-3 hover:bg-surface-2", children: [_jsx("span", { className: "w-8 shrink-0 text-right text-sm text-faint tabular-nums", children: c.index ?? "" }), c.type === "episode" && c.images?.thumb && _jsx("img", { src: imageUrl(c.images.thumb, 160), alt: "", loading: "lazy", className: "aspect-video w-32 shrink-0 rounded object-cover" }), _jsxs("span", { className: "min-w-0 flex-1", children: [_jsx("span", { className: c.available ? "block truncate" : "block truncate text-faint line-through", children: c.title }), c.type === "track" && c.artistCredit && c.artistCredit !== item.artistCredit && _jsx("span", { className: "block truncate text-xs text-muted", children: c.artistCredit })] }), _jsx("span", { className: "shrink-0 text-sm text-muted tabular-nums", children: c.type === "track" ? formatTrackTime(c.durationMs) : formatDuration(c.durationMs) })] }) }, c.id))) })) : (_jsx("ul", { className: "grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-x-4 gap-y-6", children: list.map((c) => (_jsx("li", { children: _jsxs(Link, { to: "/item/$itemId", params: { itemId: String(c.id) }, className: "group block", children: [_jsx(Poster, { item: c, shape: c.type === "album" ? "square" : "poster", className: "group-hover:ring-2 group-hover:ring-accent" }), _jsx("div", { className: "mt-2 truncate text-sm font-medium", children: c.title }), _jsx("div", { className: "truncate text-xs text-muted", children: subtitleFor(c) })] }) }, c.id))) }))] }));
}
export function ItemPage() {
    const { itemId } = useParams({ from: "/item/$itemId" });
    const item = useQuery(itemQuery(Number(itemId)));
    const me = useQuery(meQuery);
    if (item.isPending)
        return _jsx(Spinner, {});
    if (item.isError)
        return _jsx("div", { className: "p-8", children: _jsx(Alert, { tone: "error", children: item.error.message }) });
    const d = item.data;
    const square = d.type === "album" || d.type === "artist" || d.type === "track";
    const meta = [
        d.type === "episode" && d.parentTitle ? `${d.parentTitle} · Episode ${d.index}` : "",
        d.year ? String(d.year) : "",
        d.contentRating ?? "",
        formatDuration(d.durationMs),
        d.type === "show" || d.type === "artist" ? subtitleFor(d) : "",
    ].filter(Boolean);
    const backdrop = d.images?.backdrop;
    return (_jsxs("div", { className: "relative", children: [backdrop && (_jsxs("div", { className: "pointer-events-none absolute inset-x-0 top-0 h-[28rem] overflow-hidden", "aria-hidden": true, children: [_jsx("img", { src: imageUrl(backdrop, 1280), alt: "", className: "size-full object-cover object-top opacity-35" }), _jsx("div", { className: "absolute inset-0 bg-gradient-to-t from-bg via-bg/60 to-transparent" }), _jsx("div", { className: "absolute inset-0 bg-gradient-to-r from-bg/80 to-transparent" })] })), _jsxs("div", { className: "relative p-6 lg:p-8", children: [_jsxs("div", { className: "flex flex-col gap-8 sm:flex-row", children: [_jsx("div", { className: square ? "w-48 shrink-0" : "w-44 shrink-0 sm:w-56", children: _jsx(Poster, { item: d, shape: d.type === "episode" ? "wide" : square ? "square" : "poster", width: square ? 192 : 224 }) }), _jsxs("div", { className: "min-w-0 flex-1", children: [_jsx(Breadcrumbs, { item: d }), _jsx("h1", { className: "text-3xl font-bold", children: d.title }), d.artistCredit && d.type !== "artist" && _jsx("div", { className: "mt-1 text-lg text-muted", children: d.artistCredit }), _jsx("div", { className: "mt-2 text-sm text-muted", children: meta.join(" · ") }), d.genres.length > 0 && _jsx("div", { className: "mt-1 text-sm text-faint", children: d.genres.join(", ") }), _jsx(RatingBadges, { item: d }), _jsx(PlayButtons, { item: d }), me.data?.isAdmin && (_jsx("div", { className: "mt-4 flex items-center gap-3", children: _jsx(ItemActions, { item: d }) })), d.tagline && _jsx("p", { className: "mt-4 text-lg text-muted italic", children: d.tagline }), !d.available && (_jsxs("div", { className: "mt-4 flex items-center gap-2 text-sm text-danger", children: [_jsx(AlertTriangle, { className: "size-4", "aria-hidden": true }), " The files for this item are currently unavailable."] })), d.summary ? (_jsx("p", { className: "mt-5 max-w-3xl leading-relaxed text-text/90", children: d.summary })) : ((d.matchState === "unmatched" || d.matchState === "failed") &&
                                        (d.type === "movie" || d.type === "show") && (_jsx("p", { className: "mt-5 text-sm text-faint", children: d.matchState === "failed" ? "No metadata match found. Use ⋯ → Fix match to choose the right title." : "Not matched yet. Add a TMDB API key in Settings → Metadata." })))] })] }), _jsx(Children, { item: d }), _jsx(Cast, { credits: d.credits }), _jsx(MediaInfo, { item: d, isAdmin: !!me.data?.isAdmin })] })] }));
}
