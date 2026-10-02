import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Lock, MoreHorizontal, Pencil, RefreshCw, Search, Unlock } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { api, unwrap } from "@/api/client";
import { Alert, Button, Dialog, Field, Input, Spinner } from "@/components/ui";
function useItemUpdated() {
    const qc = useQueryClient();
    return (item) => {
        qc.setQueryData(["items", item.id], item);
        qc.invalidateQueries({ queryKey: ["items"] });
        qc.invalidateQueries({ queryKey: ["libraries"] });
    };
}
function FixMatchDialog({ item, onClose }) {
    const updated = useItemUpdated();
    const [title, setTitle] = useState(item.title);
    const [year, setYear] = useState(item.year ? String(item.year) : "");
    const [query, setQuery] = useState({ title: item.title, year: item.year });
    const cands = useQuery({
        queryKey: ["match", item.id, query],
        queryFn: () => unwrap(api.GET("/items/{itemId}/match", { params: { path: { itemId: item.id }, query: { title: query.title, year: query.year } } })),
    });
    const apply = useMutation({
        mutationFn: (id) => unwrap(api.PUT("/items/{itemId}/match", { params: { path: { itemId: item.id } }, body: { provider: "tmdb", id } })),
        onSuccess: (it) => {
            updated(it);
            onClose();
        },
    });
    const search = (e) => {
        e.preventDefault();
        setQuery({ title: title.trim(), year: year ? Number(year) : undefined });
    };
    return (_jsxs(Dialog, { open: true, wide: true, onClose: onClose, title: "Fix match", children: [_jsxs("form", { onSubmit: search, className: "mb-4 flex gap-2", children: [_jsx(Input, { "aria-label": "Title", value: title, onChange: (e) => setTitle(e.target.value) }), _jsx(Input, { "aria-label": "Year", placeholder: "Year", inputMode: "numeric", className: "w-24", value: year, onChange: (e) => setYear(e.target.value.replace(/\D/g, "").slice(0, 4)) }), _jsxs(Button, { type: "submit", variant: "secondary", children: [_jsx(Search, { className: "size-4" }), " Search"] })] }), (cands.isError || apply.isError) && _jsx(Alert, { tone: "error", children: (cands.error ?? apply.error)?.message }), cands.isFetching && _jsx(Spinner, { label: "Searching TMDB" }), cands.data?.length === 0 && _jsx("p", { className: "py-6 text-center text-sm text-muted", children: "No matches. Try a different title or remove the year." }), _jsx("ul", { className: "space-y-2", children: cands.data?.map((c) => (_jsx("li", { children: _jsxs("button", { disabled: apply.isPending, onClick: () => apply.mutate(c.id), className: "flex w-full gap-4 rounded-lg border border-border p-3 text-left hover:border-accent hover:bg-surface-2 disabled:opacity-60", children: [_jsx("span", { className: "h-24 w-16 shrink-0 overflow-hidden rounded bg-surface-3", children: c.posterUrl && _jsx("img", { src: c.posterUrl, alt: "", loading: "lazy", className: "size-full object-cover" }) }), _jsxs("span", { className: "min-w-0", children: [_jsxs("span", { className: "flex items-center gap-2 font-medium", children: [c.title, " ", c.year && _jsxs("span", { className: "text-muted", children: ["(", c.year, ")"] }), c.current && (_jsxs("span", { className: "flex items-center gap-1 rounded bg-success/15 px-1.5 text-[11px] text-success", children: [_jsx(Check, { className: "size-3" }), " Current"] }))] }), c.originalTitle && _jsx("span", { className: "block text-xs text-faint", children: c.originalTitle }), c.overview && _jsx("span", { className: "mt-1 line-clamp-2 block text-sm text-muted", children: c.overview })] })] }) }, c.id))) }), apply.isPending && _jsx(Spinner, { label: "Downloading metadata" })] }));
}
const fields = [
    { key: "title", label: "Title" },
    { key: "sortTitle", label: "Sort title" },
    { key: "originalTitle", label: "Original title" },
    { key: "originallyAvailableAt", label: "Release date", type: "date" },
    { key: "contentRating", label: "Content rating" },
    { key: "studio", label: "Studio / network" },
    { key: "tagline", label: "Tagline" },
    { key: "summary", label: "Summary", multiline: true },
];
function currentValue(item, key) {
    switch (key) {
        case "sortTitle":
            return "";
        case "year":
            return item.year ? String(item.year) : "";
        case "originallyAvailableAt":
            return item.originallyAvailableAt ?? "";
        default:
            return item[key] ?? "";
    }
}
function EditDialog({ item, onClose }) {
    const updated = useItemUpdated();
    const [values, setValues] = useState({});
    const [unlock, setUnlock] = useState([]);
    const locked = new Set(item.lockedFields);
    const save = useMutation({
        mutationFn: (body) => unwrap(api.PATCH("/items/{itemId}/metadata", { params: { path: { itemId: item.id } }, body })),
        onSuccess: (it) => {
            updated(it);
            onClose();
        },
    });
    const submit = () => {
        const body = { unlock: unlock.length ? unlock : undefined };
        for (const [k, v] of Object.entries(values))
            body[k] = v;
        save.mutate(body);
    };
    const dirty = Object.keys(values).length > 0 || unlock.length > 0;
    return (_jsxs(Dialog, { open: true, wide: true, onClose: onClose, title: `Edit ${item.title}`, footer: _jsxs(_Fragment, { children: [_jsx(Button, { variant: "ghost", onClick: onClose, children: "Cancel" }), _jsx(Button, { variant: "primary", disabled: !dirty, loading: save.isPending, onClick: submit, children: "Save" })] }), children: [save.isError && (_jsx("div", { className: "mb-4", children: _jsx(Alert, { tone: "error", children: save.error.message }) })), _jsxs("p", { className: "mb-4 text-sm text-muted", children: ["Edited fields are locked ", _jsx(Lock, { className: "inline size-3.5" }), " so metadata refreshes won't change them. Click a lock to hand a field back."] }), _jsx("div", { className: "space-y-4", children: fields.map((f) => {
                    const isLocked = (locked.has(f.key) || f.key in values) && !unlock.includes(f.key);
                    return (_jsx(Field, { label: f.label, children: (id) => (_jsxs("div", { className: "flex gap-2", children: [f.multiline ? (_jsx("textarea", { id: id, rows: 5, className: "w-full rounded-md border border-border bg-surface-2 px-3 py-2 text-sm focus:border-accent focus:outline-none", value: values[f.key] ?? currentValue(item, f.key), onChange: (e) => setValues({ ...values, [f.key]: e.target.value }) })) : (_jsx(Input, { id: id, type: f.type, value: values[f.key] ?? currentValue(item, f.key), placeholder: f.key === "sortTitle" ? "Automatic" : undefined, onChange: (e) => setValues({ ...values, [f.key]: e.target.value }) })), _jsx("button", { type: "button", disabled: !isLocked, onClick: () => {
                                        const { [f.key]: _drop, ...rest } = values;
                                        void _drop;
                                        setValues(rest);
                                        setUnlock([...unlock, f.key]);
                                    }, title: isLocked ? "Locked. Click to unlock." : "Not locked", "aria-label": isLocked ? `Unlock ${f.label}` : `${f.label} not locked`, className: isLocked ? "shrink-0 self-start rounded p-2 text-accent hover:bg-surface-2" : "shrink-0 self-start p-2 text-faint", children: isLocked ? _jsx(Lock, { className: "size-4" }) : _jsx(Unlock, { className: "size-4" }) })] })) }, f.key));
                }) })] }));
}
/** Admin actions for an item: edit, fix match, refresh. */
export function ItemActions({ item }) {
    const updated = useItemUpdated();
    const [open, setOpen] = useState(false);
    const [dialog, setDialog] = useState(null);
    const ref = useRef(null);
    const matchable = item.type === "movie" || item.type === "show";
    const refresh = useMutation({
        mutationFn: () => unwrap(api.POST("/items/{itemId}/refresh", { params: { path: { itemId: item.id } } })),
        onSuccess: updated,
    });
    useEffect(() => {
        if (!open)
            return;
        const onDown = (e) => !ref.current?.contains(e.target) && setOpen(false);
        document.addEventListener("mousedown", onDown);
        return () => document.removeEventListener("mousedown", onDown);
    }, [open]);
    const itemCls = "flex w-full items-center gap-3 px-4 py-2 text-left text-sm hover:bg-surface-2 disabled:opacity-50";
    return (_jsxs(_Fragment, { children: [_jsxs("div", { className: "relative", ref: ref, children: [_jsx(Button, { variant: "secondary", size: "sm", onClick: () => setOpen((v) => !v), "aria-label": "More actions", "aria-expanded": open, loading: refresh.isPending, children: _jsx(MoreHorizontal, { className: "size-4" }) }), open && (_jsxs("div", { className: "absolute left-0 z-30 mt-2 w-52 overflow-hidden rounded-lg border border-border bg-surface py-1 shadow-2xl", onClick: () => setOpen(false), children: [_jsxs("button", { className: itemCls, onClick: () => setDialog("edit"), children: [_jsx(Pencil, { className: "size-4" }), " Edit"] }), matchable && (_jsxs("button", { className: itemCls, onClick: () => setDialog("match"), children: [_jsx(Search, { className: "size-4" }), " Fix match\u2026"] })), matchable && item.matchState === "matched" && (_jsxs("button", { className: itemCls, onClick: () => refresh.mutate(), children: [_jsx(RefreshCw, { className: "size-4" }), " Refresh metadata"] }))] }))] }), refresh.isError && _jsx("span", { className: "text-sm text-danger", children: refresh.error.message }), dialog === "match" && _jsx(FixMatchDialog, { item: item, onClose: () => setDialog(null) }), dialog === "edit" && _jsx(EditDialog, { item: item, onClose: () => setDialog(null) })] }));
}
