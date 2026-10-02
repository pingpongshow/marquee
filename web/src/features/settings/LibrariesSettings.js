import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useQuery } from "@tanstack/react-query";
import { Folder, Pencil, Plus, RefreshCw, Square, Trash2 } from "lucide-react";
import { useState } from "react";
import { librariesQuery, useCancelScan, useDeleteLibrary, useScanAll, useScanLibrary } from "@/api/queries";
import { libraryIcons } from "@/app/Shell";
import { Alert, Button, Card, Dialog, Field, Input, Spinner, StringListEditor, Toggle } from "@/components/ui";
import { LibraryDialog } from "./LibraryDialog";
import { SaveBar } from "./SaveBar";
import { useSectionDraft } from "./useSectionDraft";
const typeLabels = { movies: "Movies", shows: "TV Shows", anime: "Anime", music: "Music", videos: "Other Videos", photos: "Photos" };
function ScanStatus({ lib }) {
    const p = lib.scanProgress;
    if (lib.scanStatus === "queued")
        return _jsx("div", { className: "mt-2 text-xs text-accent", children: "Waiting to scan\u2026" });
    if (lib.scanStatus === "scanning") {
        const pct = p && p.total > 0 ? Math.round((p.done / p.total) * 100) : null;
        const label = p?.phase === "walking"
            ? "Finding files…"
            : p?.phase === "cleanup"
                ? "Finishing up…"
                : p?.phase === "metadata"
                    ? `Getting metadata ${p.done} of ${p.total}`
                    : `Scanning ${p?.done ?? 0} of ${p?.total ?? 0}`;
        return (_jsxs("div", { className: "mt-2 space-y-1", children: [_jsxs("div", { className: "flex justify-between text-xs text-accent", children: [_jsx("span", { children: label }), pct !== null && _jsxs("span", { children: [pct, "%"] })] }), _jsx("div", { className: "h-1.5 overflow-hidden rounded-full bg-surface-3", role: "progressbar", "aria-valuenow": pct ?? undefined, "aria-valuemin": 0, "aria-valuemax": 100, children: _jsx("div", { className: pct === null ? "h-full w-1/3 animate-pulse bg-accent" : "h-full bg-accent transition-all", style: pct === null ? undefined : { width: `${pct}%` } }) }), p?.current && _jsx("div", { className: "truncate text-xs text-faint", children: p.current })] }));
    }
    return (_jsxs("div", { className: "mt-1 text-xs text-faint", children: [lib.itemCount, " items \u00B7 ", lib.lastScannedAt ? `Scanned ${new Date(lib.lastScannedAt).toLocaleString()}` : "Not scanned yet", lib.lastScanError && _jsxs("div", { className: "text-danger", children: ["Last scan failed: ", lib.lastScanError] }), lib.lastScanResult && !lib.lastScanError && _jsx("div", { children: lib.lastScanResult })] }));
}
export function LibrariesSettings() {
    const libraries = useQuery(librariesQuery);
    const del = useDeleteLibrary();
    const scan = useScanLibrary();
    const cancel = useCancelScan();
    const scanAll = useScanAll();
    const [dialog, setDialog] = useState(null);
    const [confirmDelete, setConfirmDelete] = useState(null);
    const scanning = useSectionDraft("library");
    return (_jsxs("div", { className: "space-y-6", children: [_jsxs(Card, { title: "Libraries", actions: _jsxs("div", { className: "flex gap-2", children: [!!libraries.data?.length && (_jsxs(Button, { size: "sm", variant: "ghost", onClick: () => scanAll.mutate(), loading: scanAll.isPending, children: [_jsx(RefreshCw, { className: "size-4" }), " Scan all"] })), _jsxs(Button, { variant: "primary", size: "sm", onClick: () => setDialog({ key: Date.now() }), children: [_jsx(Plus, { className: "size-4" }), " Add library"] })] }), children: [libraries.isPending && _jsx(Spinner, {}), libraries.isError && _jsx(Alert, { tone: "error", children: libraries.error.message }), libraries.data?.length === 0 && _jsx("p", { className: "text-sm text-muted", children: "No libraries yet. Add one to get started." }), _jsx("ul", { className: "-my-2 divide-y divide-border", children: libraries.data?.map((lib) => {
                            const Icon = libraryIcons[lib.type];
                            return (_jsxs("li", { className: "flex items-start gap-4 py-4", children: [_jsx("div", { className: "flex size-10 shrink-0 items-center justify-center rounded-md bg-surface-3", children: _jsx(Icon, { className: "size-5 text-accent", "aria-hidden": true }) }), _jsxs("div", { className: "min-w-0 flex-1", children: [_jsxs("div", { className: "flex items-baseline gap-2", children: [_jsx("span", { className: "font-medium", children: lib.name }), _jsx("span", { className: "text-xs text-faint", children: typeLabels[lib.type] })] }), _jsx("ul", { className: "mt-1 space-y-0.5", children: lib.paths.map((p) => (_jsxs("li", { className: "flex items-center gap-1.5 truncate font-mono text-xs text-muted", children: [_jsx(Folder, { className: "size-3 shrink-0", "aria-hidden": true }), " ", p] }, p))) }), _jsx(ScanStatus, { lib: lib })] }), _jsxs("div", { className: "flex shrink-0 gap-1", children: [lib.scanStatus === "idle" ? (_jsx(Button, { size: "sm", variant: "ghost", "aria-label": `Scan ${lib.name}`, title: "Scan library files", onClick: () => scan.mutate(lib.id), children: _jsx(RefreshCw, { className: "size-4" }) })) : (_jsx(Button, { size: "sm", variant: "ghost", "aria-label": `Stop scanning ${lib.name}`, title: "Stop scan", onClick: () => cancel.mutate(lib.id), children: _jsx(Square, { className: "size-4" }) })), _jsx(Button, { size: "sm", variant: "ghost", "aria-label": `Edit ${lib.name}`, onClick: () => setDialog({ library: lib, key: Date.now() }), children: _jsx(Pencil, { className: "size-4" }) }), _jsx(Button, { size: "sm", variant: "ghost", "aria-label": `Delete ${lib.name}`, onClick: () => setConfirmDelete(lib), children: _jsx(Trash2, { className: "size-4" }) })] })] }, lib.id));
                        }) })] }), scanning.draft && (_jsxs(_Fragment, { children: [_jsxs(Card, { title: "Scanning", description: "Applies to all libraries.", children: [_jsx(Toggle, { label: "Scan automatically when files change", help: "Watches library folders and picks up new, renamed and deleted files within seconds.", checked: !!scanning.draft.watchFilesystem, onChange: (v) => scanning.update({ watchFilesystem: v }) }), _jsx(Toggle, { label: "Scan all libraries when the server starts", checked: !!scanning.draft.scanOnStartup, onChange: (v) => scanning.update({ scanOnStartup: v }) }), _jsx(Field, { label: "Mark as watched after (%)", help: "Progress past this point counts as watched.", children: (id) => _jsx(Input, { id: id, type: "number", min: 50, max: 100, value: scanning.draft.watchedThresholdPercent ?? 90, onChange: (e) => scanning.update({ watchedThresholdPercent: Number(e.target.value) }) }) }), _jsx(Field, { label: "Ignore files matching", help: "Temporary and partial downloads are skipped. Patterns use * and ? wildcards.", children: () => _jsx(StringListEditor, { values: scanning.draft.ignorePatterns ?? [], onChange: (v) => scanning.update({ ignorePatterns: v }), placeholder: "*_staging.*", addLabel: "Add pattern" }) }), _jsx(Field, { label: "Folders the folder picker can browse", help: "Paths inside the Marquee container (e.g. /media).", children: () => _jsx(StringListEditor, { values: scanning.draft.browseRoots ?? [], onChange: (v) => scanning.update({ browseRoots: v }), placeholder: "/media", addLabel: "Add folder" }) })] }), _jsx(SaveBar, { dirty: scanning.dirty, saving: scanning.saving, error: scanning.error, savedAt: scanning.savedAt, onSave: scanning.save, onReset: scanning.reset })] })), dialog && _jsx(LibraryDialog, { open: true, library: dialog.library, onClose: () => setDialog(null) }, dialog.key), _jsxs(Dialog, { open: !!confirmDelete, onClose: () => setConfirmDelete(null), title: "Delete library?", footer: _jsxs(_Fragment, { children: [_jsx(Button, { variant: "ghost", onClick: () => setConfirmDelete(null), children: "Cancel" }), _jsx(Button, { variant: "danger", loading: del.isPending, onClick: () => confirmDelete && del.mutate(confirmDelete.id, { onSuccess: () => setConfirmDelete(null) }), children: "Delete library" })] }), children: [_jsxs("p", { className: "text-sm text-muted", children: [_jsx("strong", { className: "text-text", children: confirmDelete?.name }), " and its metadata, artwork and watch history will be removed from Marquee. Your media files are not touched."] }), del.isError && (_jsx("div", { className: "mt-3", children: _jsx(Alert, { tone: "error", children: del.error.message }) }))] })] }));
}
