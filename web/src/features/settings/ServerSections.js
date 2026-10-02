import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { systemInfoQuery } from "@/api/queries";
import { Alert, Button, Card, Field, Input, Select, Spinner, StringListEditor, Toggle } from "@/components/ui";
import { SaveBar } from "./SaveBar";
import { useSectionDraft } from "./useSectionDraft";
export const languages = [
    ["en-US", "English (US)"],
    ["en-GB", "English (UK)"],
    ["ja-JP", "Japanese"],
    ["es-ES", "Spanish"],
    ["fr-FR", "French"],
    ["de-DE", "German"],
    ["it-IT", "Italian"],
    ["pt-BR", "Portuguese (Brazil)"],
    ["ko-KR", "Korean"],
    ["zh-CN", "Chinese (Simplified)"],
];
/** Number input that edits a kbps value in Mbps. Empty or 0 means "not set / unlimited". */
export function MbpsInput({ id, kbps, onChange, placeholder }) {
    return (_jsxs("div", { className: "relative", children: [_jsx(Input, { id: id, type: "number", min: 0, step: 0.1, inputMode: "decimal", placeholder: placeholder, value: kbps ? kbps / 1000 : "", onChange: (e) => onChange(Math.round((parseFloat(e.target.value) || 0) * 1000)), className: "pr-14" }), _jsx("span", { className: "pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 text-sm text-faint", children: "Mbps" })] }));
}
export function GeneralSettings() {
    const s = useSectionDraft("general");
    if (!s.draft)
        return _jsx(Spinner, {});
    return (_jsxs(_Fragment, { children: [_jsxs(Card, { children: [_jsx(Field, { label: "Server name", help: "Shown in every Marquee app.", children: (id) => _jsx(Input, { id: id, maxLength: 64, value: s.draft.serverName ?? "", onChange: (e) => s.update({ serverName: e.target.value }) }) }), _jsx(Field, { label: "Default metadata language", help: "Titles, summaries and artwork are fetched in this language. Libraries can override it.", children: (id) => (_jsx(Select, { id: id, value: s.draft.metadataLanguage, onChange: (e) => s.update({ metadataLanguage: e.target.value }), children: languages.map(([v, l]) => (_jsx("option", { value: v, children: l }, v))) })) })] }), _jsx(SaveBar, { dirty: s.dirty, saving: s.saving, error: s.error, savedAt: s.savedAt, onSave: s.save, onReset: s.reset })] }));
}
export function NetworkSettings() {
    const s = useSectionDraft("network");
    const info = useQuery(systemInfoQuery);
    if (!s.draft)
        return _jsx(Spinner, {});
    return (_jsxs(_Fragment, { children: [_jsxs("div", { className: "space-y-6", children: [info.data && (_jsxs(Alert, { tone: "info", children: ["This browser is connected as ", _jsx("strong", { className: "text-text", children: info.data.networkClass === "local" ? "Local" : "Remote" }), "."] })), _jsxs(Card, { title: "Local network", description: "Devices on these networks connect directly and always get full quality. Everything else, including Tailscale, is treated as remote.", children: [_jsx(Field, { label: "LAN subnets", help: "CIDR notation, e.g. 10.1.1.0/24. Tailscale addresses (100.64.0.0/10) are always remote.", children: () => _jsx(StringListEditor, { values: s.draft.lanSubnets ?? [], onChange: (v) => s.update({ lanSubnets: v }), placeholder: "10.1.1.0/24", addLabel: "Add subnet" }) }), _jsx(Field, { label: "Local server address", help: "The address local apps use, e.g. http://10.1.1.10:32500.", children: (id) => _jsx(Input, { id: id, placeholder: "http://10.1.1.10:32500", value: s.draft.lanUrl ?? "", onChange: (e) => s.update({ lanUrl: e.target.value }) }) }), _jsx(Toggle, { label: "Advertise on the local network (Bonjour)", help: "Lets iPhone, iPad and Apple TV apps find this server automatically.", checked: !!s.draft.bonjourEnabled, onChange: (v) => s.update({ bonjourEnabled: v }) })] })] }), _jsx(SaveBar, { dirty: s.dirty, saving: s.saving, error: s.error, savedAt: s.savedAt, onSave: s.save, onReset: s.reset })] }));
}
export function RemoteAccessSettings() {
    const s = useSectionDraft("remoteAccess");
    if (!s.draft)
        return _jsx(Spinner, {});
    const d = s.draft;
    return (_jsxs(_Fragment, { children: [_jsxs("div", { className: "space-y-6", children: [_jsxs(Card, { title: "Tailscale", description: "Remote devices reach this server through your tailnet. Local devices never use Tailscale.", children: [_jsx(Toggle, { label: "Allow remote streaming", checked: !!d.enabled, onChange: (v) => s.update({ enabled: v }) }), _jsx(Field, { label: "Remote server address", help: _jsxs(_Fragment, { children: ["The HTTPS address from ", _jsx("code", { className: "text-text", children: "tailscale serve" }), ", e.g. https://tower.your-tailnet.ts.net."] }), children: (id) => _jsx(Input, { id: id, placeholder: "https://tower.your-tailnet.ts.net", value: d.remoteUrl ?? "", onChange: (e) => s.update({ remoteUrl: e.target.value }) }) })] }), _jsxs(Card, { title: "Bandwidth", description: "Remote quality is chosen automatically: the lowest of the app\u2019s quality setting, the user\u2019s limit, the measured connection speed, and this server\u2019s fair share of upload bandwidth.", children: [_jsx(Field, { label: "Internet upload speed", help: "Your server\u2019s upload speed. Shared fairly between remote streams. Leave empty if unknown.", children: (id) => _jsx(MbpsInput, { id: id, kbps: d.uploadSpeedKbps, onChange: (v) => s.update({ uploadSpeedKbps: v }), placeholder: "e.g. 40" }) }), _jsxs("div", { className: "grid gap-5 sm:grid-cols-2", children: [_jsx(Field, { label: "Total remote limit", help: "Across all remote streams. Empty = no limit.", children: (id) => _jsx(MbpsInput, { id: id, kbps: d.totalRemoteLimitKbps, onChange: (v) => s.update({ totalRemoteLimitKbps: v }), placeholder: "Unlimited" }) }), _jsx(Field, { label: "Per-stream remote limit", help: "Empty = no limit.", children: (id) => _jsx(MbpsInput, { id: id, kbps: d.perStreamRemoteLimitKbps, onChange: (v) => s.update({ perStreamRemoteLimitKbps: v }), placeholder: "Unlimited" }) })] }), _jsx(Field, { label: "Minimum per-stream bandwidth", help: "Floor when many remote streams share the upload. Below this, new streams start at the lowest quality.", children: (id) => _jsx(MbpsInput, { id: id, kbps: d.minStreamKbps, onChange: (v) => s.update({ minStreamKbps: v }) }) })] })] }), _jsx(SaveBar, { dirty: s.dirty, saving: s.saving, error: s.error, savedAt: s.savedAt, onSave: s.save, onReset: s.reset })] }));
}
function SecretField({ label, help, isSet, value, onChange }) {
    const [editing, setEditing] = useState(false);
    return (_jsx(Field, { label: label, help: help, children: (id) => editing || !isSet ? (_jsx(Input, { id: id, type: "password", autoComplete: "off", placeholder: "Paste API key", value: value ?? "", onChange: (e) => onChange(e.target.value) })) : (_jsxs("div", { className: "flex items-center gap-3", children: [value === "" ? (_jsx("span", { className: "rounded bg-danger/15 px-2 py-1 text-xs font-medium text-danger", children: "Removed when you save" })) : (_jsx("span", { className: "rounded bg-success/15 px-2 py-1 text-xs font-medium text-success", children: "Configured" })), _jsx(Button, { size: "sm", variant: "ghost", onClick: () => setEditing(true), children: "Replace" }), _jsx(Button, { size: "sm", variant: "ghost", onClick: () => onChange(""), children: "Remove" })] })) }));
}
export function MetadataSettings() {
    // Secrets are write-only: the server returns *Set flags; the draft carries new values to send.
    const s = useSectionDraft("metadata", (d) => {
        const x = d;
        return {
            tmdbApiKey: x.tmdbApiKey,
            fanartApiKey: x.fanartApiKey,
            openSubtitlesApiKey: x.openSubtitlesApiKey,
            omdbApiKey: x.omdbApiKey,
            omdbDailyLimit: d.omdbDailyLimit,
            animeEpisodeOrdering: d.animeEpisodeOrdering,
        };
    });
    if (!s.draft)
        return _jsx(Spinner, {});
    const d = s.draft;
    const set = (k) => (v) => s.update({ [k]: v });
    return (_jsxs(_Fragment, { children: [_jsxs("div", { className: "space-y-6", children: [_jsxs(Card, { title: "Providers", description: "Movies, TV and anime use TMDB, with AniList for anime. Music uses MusicBrainz (no key needed).", children: [_jsx(SecretField, { label: "TMDB API key", help: _jsx(_Fragment, { children: "Free at themoviedb.org \u2192 Settings \u2192 API. Required for movie and TV metadata." }), isSet: !!d.tmdbApiKeySet, value: d.tmdbApiKey, onChange: set("tmdbApiKey") }), _jsx(SecretField, { label: "Fanart.tv API key", help: "Optional. Adds logos, clear art and artist images.", isSet: !!d.fanartApiKeySet, value: d.fanartApiKey, onChange: set("fanartApiKey") }), _jsx(SecretField, { label: "OpenSubtitles API key", help: "Optional. Enables subtitle search from the player.", isSet: !!d.openSubtitlesApiKeySet, value: d.openSubtitlesApiKey, onChange: set("openSubtitlesApiKey") })] }), _jsxs(Card, { title: "Ratings", description: "IMDb, Rotten Tomatoes and Metacritic scores for movies and shows, from OMDb.", children: [_jsx(SecretField, { label: "OMDb API key", help: "Optional. Free keys (omdbapi.com) allow 1,000 lookups a day; a large library fills in over a few days.", isSet: !!d.omdbApiKeySet, value: d.omdbApiKey, onChange: set("omdbApiKey") }), _jsx(Field, { label: "Daily lookup limit", help: "Keep below your key's daily allowance. Ratings refresh every 30 days.", children: (id) => _jsx(Input, { id: id, type: "number", min: 1, value: d.omdbDailyLimit ?? 950, onChange: (e) => s.update({ omdbDailyLimit: Number(e.target.value) }) }) })] }), _jsx(Card, { title: "Anime", children: _jsx(Field, { label: "Default episode numbering", help: "Seasonal matches TVDB-style seasons. Absolute numbers episodes 1\u2026n. Each show can override this.", children: (id) => (_jsxs(Select, { id: id, value: d.animeEpisodeOrdering, onChange: (e) => s.update({ animeEpisodeOrdering: e.target.value }), children: [_jsx("option", { value: "seasonal", children: "Seasonal" }), _jsx("option", { value: "absolute", children: "Absolute" })] })) }) })] }), _jsx(SaveBar, { dirty: s.dirty, saving: s.saving, error: s.error, savedAt: s.savedAt, onSave: s.save, onReset: s.reset })] }));
}
export function TaskSettings() {
    const s = useSectionDraft("tasks");
    if (!s.draft)
        return _jsx(Spinner, {});
    return (_jsxs(_Fragment, { children: [_jsxs(Card, { title: "Maintenance", description: "Heavy jobs (metadata refresh, thumbnail and intro detection, backups) run inside this window.", children: [_jsxs("div", { className: "grid gap-5 sm:grid-cols-2", children: [_jsx(Field, { label: "Window starts at", children: (id) => _jsx(Input, { id: id, type: "time", value: s.draft.maintenanceWindowStart ?? "03:00", onChange: (e) => s.update({ maintenanceWindowStart: e.target.value }) }) }), _jsx(Field, { label: "Window length (hours)", children: (id) => _jsx(Input, { id: id, type: "number", min: 1, max: 12, value: s.draft.maintenanceWindowHours ?? 4, onChange: (e) => s.update({ maintenanceWindowHours: Number(e.target.value) }) }) })] }), _jsx(Field, { label: "Database backups to keep", help: "A backup is taken daily and before every upgrade.", children: (id) => _jsx(Input, { id: id, type: "number", min: 1, max: 60, value: s.draft.backupRetention ?? 7, onChange: (e) => s.update({ backupRetention: Number(e.target.value) }) }) })] }), _jsx(SaveBar, { dirty: s.dirty, saving: s.saving, error: s.error, savedAt: s.savedAt, onSave: s.save, onReset: s.reset })] }));
}
