import { Link } from "@tanstack/react-router";
import { X } from "lucide-react";
import { useSyncExternalStore } from "react";

type ToastLink =
  | { label: string; to: "/playlist/$playlistId"; params: { playlistId: string } }
  | { label: string; to: "/item/$itemId"; params: { itemId: string } };

type ToastAction = { label: string; onClick: () => void };
type Toast = { id: number; message: string; link?: ToastLink; action?: ToastAction; tone?: "info" | "error" };

let toasts: Toast[] = [];
let nextId = 1;
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((l) => l());

/** Shows a short message at the bottom of the screen for a few seconds. Callable from anywhere. */
export function toast(message: string, opts?: { link?: ToastLink; action?: ToastAction; tone?: "info" | "error"; ms?: number }) {
  const id = nextId++;
  toasts = [...toasts, { id, message, link: opts?.link, action: opts?.action, tone: opts?.tone }].slice(-4);
  emit();
  window.setTimeout(() => dismiss(id), opts?.ms ?? (opts?.link || opts?.action ? 8000 : 4500));
}

function dismiss(id: number) {
  toasts = toasts.filter((t) => t.id !== id);
  emit();
}

function subscribe(fn: () => void) {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}

/** Renders the toasts; mounted once, above the players. */
export function Toaster() {
  const list = useSyncExternalStore(subscribe, () => toasts);
  if (!list.length) return null;
  return (
    <div className="pointer-events-none fixed inset-x-0 bottom-24 z-[70] flex flex-col items-center gap-2 px-4" aria-live="polite">
      {list.map((t) => (
        <div
          key={t.id}
          role="status"
          className={
            "pointer-events-auto flex max-w-md items-center gap-3 rounded-lg border px-4 py-2.5 text-sm shadow-2xl backdrop-blur " +
            (t.tone === "error" ? "border-danger/50 bg-danger/20 text-text" : "border-border bg-surface/95 text-text")
          }
        >
          <span className="min-w-0 flex-1">{t.message}</span>
          {t.link?.to === "/playlist/$playlistId" && (
            <Link to={t.link.to} params={t.link.params} className="shrink-0 font-semibold text-accent hover:underline" onClick={() => dismiss(t.id)}>
              {t.link.label}
            </Link>
          )}
          {t.link?.to === "/item/$itemId" && (
            <Link to={t.link.to} params={t.link.params} className="shrink-0 font-semibold text-accent hover:underline" onClick={() => dismiss(t.id)}>
              {t.link.label}
            </Link>
          )}
          {t.action && (
            <button
              className="shrink-0 font-semibold text-accent hover:underline"
              onClick={() => {
                dismiss(t.id);
                t.action!.onClick();
              }}
            >
              {t.action.label}
            </button>
          )}
          <button onClick={() => dismiss(t.id)} className="shrink-0 rounded p-0.5 text-muted hover:text-text" aria-label="Dismiss">
            <X className="size-4" />
          </button>
        </div>
      ))}
    </div>
  );
}
