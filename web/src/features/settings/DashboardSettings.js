import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { clsx } from "clsx";
import { Cpu, Globe, Home, Square, Tv } from "lucide-react";
import { api, imageUrl, unwrap } from "@/api/client";
import { Card, Spinner } from "@/components/ui";
const methodLabel = { direct_play: "Direct Play", direct_stream: "Direct Stream", transcode: "Transcode" };
const encoderLabel = { nvenc: "NVIDIA NVENC", qsv: "Intel Quick Sync", software: "CPU", copy: "Copy" };
function mbps(kbps) {
    return `${(kbps / 1000).toFixed(1)} Mbps`;
}
function time(ms) {
    const s = Math.floor(ms / 1000);
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    return h ? `${h}:${String(m).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}` : `${m}:${String(s % 60).padStart(2, "0")}`;
}
function Stat({ label, value, hint }) {
    return (_jsxs("div", { className: "rounded-lg border border-border bg-surface p-4", children: [_jsx("div", { className: "text-xs tracking-wider text-faint uppercase", children: label }), _jsx("div", { className: "mt-1 text-2xl font-semibold tabular-nums", children: value }), hint && _jsx("div", { className: "mt-0.5 text-xs text-muted", children: hint })] }));
}
function SessionCard({ s }) {
    const qc = useQueryClient();
    const stop = useMutation({
        mutationFn: () => unwrap(api.DELETE("/playback/sessions/{sessionId}", { params: { path: { sessionId: s.id } } })),
        onSuccess: () => qc.invalidateQueries({ queryKey: ["dashboard"] }),
    });
    const pct = s.durationMs ? Math.min(100, (s.positionMs / s.durationMs) * 100) : 0;
    return (_jsxs("li", { className: "flex gap-4 rounded-lg border border-border bg-surface p-3", children: [_jsx("div", { className: "aspect-video w-40 shrink-0 overflow-hidden rounded bg-surface-3", children: s.imageId && _jsx("img", { src: imageUrl(s.imageId, 160), alt: "", className: "size-full object-cover" }) }), _jsxs("div", { className: "min-w-0 flex-1 space-y-1", children: [_jsxs("div", { className: "flex items-start gap-2", children: [_jsxs("div", { className: "min-w-0 flex-1", children: [_jsx("div", { className: "truncate font-medium", children: s.title }), s.subtitle && _jsx("div", { className: "truncate text-xs text-muted", children: s.subtitle })] }), _jsx("button", { onClick: () => stop.mutate(), className: "rounded p-1.5 text-muted hover:bg-surface-2 hover:text-danger", title: "Stop this stream", "aria-label": `Stop ${s.title}`, children: _jsx(Square, { className: "size-4" }) })] }), _jsxs("div", { className: "flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted", children: [_jsx("span", { className: "font-medium text-text", children: s.userName }), _jsx("span", { children: s.deviceName }), _jsxs("span", { className: "flex items-center gap-1", children: [s.networkClass === "remote" ? _jsx(Globe, { className: "size-3" }) : _jsx(Home, { className: "size-3" }), s.networkClass === "remote" ? "Remote" : "Local", s.clientIp ? ` · ${s.clientIp}` : ""] })] }), _jsxs("div", { className: "flex flex-wrap items-center gap-2 text-xs", children: [_jsx("span", { className: clsx("rounded px-1.5 py-0.5 font-medium", s.method === "transcode" ? "bg-accent/15 text-accent" : "bg-success/15 text-success"), title: s.reasons?.join("\n"), children: methodLabel[s.method] }), _jsx("span", { className: "text-muted", children: s.summary }), s.encoder && (_jsxs("span", { className: "flex items-center gap-1 text-muted", children: [_jsx(Cpu, { className: "size-3" }), " ", encoderLabel[s.encoder] ?? s.encoder] })), _jsx("span", { className: "text-muted", children: mbps(s.bitrateKbps) }), s.state === "paused" && _jsx("span", { className: "text-faint", children: "Paused" })] }), s.reasons && s.reasons.length > 0 && _jsx("div", { className: "truncate text-[11px] text-faint", children: s.reasons.join(" · ") }), _jsxs("div", { className: "flex items-center gap-2 pt-1", children: [_jsx("div", { className: "h-1 flex-1 overflow-hidden rounded-full bg-surface-3", children: _jsx("div", { className: "h-full bg-accent", style: { width: `${pct}%` } }) }), _jsxs("span", { className: "text-[11px] text-muted tabular-nums", children: [time(s.positionMs), " / ", time(s.durationMs)] })] })] })] }));
}
export function DashboardSettings() {
    const sessions = useQuery({ queryKey: ["dashboard", "sessions"], queryFn: () => unwrap(api.GET("/playback/sessions")), refetchInterval: 3000 });
    const status = useQuery({ queryKey: ["dashboard", "status"], queryFn: () => unwrap(api.GET("/system/status")), refetchInterval: 5000 });
    const history = useQuery({ queryKey: ["dashboard", "history"], queryFn: () => unwrap(api.GET("/activity/history", { params: { query: { limit: 40 } } })), refetchInterval: 30000 });
    const st = status.data;
    return (_jsxs("div", { className: "space-y-6", children: [st && (_jsxs("div", { className: "grid grid-cols-2 gap-3 sm:grid-cols-4", children: [_jsx(Stat, { label: "Streams", value: String(st.activeStreams), hint: st.activeStreams ? undefined : "Nothing playing" }), _jsx(Stat, { label: "Transcodes", value: `${st.activeTranscodes} / ${st.maxTranscodes}`, hint: st.encoders.map((e) => encoderLabel[e] ?? e).join(" → ") }), _jsx(Stat, { label: "Local", value: mbps(st.localKbps) }), _jsx(Stat, { label: "Remote", value: mbps(st.remoteKbps), hint: st.uploadSpeedKbps ? `of ${mbps(st.uploadSpeedKbps)} upload` : "Set upload speed in Remote Access" })] })), _jsxs(Card, { title: "Now playing", children: [sessions.isPending && _jsx(Spinner, {}), sessions.data?.length === 0 && (_jsxs("p", { className: "flex items-center gap-2 text-sm text-muted", children: [_jsx(Tv, { className: "size-4" }), " Nobody is watching right now."] })), _jsx("ul", { className: "space-y-3", children: sessions.data?.map((s) => (_jsx(SessionCard, { s: s }, s.id))) })] }), _jsxs(Card, { title: "Recent plays", children: [history.isPending && _jsx(Spinner, {}), _jsx("ul", { className: "-my-2 divide-y divide-border text-sm", children: history.data?.map((h) => (_jsxs("li", { className: "flex items-center gap-3 py-2", children: [_jsx("span", { className: "w-32 shrink-0 text-xs text-faint tabular-nums", children: new Date(h.startedAt).toLocaleString([], { dateStyle: "short", timeStyle: "short" }) }), _jsx("span", { className: "w-28 shrink-0 truncate text-muted", children: h.userName }), _jsx("span", { className: "min-w-0 flex-1 truncate", children: h.title }), _jsx("span", { className: "hidden shrink-0 text-xs text-faint sm:inline", children: h.source === "plex" ? "Plex" : [h.method && methodLabel[h.method], h.networkClass === "remote" ? "Remote" : h.networkClass ? "Local" : ""].filter(Boolean).join(" · ") })] }, h.id))) }), st && (_jsxs("div", { className: "flex flex-wrap gap-x-6 gap-y-1 border-t border-border pt-4 text-xs text-muted", children: [_jsxs("span", { children: ["Marquee ", st.version] }), _jsxs("span", { children: ["Up since ", new Date(st.startedAt).toLocaleString()] }), st.libraries.map((l) => (_jsxs("span", { children: [l.name, ": ", l.items.toLocaleString()] }, l.id)))] }))] })] }));
}
