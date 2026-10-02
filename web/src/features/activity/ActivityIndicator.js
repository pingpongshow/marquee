import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { clsx } from "clsx";
import { Activity, Square } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { api, unwrap } from "@/api/client";
import { Link } from "@tanstack/react-router";
import { useCancelScan } from "@/api/queries";
function taskDetail(t) {
    const p = t.progress;
    if (t.state === "queued" || !p)
        return "Waiting…";
    if (t.kind === "stream") {
        const fmt = (s) => `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
        return `${fmt(p.done)} / ${fmt(p.total)}`;
    }
    if (p.phase === "walking")
        return "Finding files…";
    if (p.phase === "cleanup")
        return "Finishing up…";
    return `${p.done.toLocaleString()} of ${p.total.toLocaleString()}`;
}
function pct(t) {
    const p = t.progress;
    return p && p.total > 0 && (p.phase === "probing" || p.phase === "metadata") ? Math.round((p.done / p.total) * 100) : null;
}
/**
 * Header activity indicator (like Plex's): pulses while the server is busy and opens a panel
 * listing scans and metadata work. Streams join this list once playback lands (M3).
 */
export function ActivityIndicator() {
    const qc = useQueryClient();
    const cancel = useCancelScan();
    const [open, setOpen] = useState(false);
    const ref = useRef(null);
    const activity = useQuery({
        queryKey: ["activity"],
        queryFn: () => unwrap(api.GET("/activity")),
        refetchInterval: (q) => (q.state.data?.tasks.length ? 2000 : 10000),
        refetchIntervalInBackground: false,
    });
    const tasks = activity.data?.tasks ?? [];
    const busy = tasks.length > 0;
    // When work finishes, refresh library counts and lists.
    const prevBusy = useRef(false);
    useEffect(() => {
        if (prevBusy.current && !busy) {
            qc.invalidateQueries({ queryKey: ["libraries"] });
            qc.invalidateQueries({ queryKey: ["items"] });
        }
        prevBusy.current = busy;
    }, [busy, qc]);
    useEffect(() => {
        if (!open)
            return;
        const onDown = (e) => !ref.current?.contains(e.target) && setOpen(false);
        const onKey = (e) => e.key === "Escape" && setOpen(false);
        document.addEventListener("mousedown", onDown);
        document.addEventListener("keydown", onKey);
        return () => {
            document.removeEventListener("mousedown", onDown);
            document.removeEventListener("keydown", onKey);
        };
    }, [open]);
    return (_jsxs("div", { className: "relative", ref: ref, children: [_jsxs("button", { onClick: () => setOpen((v) => !v), className: clsx("relative rounded p-1.5 hover:bg-surface-2", busy ? "text-accent" : "text-muted hover:text-text"), "aria-label": busy ? `Activity: ${tasks.length} running` : "Activity", "aria-expanded": open, title: "Activity", children: [_jsx(Activity, { className: clsx("size-5", busy && "animate-pulse") }), busy && (_jsx("span", { className: "absolute -top-0.5 -right-0.5 flex size-4 items-center justify-center rounded-full bg-accent text-[10px] font-bold text-black", children: tasks.length }))] }), open && (_jsxs("div", { className: "absolute right-0 z-40 mt-2 w-80 overflow-hidden rounded-lg border border-border bg-surface shadow-2xl", children: [_jsx("div", { className: "border-b border-border px-4 py-2.5 text-sm font-semibold", children: "Activity" }), !busy && _jsx("p", { className: "px-4 py-6 text-center text-sm text-muted", children: "Nothing is running." }), _jsx("ul", { className: "max-h-96 divide-y divide-border overflow-y-auto", children: tasks.map((t) => {
                            const percent = pct(t);
                            return (_jsxs("li", { className: "px-4 py-3", children: [_jsxs("div", { className: "flex items-start gap-2", children: [_jsxs("div", { className: "min-w-0 flex-1", children: [_jsx("div", { className: "truncate text-sm font-medium", children: t.title }), _jsx("div", { className: "text-xs text-muted", children: taskDetail(t) })] }), (t.libraryId || t.kind === "stream") && (_jsx("button", { className: "rounded p-1 text-muted hover:bg-surface-2 hover:text-text", "aria-label": `Stop: ${t.title}`, title: "Stop", onClick: () => {
                                                    if (t.kind === "stream")
                                                        void api.DELETE("/playback/sessions/{sessionId}", { params: { path: { sessionId: t.id.replace("stream:", "") } } });
                                                    else if (t.libraryId)
                                                        cancel.mutate(t.libraryId);
                                                }, children: _jsx(Square, { className: "size-3.5" }) }))] }), t.state === "running" && (_jsx("div", { className: "mt-2 h-1 overflow-hidden rounded-full bg-surface-3", children: _jsx("div", { className: clsx("h-full bg-accent", percent === null && "w-1/3 animate-pulse"), style: percent === null ? undefined : { width: `${percent}%` } }) })), t.progress?.current && _jsx("div", { className: "mt-1 truncate text-[11px] text-faint", children: t.progress.current })] }, t.id));
                        }) }), _jsx(Link, { to: "/settings/$section", params: { section: "dashboard" }, onClick: () => setOpen(false), className: "block border-t border-border px-4 py-2 text-xs text-muted hover:text-text", children: "Open dashboard" })] }))] }));
}
