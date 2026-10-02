import { clsx } from "clsx";
import { Loader2, X } from "lucide-react";
import { useEffect, useId, useRef, type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes } from "react";

type Variant = "primary" | "secondary" | "ghost" | "danger";

export function Button({
  variant = "secondary",
  size = "md",
  loading,
  className,
  children,
  disabled,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; size?: "sm" | "md"; loading?: boolean }) {
  return (
    <button
      {...rest}
      disabled={disabled || loading}
      className={clsx(
        "inline-flex items-center justify-center gap-2 rounded-md font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50",
        size === "sm" ? "h-8 px-3 text-sm" : "h-10 px-4 text-sm",
        variant === "primary" && "bg-accent text-black hover:bg-accent-strong",
        variant === "secondary" && "bg-surface-3 text-text hover:bg-border",
        variant === "ghost" && "text-muted hover:bg-surface-2 hover:text-text",
        variant === "danger" && "bg-danger/90 text-white hover:bg-danger",
        className,
      )}
    >
      {loading && <Loader2 className="size-4 animate-spin" aria-hidden />}
      {children}
    </button>
  );
}

export function Field({ label, help, error, children }: { label: string; help?: ReactNode; error?: string; children: (id: string) => ReactNode }) {
  const id = useId();
  return (
    <div className="space-y-1.5">
      <label htmlFor={id} className="block text-sm font-medium text-text">
        {label}
      </label>
      {children(id)}
      {error ? <p className="text-sm text-danger">{error}</p> : help ? <p className="text-sm text-muted">{help}</p> : null}
    </div>
  );
}

export function Input({ className, ...rest }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      {...rest}
      className={clsx(
        "h-10 w-full rounded-md border border-border bg-surface-2 px-3 text-sm text-text placeholder:text-faint focus:border-accent focus:outline-none",
        className,
      )}
    />
  );
}

export function Select({ className, children, ...rest }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      {...rest}
      className={clsx("h-10 w-full rounded-md border border-border bg-surface-2 px-3 text-sm text-text focus:border-accent focus:outline-none", className)}
    >
      {children}
    </select>
  );
}

/** A labelled on/off switch row, used throughout settings. */
export function Toggle({ label, help, checked, onChange }: { label: string; help?: ReactNode; checked: boolean; onChange: (v: boolean) => void }) {
  const id = useId();
  return (
    <div className="flex items-start justify-between gap-6">
      <div>
        <label htmlFor={id} className="text-sm font-medium text-text">
          {label}
        </label>
        {help && <p className="mt-0.5 text-sm text-muted">{help}</p>}
      </div>
      <button
        id={id}
        type="button"
        role="switch"
        aria-checked={checked}
        onClick={() => onChange(!checked)}
        className={clsx("relative mt-0.5 h-6 w-11 shrink-0 rounded-full transition-colors", checked ? "bg-accent" : "bg-surface-3")}
      >
        <span className={clsx("absolute top-0.5 left-0.5 size-5 rounded-full bg-white shadow transition-transform", checked && "translate-x-5")} />
      </button>
    </div>
  );
}

export function Card({ title, description, children, actions }: { title?: string; description?: ReactNode; children: ReactNode; actions?: ReactNode }) {
  return (
    <section className="rounded-lg border border-border bg-surface">
      {(title || actions) && (
        <header className="flex items-start justify-between gap-4 border-b border-border px-5 py-4">
          <div>
            {title && <h2 className="text-base font-semibold">{title}</h2>}
            {description && <p className="mt-0.5 text-sm text-muted">{description}</p>}
          </div>
          {actions}
        </header>
      )}
      <div className="space-y-5 px-5 py-5">{children}</div>
    </section>
  );
}

export function Badge({ children, tone = "muted" }: { children: ReactNode; tone?: "accent" | "muted" | "danger" }) {
  return (
    <span
      className={clsx(
        "rounded px-1.5 py-0.5 text-[11px]",
        tone === "accent" && "bg-accent/15 font-medium text-accent",
        tone === "muted" && "bg-surface-3 text-muted",
        tone === "danger" && "bg-danger/15 font-medium text-danger",
      )}
    >
      {children}
    </span>
  );
}

export function Alert({ tone = "info", children }: { tone?: "info" | "error" | "success"; children: ReactNode }) {
  return (
    <div
      role={tone === "error" ? "alert" : "status"}
      className={clsx(
        "rounded-md border px-4 py-3 text-sm",
        tone === "info" && "border-border bg-surface-2 text-muted",
        tone === "error" && "border-danger/40 bg-danger/10 text-danger",
        tone === "success" && "border-success/40 bg-success/10 text-success",
      )}
    >
      {children}
    </div>
  );
}

export function Spinner({ label = "Loading" }: { label?: string }) {
  return (
    <div className="flex items-center gap-2 p-8 text-muted" role="status">
      <Loader2 className="size-5 animate-spin" aria-hidden />
      <span className="text-sm">{label}…</span>
    </div>
  );
}

/** Modal dialog built on the native <dialog> element (focus trap and Esc handling for free). */
export function Dialog({ open, onClose, title, children, footer, wide }: { open: boolean; onClose: () => void; title: string; children: ReactNode; footer?: ReactNode; wide?: boolean }) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) d.showModal();
    if (!open && d.open) d.close();
  }, [open]);
  return (
    <dialog
      ref={ref}
      aria-labelledby={titleId}
      onClose={onClose}
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
      className={clsx(
        "m-auto w-[calc(100%-2rem)] rounded-lg border border-border bg-surface p-0 text-text shadow-2xl backdrop:bg-black/70",
        wide ? "max-w-2xl" : "max-w-md",
      )}
    >
      {open && (
        <div className="flex max-h-[85vh] flex-col">
          <header className="flex items-center justify-between border-b border-border px-5 py-4">
            <h2 id={titleId} className="text-lg font-semibold">
              {title}
            </h2>
            <button type="button" onClick={onClose} className="rounded p-1 text-muted hover:bg-surface-2 hover:text-text" aria-label="Close">
              <X className="size-5" />
            </button>
          </header>
          <div className={clsx("flex-1 overflow-y-auto px-5 py-5", wide && "min-h-[26rem]")}>{children}</div>
          {footer && <footer className="flex justify-end gap-2 border-t border-border px-5 py-4">{footer}</footer>}
        </div>
      )}
    </dialog>
  );
}

/** Editable list of short strings (subnets, ignore patterns, browse roots). */
export function StringListEditor({ values, onChange, placeholder, addLabel = "Add" }: { values: string[]; onChange: (v: string[]) => void; placeholder?: string; addLabel?: string }) {
  return (
    <div className="space-y-2">
      {values.map((v, i) => (
        <div key={i} className="flex gap-2">
          <Input
            value={v}
            placeholder={placeholder}
            onChange={(e) => onChange(values.map((x, j) => (j === i ? e.target.value : x)))}
            aria-label={`${placeholder ?? "Value"} ${i + 1}`}
          />
          <Button variant="ghost" type="button" onClick={() => onChange(values.filter((_, j) => j !== i))} aria-label="Remove">
            <X className="size-4" />
          </Button>
        </div>
      ))}
      <Button variant="ghost" size="sm" type="button" onClick={() => onChange([...values, ""])}>
        + {addLabel}
      </Button>
    </div>
  );
}
