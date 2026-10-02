import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, CheckCircle2, Download, RefreshCw } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { usersQuery } from "@/api/queries";
import { Alert, Button, Card, Input, Select, Spinner, Toggle } from "@/components/ui";
const statusQuery = {
    queryKey: ["plex-import"],
    queryFn: () => unwrap(api.GET("/plex-import")),
};
function pct(a, b) {
    return b ? `${Math.round((a / b) * 100)}%` : "–";
}
function ReportView({ r }) {
    const rows = [
        ["Files paired", `${r.matchedFiles.toLocaleString()} of ${r.files.toLocaleString()}`],
        ["Users", `${r.usersLinked} linked, ${r.usersCreated} created`],
        ["Watched / resume points", r.watchStateItems.toLocaleString()],
        ["Ratings", r.ratings.toLocaleString()],
        ["Play history", `${r.historyImported.toLocaleString()} imported${r.historySkipped ? `, ${r.historySkipped} already present` : ""}`],
        ["Matches", `${r.matchesAgreed.toLocaleString()} agreed, ${r.matchesApplied} taken from Plex, ${r.matchesKept.length} kept Marquee's`],
        ["Chosen artwork", `${r.artwork} copied${r.artworkMissing ? `, ${r.artworkMissing} not found` : ""}`],
        ["Intro & credits markers", r.markers.toLocaleString()],
        ["Playlists", `${r.playlists} (${r.playlistItems.toLocaleString()} items${r.playlistItemsMissing ? `, ${r.playlistItemsMissing} not in your library` : ""})`],
    ];
    return (_jsxs("div", { className: "space-y-4 text-sm", children: [_jsx("dl", { className: "grid grid-cols-[auto_1fr] gap-x-6 gap-y-1.5", children: rows.map(([k, v]) => (_jsxs("div", { className: "contents", children: [_jsx("dt", { className: "text-muted", children: k }), _jsx("dd", { children: v })] }, k))) }), r.usersCreated > 0 && _jsx(Alert, { tone: "info", children: "New users sign in by choosing their profile on the home network. Add a PIN or password for them in Settings \u2192 Users if you like." }), [
                ["Kept Marquee's match (review with ⋯ → Fix match if Plex was right)", r.matchesKept],
                ["Not applied", r.matchFailures],
                ["Plex files not found in Marquee", r.unmatchedSample],
                ["Warnings", r.warnings],
            ].map(([title, list]) => list.length ? (_jsxs("details", { className: "rounded-md bg-surface-2 px-3 py-2", children: [_jsxs("summary", { className: "cursor-pointer", children: [title, " (", list.length, ")"] }), _jsx("ul", { className: "mt-2 max-h-60 space-y-0.5 overflow-y-auto font-mono text-xs text-muted", children: list.map((x) => (_jsx("li", { children: x }, x))) })] }, title)) : null), _jsxs("p", { className: "text-xs text-faint", children: ["Finished ", new Date(r.finishedAt).toLocaleString(), ". Running the import again updates everything without duplicating it."] })] }));
}
export function PlexImportSettings() {
    const qc = useQueryClient();
    const status = useQuery({ ...statusQuery, refetchInterval: (q) => (q.state.data?.running ? 1500 : false) });
    const users = useQuery(usersQuery);
    const [preview, setPreview] = useState(null);
    const [mappings, setMappings] = useState([]);
    const [choices, setChoices] = useState({});
    const [include, setInclude] = useState({ matches: true, watchState: true, history: true, playlists: true, markers: true, artwork: true });
    const check = useMutation({
        mutationFn: (m) => unwrap(api.POST("/plex-import/preview", { body: m ? { pathMappings: m } : {} })),
        onSuccess: (pv) => {
            setPreview(pv);
            setMappings(pv.pathMappings);
            setChoices((prev) => Object.fromEntries(pv.accounts.map((a) => [
                a.id,
                prev[a.id] ?? { plexId: a.id, action: a.suggestedAction, userId: a.suggestedUserId, username: a.suggestedUsername ?? a.name },
            ])));
        },
    });
    const start = useMutation({
        mutationFn: () => unwrap(api.POST("/plex-import", { body: { pathMappings: mappings, accounts: Object.values(choices), include } })),
        onSuccess: (st) => qc.setQueryData(statusQuery.queryKey, st),
    });
    if (status.isPending)
        return _jsx(Spinner, {});
    const st = status.data;
    if (!st?.available)
        return (_jsx(Card, { title: "Plex database not found", children: _jsxs("p", { className: "text-sm text-muted", children: ["Mount your Plex data folder (the one containing ", _jsx("code", { className: "text-text", children: "Library" }), ") read-only at ", _jsx("code", { className: "text-text", children: "/plex" }), " in the Marquee container, then reload this page."] }) }));
    if (st.running)
        return (_jsx(Card, { title: "Importing from Plex", children: _jsxs("div", { className: "space-y-2", children: [_jsxs("div", { className: "text-sm", children: [st.progress?.step, "\u2026"] }), _jsx("div", { className: "h-1.5 overflow-hidden rounded-full bg-surface-3", children: _jsx("div", { className: st.progress?.total ? "h-full bg-accent transition-all" : "h-full w-1/3 animate-pulse bg-accent", style: st.progress?.total ? { width: pct(st.progress.done, st.progress.total) } : undefined }) }), _jsx("p", { className: "text-xs text-faint", children: "You can leave this page; the import continues in the background." })] }) }));
    return (_jsxs("div", { className: "space-y-6", children: [st.lastError && _jsxs(Alert, { tone: "error", children: ["The last import failed: ", st.lastError] }), st.lastReport && (_jsx(Card, { title: "Last import", description: `From ${st.databasePath}`, children: _jsx(ReportView, { r: st.lastReport }) })), _jsxs(Card, { title: st.lastReport ? "Import again" : "Import from Plex", description: "Brings over watch status, resume points, ratings, play history, playlists, intro/credits markers, the posters you chose and Plex's matches. Plex itself is not changed.", actions: _jsxs(Button, { size: "sm", variant: preview ? "ghost" : "primary", onClick: () => check.mutate(undefined), loading: check.isPending, children: [_jsx(RefreshCw, { className: "size-4" }), " ", preview ? "Re-check" : "Check Plex library"] }), children: [check.isError && _jsx(Alert, { tone: "error", children: check.error.message }), check.isPending && _jsx(Spinner, { label: "Reading a copy of the Plex database" }), !preview && !check.isPending && _jsx("p", { className: "text-sm text-muted", children: "Marquee reads a copy of the Plex database and shows what it found before anything is imported." }), preview && !check.isPending && (_jsxs("div", { className: "space-y-8", children: [_jsxs("section", { children: [_jsx("h3", { className: "mb-2 text-sm font-semibold", children: "1. Folders" }), _jsxs("p", { className: "mb-3 text-sm text-muted", children: [preview.matchedFiles.toLocaleString(), " of ", preview.files.toLocaleString(), " Plex files (", pct(preview.matchedFiles, preview.files), ") were found in Marquee."] }), _jsx("div", { className: "space-y-2", children: mappings.map((m, i) => {
                                            const sec = preview.sections.find((s) => s.roots.includes(m.plexPath));
                                            return (_jsxs("div", { className: "grid grid-cols-[1fr_auto_1fr_5rem] items-center gap-2", children: [_jsx(Input, { "aria-label": "Plex folder", value: m.plexPath, onChange: (e) => setMappings(mappings.map((x, j) => (j === i ? { ...x, plexPath: e.target.value } : x))) }), _jsx(ArrowRight, { className: "size-4 text-faint", "aria-hidden": true }), _jsx(Input, { "aria-label": "Marquee folder", value: m.marqueePath, onChange: (e) => setMappings(mappings.map((x, j) => (j === i ? { ...x, marqueePath: e.target.value } : x))) }), _jsx("span", { className: "text-right text-xs text-muted", title: sec ? `${sec.name}: ${sec.matchedFiles} of ${sec.files} files` : undefined, children: sec ? pct(sec.matchedFiles, sec.files) : "" })] }, i));
                                        }) }), _jsx(Button, { size: "sm", variant: "ghost", className: "mt-2", onClick: () => check.mutate(mappings), children: "Re-check with these folders" })] }), _jsxs("section", { children: [_jsx("h3", { className: "mb-2 text-sm font-semibold", children: "2. People" }), _jsx("ul", { className: "divide-y divide-border rounded-md border border-border", children: preview.accounts.map((a) => {
                                            const c = choices[a.id];
                                            if (!c)
                                                return null;
                                            const set = (patch) => setChoices({ ...choices, [a.id]: { ...c, ...patch } });
                                            return (_jsxs("li", { className: "flex flex-wrap items-center gap-3 px-3 py-2.5", children: [_jsxs("div", { className: "min-w-40 flex-1", children: [_jsxs("div", { className: "text-sm font-medium", children: [a.name, " ", a.isOwner && _jsx("span", { className: "text-xs text-accent", children: "(Plex owner)" })] }), _jsxs("div", { className: "text-xs text-muted", children: [a.watched, " watched \u00B7 ", a.inProgress, " in progress \u00B7 ", a.history, " plays", a.playlists ? ` · ${a.playlists} playlists` : ""] })] }), _jsxs(Select, { "aria-label": `Action for ${a.name}`, className: "h-8 w-36", value: c.action, onChange: (e) => set({ action: e.target.value }), children: [_jsx("option", { value: "link", children: "Link to user" }), _jsx("option", { value: "create", children: "Create user" }), _jsx("option", { value: "merge", children: "Merge into\u2026" }), _jsx("option", { value: "skip", children: "Skip" })] }), c.action === "link" && (_jsxs(Select, { "aria-label": "Marquee user", className: "h-8 w-44", value: c.userId ?? "", onChange: (e) => set({ userId: Number(e.target.value) || undefined }), children: [_jsx("option", { value: "", children: "Choose\u2026" }), users.data?.map((u) => (_jsx("option", { value: u.id, children: u.displayName }, u.id)))] })), c.action === "create" && _jsx(Input, { "aria-label": "New username", className: "h-8 w-44", value: c.username ?? "", onChange: (e) => set({ username: e.target.value }) }), c.action === "merge" && (_jsxs(Select, { "aria-label": "Merge into account", className: "h-8 w-44", value: c.mergeInto ?? "", onChange: (e) => set({ mergeInto: Number(e.target.value) || undefined }), children: [_jsx("option", { value: "", children: "Choose account\u2026" }), preview.accounts
                                                                .filter((o) => o.id !== a.id && (choices[o.id]?.action === "create" || choices[o.id]?.action === "link"))
                                                                .map((o) => (_jsx("option", { value: o.id, children: o.name }, o.id)))] }))] }, a.id));
                                        }) })] }), _jsxs("section", { children: [_jsx("h3", { className: "mb-2 text-sm font-semibold", children: "3. What to import" }), _jsx("div", { className: "grid gap-3 sm:grid-cols-2", children: [
                                            ["watchState", "Watched status, resume points and ratings"],
                                            ["history", "Play history"],
                                            ["playlists", "Playlists"],
                                            ["markers", "Intro and credits markers"],
                                            ["artwork", "Posters you chose in Plex"],
                                            ["matches", "Plex's matches where they fit better"],
                                        ].map(([k, label]) => (_jsx(Toggle, { label: label, checked: include[k], onChange: (v) => setInclude({ ...include, [k]: v }) }, k))) })] }), start.isError && _jsx(Alert, { tone: "error", children: start.error.message }), _jsxs("div", { className: "flex items-center justify-end gap-3", children: [st.lastReport && (_jsxs("span", { className: "flex items-center gap-1 text-xs text-muted", children: [_jsx(CheckCircle2, { className: "size-4 text-success" }), " Safe to repeat: nothing is duplicated"] })), _jsxs(Button, { variant: "primary", onClick: () => start.mutate(), loading: start.isPending, disabled: !mappings.length, children: [_jsx(Download, { className: "size-4" }), " Start import"] })] })] }))] })] }));
}
