import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useQuery } from "@tanstack/react-query";
import { ChevronLeft, Folder, FolderOpen } from "lucide-react";
import { useState } from "react";
import { browseQuery } from "@/api/queries";
import { Alert, Button, Spinner } from "@/components/ui";
/** Browses folders on the server (inside the configured browse roots) and picks one. */
export function FolderBrowser({ onPick, initialPath = null }) {
    const [path, setPath] = useState(initialPath);
    const listing = useQuery(browseQuery(path));
    return (_jsxs("div", { className: "overflow-hidden rounded-md border border-border", children: [_jsxs("div", { className: "flex items-center gap-2 border-b border-border bg-surface-2 px-3 py-2", children: [_jsx(Button, { size: "sm", variant: "ghost", "aria-label": "Up one folder", disabled: path === null, onClick: () => setPath(listing.data?.parent ?? null), children: _jsx(ChevronLeft, { className: "size-4" }) }), _jsx("span", { className: "min-w-0 flex-1 truncate font-mono text-sm text-muted", title: path ?? undefined, children: path ?? "Media roots" }), path && (_jsx(Button, { size: "sm", variant: "primary", onClick: () => onPick(path), children: "Use this folder" }))] }), _jsxs("div", { className: "max-h-64 overflow-y-auto", children: [listing.isPending && _jsx(Spinner, {}), listing.isError && (_jsx("div", { className: "p-3", children: _jsx(Alert, { tone: "error", children: listing.error.message }) })), listing.data?.entries.length === 0 && _jsx("p", { className: "p-4 text-sm text-faint", children: "No subfolders." }), _jsx("ul", { children: listing.data?.entries.map((e) => (_jsx("li", { children: _jsxs("button", { type: "button", onClick: () => setPath(e.path), className: "flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm hover:bg-surface-2", children: [path === null ? _jsx(FolderOpen, { className: "size-4 text-accent", "aria-hidden": true }) : _jsx(Folder, { className: "size-4 text-faint", "aria-hidden": true }), _jsx("span", { className: "truncate", children: e.name })] }) }, e.path))) })] })] }));
}
