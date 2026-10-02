import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useState } from "react";
import { Alert, Button } from "@/components/ui";
/** Sticky footer shown while a settings section has unsaved changes. */
export function SaveBar({ dirty, saving, error, savedAt, onSave, onReset }) {
    // "Saved" shows for a moment after each save, then hides.
    const [hiddenFor, setHiddenFor] = useState(null);
    const showSaved = savedAt !== null && hiddenFor !== savedAt;
    useEffect(() => {
        if (!savedAt)
            return;
        const t = setTimeout(() => setHiddenFor(savedAt), 2500);
        return () => clearTimeout(t);
    }, [savedAt]);
    if (!dirty && !showSaved && !error)
        return null;
    return (_jsxs("div", { className: "sticky bottom-0 -mx-6 mt-6 space-y-3 border-t border-border bg-bg/95 px-6 py-4 backdrop-blur lg:-mx-8 lg:px-8", children: [error && _jsx(Alert, { tone: "error", children: error.message }), _jsxs("div", { className: "flex items-center justify-end gap-3", children: [showSaved && !dirty && _jsx("span", { className: "text-sm text-success", role: "status", children: "Saved" }), dirty && _jsx("span", { className: "mr-auto text-sm text-muted", children: "You have unsaved changes" }), _jsx(Button, { variant: "ghost", onClick: onReset, disabled: !dirty || saving, children: "Discard" }), _jsx(Button, { variant: "primary", onClick: onSave, disabled: !dirty, loading: saving, children: "Save changes" })] })] }));
}
