import { clsx } from "clsx";
import {
  useCallback,
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
  open: controlled,
  onOpenChange,
  floating,
}: {
  label: string;
  trigger: ReactNode;
  children: ReactNode;
  align?: "left" | "right";
  className?: string;
  /** Optional control from outside, e.g. to open it on right-click. */
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  /** Position the list against the viewport, so a scrolling row (overflow) can't clip it. */
  floating?: boolean;
}) {
  const [own, setOwn] = useState(false);
  const open = controlled ?? own;
  const changeRef = useRef(onOpenChange);
  useEffect(() => {
    changeRef.current = onOpenChange;
  });
  const setOpen = useCallback((next: boolean) => {
    setOwn(next);
    changeRef.current?.(next);
  }, []);
  const [side, setSide] = useState(align);
  const [shift, setShift] = useState(0);
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null);
  const ref = useRef<HTMLDivElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  // Open on the other side when the preferred one would run off the screen; when neither fits,
  // nudge it back on screen. Measured once per open from the anchor, so it can't oscillate.
  useLayoutEffect(() => {
    if (!open) {
      setSide(align);
      setShift(0);
      setPos(null);
      return;
    }
    const a = ref.current?.getBoundingClientRect();
    const w = menu.current?.offsetWidth;
    if (!a || !w) return;
    const vw = window.innerWidth;
    if (floating) {
      const h = menu.current?.offsetHeight ?? 0;
      const vh = window.innerHeight;
      const left = Math.min(Math.max(align === "right" ? a.right - w : a.left, 8), Math.max(8, vw - 8 - w));
      // Below the trigger, or above it when there isn't room.
      const top = a.bottom + 4 + h <= vh - 8 ? a.bottom + 4 : Math.max(8, a.top - 4 - h);
      setPos({ top, left });
      return;
    }
    const leftOf = (s: "left" | "right") =>
      s === "right" ? a.right - w : a.left;
    const fits = (s: "left" | "right") =>
      leftOf(s) >= 8 && leftOf(s) + w <= vw - 8;
    const other = align === "right" ? "left" : "right";
    const chosen = fits(align) || !fits(other) ? align : other;
    const left = leftOf(chosen);
    setSide(chosen);
    setShift(Math.min(Math.max(left, 8), Math.max(8, vw - 8 - w)) - left);
  }, [open, align, floating]);
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) =>
      !ref.current?.contains(e.target as Node) && setOpen(false);
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    // A floating list would drift from its trigger when the page or row scrolls.
    const onMove = () => floating && setOpen(false);
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    window.addEventListener("scroll", onMove, true);
    window.addEventListener("resize", onMove);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
      window.removeEventListener("scroll", onMove, true);
      window.removeEventListener("resize", onMove);
    };
  }, [open, setOpen, floating]);
  return (
    <div className={clsx("relative", className)} ref={ref}>
      <button
        type="button"
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          setOpen(!open);
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
            "z-40 w-56 max-w-[calc(100vw-16px)] overflow-hidden rounded-lg border border-border bg-surface py-1 text-text shadow-2xl",
            floating ? "fixed" : ["absolute mt-1", side === "right" ? "right-0" : "left-0"],
          )}
          style={
            floating
              ? // Hidden for the first layout pass, until it's measured and placed.
                { top: pos?.top ?? 0, left: pos?.left ?? 0, visibility: pos ? undefined : "hidden" }
              : shift
                ? { transform: `translateX(${shift}px)` }
                : undefined
          }
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
