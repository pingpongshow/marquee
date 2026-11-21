import { Delete } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

const KEYS = ["1", "2", "3", "4", "5", "6", "7", "8", "9", "", "0", "⌫"];

/**
 * Four-digit PIN entry: a compact keypad, the keyboard's digits and Backspace, and a pasted
 * PIN. Calls onComplete as soon as the fourth digit is in. Each press counts at once: digits
 * are kept in a ref, so quick taps never overwrite each other, and the keypad answers on
 * pointer down (no wait for the click, nor the touch delay). resetKey clears it (after a wrong
 * PIN); error shows under the dots, which shake.
 */
export function PinPad({
  onComplete,
  disabled,
  resetKey,
  error,
}: {
  onComplete: (pin: string) => void;
  disabled?: boolean;
  resetKey?: unknown;
  error?: string | null;
}) {
  // The digits belong to one resetKey: a new key (a wrong PIN) starts them again.
  const [entered, setEntered] = useState({ key: resetKey, pin: "" });
  const pin = Object.is(entered.key, resetKey) ? entered.pin : "";
  const pinRef = useRef(entered);
  const rootRef = useRef<HTMLDivElement>(null);
  const live = useRef({ onComplete, disabled, resetKey });
  useEffect(() => {
    live.current = { onComplete, disabled, resetKey };
  });

  const current = () => (Object.is(pinRef.current.key, live.current.resetKey) ? pinRef.current.pin : "");
  const setPin = useCallback((next: string) => {
    pinRef.current = { key: live.current.resetKey, pin: next };
    setEntered(pinRef.current);
  }, []);

  const add = useCallback(
    (digits: string) => {
      const now = current();
      if (live.current.disabled || now.length >= 4) return;
      const next = (now + digits.replace(/\D/g, "")).slice(0, 4);
      if (next === now) return;
      setPin(next);
      if (next.length === 4) live.current.onComplete(next);
    },
    [setPin],
  );
  const back = useCallback(() => {
    if (!live.current.disabled) setPin(current().slice(0, -1));
  }, [setPin]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return;
      if (/^\d$/.test(e.key)) {
        e.preventDefault();
        add(e.key);
      } else if (e.key === "Backspace") {
        e.preventDefault();
        back();
      }
    };
    const onPaste = (e: ClipboardEvent) => {
      const text = e.clipboardData?.getData("text") ?? "";
      if (/\d/.test(text)) {
        e.preventDefault();
        add(text);
      }
    };
    window.addEventListener("keydown", onKey);
    window.addEventListener("paste", onPaste);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("paste", onPaste);
    };
  }, [add, back]);

  // Typing works straight away: nothing else on the page keeps the focus.
  useEffect(() => {
    const active = document.activeElement;
    if (!active || active === document.body || !rootRef.current?.contains(active)) rootRef.current?.focus({ preventScroll: true });
  }, []);

  return (
    <div ref={rootRef} tabIndex={-1} className="flex flex-col items-center gap-3 outline-none" data-testid="pin-pad">
      <div
        // A new key after a wrong PIN draws the dots afresh, so they shake each time.
        key={String(resetKey)}
        className={error ? "pin-shake flex gap-3" : "flex gap-3"}
        role="status"
        aria-label={`${pin.length} of 4 digits entered`}
      >
        {[0, 1, 2, 3].map((i) => (
          <span
            key={i}
            className={
              i < pin.length
                ? "size-3 rounded-full bg-accent"
                : error
                  ? "size-3 rounded-full border-2 border-danger"
                  : "size-3 rounded-full border-2 border-border"
            }
          />
        ))}
      </div>
      <p className="min-h-5 text-center text-sm text-danger" aria-live="assertive">
        {error ?? ""}
      </p>
      <div className="grid grid-cols-3 gap-2">
        {KEYS.map((k) =>
          k === "" ? (
            <span key="blank" />
          ) : (
            <button
              key={k}
              type="button"
              disabled={disabled}
              // Pointer down answers at once; a keyboard's Enter or Space on the focused key
              // arrives as a click with no pointer (detail 0).
              onPointerDown={(e) => {
                if (e.button !== 0) return;
                e.preventDefault();
                if (k === "⌫") back();
                else add(k);
              }}
              onClick={(e) => {
                if (e.detail !== 0) return;
                if (k === "⌫") back();
                else add(k);
              }}
              className="flex h-11 w-14 touch-manipulation select-none items-center justify-center rounded-lg bg-surface-2 text-lg font-semibold hover:bg-surface-3 active:bg-surface-3 disabled:opacity-50"
              aria-label={k === "⌫" ? "Delete digit" : k}
            >
              {k === "⌫" ? <Delete className="size-4" /> : k}
            </button>
          ),
        )}
      </div>
    </div>
  );
}
