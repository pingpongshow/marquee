import { clsx } from "clsx";
import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";

/** A dropdown menu anchored to its trigger button. Items close the menu when clicked. */
export function Menu({
  label,
  trigger,
  children,
  align = "right",
  className,
}: {
  label: string;
  trigger: ReactNode;
  children: ReactNode;
  align?: "left" | "right";
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  const [side, setSide] = useState(align);
  const ref = useRef<HTMLDivElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  // Open on the other side when the preferred one would run off the screen.
  useLayoutEffect(() => {
    if (!open) return setSide(align);
    const r = menu.current?.getBoundingClientRect();
    if (!r) return;
    if (r.left < 8 && side === "right") setSide("left");
    else if (r.right > window.innerWidth - 8 && side === "left")
      setSide("right");
  }, [open, side, align]);
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) =>
      !ref.current?.contains(e.target as Node) && setOpen(false);
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);
  return (
    <div className={clsx("relative", className)} ref={ref}>
      <button
        type="button"
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          setOpen((v) => !v);
        }}
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        className="rounded-full p-1.5 text-muted hover:bg-surface-3 hover:text-text"
      >
        {trigger}
      </button>
      {open && (
        <div
          role="menu"
          ref={menu}
          className={clsx(
            "absolute z-40 mt-1 w-56 max-w-[calc(100vw-16px)] overflow-hidden rounded-lg border border-border bg-surface py-1 text-text shadow-2xl",
            side === "right" ? "right-0" : "left-0",
          )}
          onClick={(e) => {
            e.stopPropagation();
            setOpen(false);
          }}
        >
          {children}
        </div>
      )}
    </div>
  );
}

export function MenuItem({
  icon,
  children,
  onClick,
  disabled,
}: {
  icon?: ReactNode;
  children: ReactNode;
  onClick: () => void;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      role="menuitem"
      disabled={disabled}
      onClick={(e) => {
        e.preventDefault();
        onClick();
      }}
      className="flex w-full items-center gap-3 px-4 py-2 text-left text-sm hover:bg-surface-2 disabled:opacity-50 [&>svg]:size-4 [&>svg]:shrink-0"
    >
      {icon}
      {children}
    </button>
  );
}

export function MenuDivider() {
  return <div className="my-1 border-t border-border" role="separator" />;
}
