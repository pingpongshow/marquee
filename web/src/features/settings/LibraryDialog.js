import { jsx as _jsx, Fragment as _Fragment, jsxs as _jsxs } from "react/jsx-runtime";
import { clsx } from "clsx";
import { Folder, X } from "lucide-react";
import { useState } from "react";
import { useCreateLibrary, useUpdateLibrary } from "@/api/queries";
import { libraryIcons } from "@/app/Shell";
import { Alert, Button, Dialog, Field, Input, Select, StringListEditor, Toggle } from "@/components/ui";
import { FolderBrowser } from "./FolderBrowser";
import { languages } from "./ServerSections";
const types = [
    { type: "movies", label: "Movies", help: "Movie (Year)/Movie (Year).mkv" },
    { type: "shows", label: "TV Shows", help: "Show/Season 01/Show - S01E01.mkv" },
    { type: "anime", label: "Anime", help: "TV layout with anime metadata and episode numbering" },
    { type: "music", label: "Music", help: "Organised by embedded tags" },
    { type: "videos", label: "Other Videos", help: "Home and personal videos, organised by folder" },
];
const defaultNames = { movies: "Movies", shows: "TV Shows", anime: "Anime", music: "Music", videos: "Home Videos", photos: "Photos" };
/** Add (library undefined) or edit a library. Mirrors Plex's add-library flow: type → folders → advanced. */
export function LibraryDialog({ open, onClose, library }) {
    const editing = !!library;
    const [step, setStep] = useState(editing ? "folders" : "type");
    const [type, setType] = useState(library?.type ?? "movies");
    const [name, setName] = useState(library?.name ?? defaultNames.movies);
    const [nameTouched, setNameTouched] = useState(editing);
    const [paths, setPaths] = useState(library?.paths ?? []);
    const [browsing, setBrowsing] = useState(!editing);
    const [opts, setOpts] = useState(library?.options ?? {});
    const create = useCreateLibrary();
    const update = useUpdateLibrary();
    const mutation = editing ? update : create;
    const isTV = type === "shows" || type === "anime";
    const submit = () => {
        const options = { ...opts };
        if (!isTV)
            delete options.episodeOrdering;
        if (editing)
            update.mutate({ id: library.id, body: { name, paths, options } }, { onSuccess: onClose });
        else
            // The server queues the first scan itself.
            create.mutate({ name, type, paths, options }, { onSuccess: onClose });
    };
    const steps = [
        ...(editing ? [] : [{ id: "type", label: "Type" }]),
        { id: "folders", label: "Folders" },
        { id: "advanced", label: "Advanced" },
    ];
    const footer = (_jsxs(_Fragment, { children: [_jsx(Button, { variant: "ghost", onClick: onClose, children: "Cancel" }), step === "type" && (_jsx(Button, { variant: "primary", onClick: () => setStep("folders"), disabled: !name.trim(), children: "Next" })), step === "folders" && !editing && (_jsx(Button, { variant: "secondary", onClick: () => setStep("advanced"), disabled: paths.length === 0, children: "Advanced" })), (step === "advanced" || step === "folders") && (_jsx(Button, { variant: "primary", onClick: submit, loading: mutation.isPending, disabled: paths.length === 0 || !name.trim(), children: editing ? "Save changes" : "Add library" }))] }));
    return (_jsxs(Dialog, { open: open, onClose: onClose, title: editing ? `Edit ${library.name}` : "Add library", footer: footer, wide: true, children: [_jsx("div", { className: "mb-5 flex gap-1 border-b border-border", children: steps.map((s) => (_jsx("button", { type: "button", onClick: () => (s.id === "type" || name.trim()) && setStep(s.id), className: clsx("-mb-px border-b-2 px-3 py-2 text-sm", step === s.id ? "border-accent font-medium text-text" : "border-transparent text-muted hover:text-text"), children: s.label }, s.id))) }), mutation.isError && (_jsx("div", { className: "mb-4", children: _jsx(Alert, { tone: "error", children: mutation.error.message }) })), step === "type" && (_jsxs("div", { className: "space-y-5", children: [_jsx("div", { className: "grid gap-2 sm:grid-cols-2", children: types.map((t) => {
                            const Icon = libraryIcons[t.type];
                            return (_jsxs("button", { type: "button", onClick: () => {
                                    setType(t.type);
                                    if (!nameTouched)
                                        setName(defaultNames[t.type]);
                                }, "aria-pressed": type === t.type, className: clsx("flex items-start gap-3 rounded-lg border p-3 text-left transition-colors", type === t.type ? "border-accent bg-accent/10" : "border-border hover:bg-surface-2"), children: [_jsx(Icon, { className: clsx("mt-0.5 size-5", type === t.type ? "text-accent" : "text-muted"), "aria-hidden": true }), _jsxs("div", { children: [_jsx("div", { className: "text-sm font-medium", children: t.label }), _jsx("div", { className: "text-xs text-muted", children: t.help })] })] }, t.type));
                        }) }), _jsx(Field, { label: "Library name", children: (id) => (_jsx(Input, { id: id, maxLength: 64, value: name, onChange: (e) => {
                                setName(e.target.value);
                                setNameTouched(true);
                            } })) }), _jsx(Field, { label: "Language", help: "For titles, summaries and artwork.", children: (id) => (_jsx(Select, { id: id, value: opts.language ?? "en-US", onChange: (e) => setOpts({ ...opts, language: e.target.value }), children: languages.map(([v, l]) => (_jsx("option", { value: v, children: l }, v))) })) })] })), step === "folders" && (_jsxs("div", { className: "space-y-4", children: [editing && (_jsx(Field, { label: "Library name", children: (id) => _jsx(Input, { id: id, maxLength: 64, value: name, onChange: (e) => setName(e.target.value) }) })), _jsxs("div", { children: [_jsx("div", { className: "mb-2 text-sm font-medium", children: "Folders" }), paths.length === 0 ? (_jsx("p", { className: "text-sm text-muted", children: "Choose at least one folder that contains your media." })) : (_jsx("ul", { className: "space-y-1.5", children: paths.map((p) => (_jsxs("li", { className: "flex items-center gap-2 rounded-md bg-surface-2 px-3 py-2", children: [_jsx(Folder, { className: "size-4 text-accent", "aria-hidden": true }), _jsx("span", { className: "flex-1 truncate font-mono text-sm", children: p }), _jsx("button", { type: "button", className: "rounded p-1 text-muted hover:text-text", "aria-label": `Remove ${p}`, onClick: () => setPaths(paths.filter((x) => x !== p)), children: _jsx(X, { className: "size-4" }) })] }, p))) }))] }), browsing ? (_jsx(FolderBrowser, { onPick: (p) => {
                            if (!paths.includes(p))
                                setPaths([...paths, p]);
                            setBrowsing(false);
                        } })) : (_jsx(Button, { variant: "secondary", size: "sm", onClick: () => setBrowsing(true), children: "Browse for folder" }))] })), step === "advanced" && (_jsxs("div", { className: "space-y-5", children: [isTV && (_jsx(Field, { label: "Episode ordering", help: type === "anime" ? "Anime is often numbered absolutely (1…n) rather than by season." : undefined, children: (id) => (_jsxs(Select, { id: id, value: opts.episodeOrdering ?? "aired", onChange: (e) => setOpts({ ...opts, episodeOrdering: e.target.value }), children: [_jsx("option", { value: "aired", children: "Aired (by season)" }), _jsx("option", { value: "absolute", children: "Absolute" }), _jsx("option", { value: "dvd", children: "DVD" })] })) })), _jsx(Toggle, { label: "Show on Home", help: "Include this library\u2019s hubs (Recently Added, Continue Watching) on the Home screen.", checked: opts.includeInHome ?? true, onChange: (v) => setOpts({ ...opts, includeInHome: v }) }), _jsx(Toggle, { label: "Include in search", checked: opts.includeInSearch ?? true, onChange: (v) => setOpts({ ...opts, includeInSearch: v }) }), _jsx(Field, { label: "Full rescan every (hours)", help: "New files are picked up instantly by file watching; this is a safety net. 0 = never.", children: (id) => _jsx(Input, { id: id, type: "number", min: 0, value: opts.scanIntervalHours ?? 24, onChange: (e) => setOpts({ ...opts, scanIntervalHours: Number(e.target.value) }) }) }), _jsx(Field, { label: "Ignore files matching", help: "Added to the server-wide ignore patterns. Example: *_staging.*", children: () => _jsx(StringListEditor, { values: opts.ignorePatterns ?? [], onChange: (v) => setOpts({ ...opts, ignorePatterns: v }), placeholder: "*.sample.*", addLabel: "Add pattern" }) })] }))] }));
}
