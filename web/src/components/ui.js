import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { clsx } from "clsx";
import { Loader2, X } from "lucide-react";
import { useEffect, useId, useRef } from "react";
export function Button({ variant = "secondary", size = "md", loading, className, children, disabled, ...rest }) {
    return (_jsxs("button", { ...rest, disabled: disabled || loading, className: clsx("inline-flex items-center justify-center gap-2 rounded-md font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50", size === "sm" ? "h-8 px-3 text-sm" : "h-10 px-4 text-sm", variant === "primary" && "bg-accent text-black hover:bg-accent-strong", variant === "secondary" && "bg-surface-3 text-text hover:bg-border", variant === "ghost" && "text-muted hover:bg-surface-2 hover:text-text", variant === "danger" && "bg-danger/90 text-white hover:bg-danger", className), children: [loading && _jsx(Loader2, { className: "size-4 animate-spin", "aria-hidden": true }), children] }));
}
export function Field({ label, help, error, children }) {
    const id = useId();
    return (_jsxs("div", { className: "space-y-1.5", children: [_jsx("label", { htmlFor: id, className: "block text-sm font-medium text-text", children: label }), children(id), error ? _jsx("p", { className: "text-sm text-danger", children: error }) : help ? _jsx("p", { className: "text-sm text-muted", children: help }) : null] }));
}
export function Input({ className, ...rest }) {
    return (_jsx("input", { ...rest, className: clsx("h-10 w-full rounded-md border border-border bg-surface-2 px-3 text-sm text-text placeholder:text-faint focus:border-accent focus:outline-none", className) }));
}
export function Select({ className, children, ...rest }) {
    return (_jsx("select", { ...rest, className: clsx("h-10 w-full rounded-md border border-border bg-surface-2 px-3 text-sm text-text focus:border-accent focus:outline-none", className), children: children }));
}
/** A labelled on/off switch row, used throughout settings. */
export function Toggle({ label, help, checked, onChange }) {
    const id = useId();
    return (_jsxs("div", { className: "flex items-start justify-between gap-6", children: [_jsxs("div", { children: [_jsx("label", { htmlFor: id, className: "text-sm font-medium text-text", children: label }), help && _jsx("p", { className: "mt-0.5 text-sm text-muted", children: help })] }), _jsx("button", { id: id, type: "button", role: "switch", "aria-checked": checked, onClick: () => onChange(!checked), className: clsx("relative mt-0.5 h-6 w-11 shrink-0 rounded-full transition-colors", checked ? "bg-accent" : "bg-surface-3"), children: _jsx("span", { className: clsx("absolute top-0.5 left-0.5 size-5 rounded-full bg-white shadow transition-transform", checked && "translate-x-5") }) })] }));
}
export function Card({ title, description, children, actions }) {
    return (_jsxs("section", { className: "rounded-lg border border-border bg-surface", children: [(title || actions) && (_jsxs("header", { className: "flex items-start justify-between gap-4 border-b border-border px-5 py-4", children: [_jsxs("div", { children: [title && _jsx("h2", { className: "text-base font-semibold", children: title }), description && _jsx("p", { className: "mt-0.5 text-sm text-muted", children: description })] }), actions] })), _jsx("div", { className: "space-y-5 px-5 py-5", children: children })] }));
}
export function Alert({ tone = "info", children }) {
    return (_jsx("div", { role: tone === "error" ? "alert" : "status", className: clsx("rounded-md border px-4 py-3 text-sm", tone === "info" && "border-border bg-surface-2 text-muted", tone === "error" && "border-danger/40 bg-danger/10 text-danger", tone === "success" && "border-success/40 bg-success/10 text-success"), children: children }));
}
export function Spinner({ label = "Loading" }) {
    return (_jsxs("div", { className: "flex items-center gap-2 p-8 text-muted", role: "status", children: [_jsx(Loader2, { className: "size-5 animate-spin", "aria-hidden": true }), _jsxs("span", { className: "text-sm", children: [label, "\u2026"] })] }));
}
/** Modal dialog built on the native <dialog> element (focus trap and Esc handling for free). */
export function Dialog({ open, onClose, title, children, footer, wide }) {
    const ref = useRef(null);
    useEffect(() => {
        const d = ref.current;
        if (!d)
            return;
        if (open && !d.open)
            d.showModal();
        if (!open && d.open)
            d.close();
    }, [open]);
    return (_jsx("dialog", { ref: ref, onClose: onClose, onCancel: (e) => {
            e.preventDefault();
            onClose();
        }, className: clsx("m-auto w-[calc(100%-2rem)] rounded-lg border border-border bg-surface p-0 text-text shadow-2xl backdrop:bg-black/70", wide ? "max-w-2xl" : "max-w-md"), children: open && (_jsxs("div", { className: "flex max-h-[85vh] flex-col", children: [_jsxs("header", { className: "flex items-center justify-between border-b border-border px-5 py-4", children: [_jsx("h2", { className: "text-lg font-semibold", children: title }), _jsx("button", { type: "button", onClick: onClose, className: "rounded p-1 text-muted hover:bg-surface-2 hover:text-text", "aria-label": "Close", children: _jsx(X, { className: "size-5" }) })] }), _jsx("div", { className: clsx("flex-1 overflow-y-auto px-5 py-5", wide && "min-h-[26rem]"), children: children }), footer && _jsx("footer", { className: "flex justify-end gap-2 border-t border-border px-5 py-4", children: footer })] })) }));
}
/** Editable list of short strings (subnets, ignore patterns, browse roots). */
export function StringListEditor({ values, onChange, placeholder, addLabel = "Add" }) {
    return (_jsxs("div", { className: "space-y-2", children: [values.map((v, i) => (_jsxs("div", { className: "flex gap-2", children: [_jsx(Input, { value: v, placeholder: placeholder, onChange: (e) => onChange(values.map((x, j) => (j === i ? e.target.value : x))), "aria-label": `${placeholder ?? "Value"} ${i + 1}` }), _jsx(Button, { variant: "ghost", type: "button", onClick: () => onChange(values.filter((_, j) => j !== i)), "aria-label": "Remove", children: _jsx(X, { className: "size-4" }) })] }, i))), _jsxs(Button, { variant: "ghost", size: "sm", type: "button", onClick: () => onChange([...values, ""]), children: ["+ ", addLabel] })] }));
}
