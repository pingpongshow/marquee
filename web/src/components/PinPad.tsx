import { Delete } from "lucide-react";
import { useEffect, useState } from "react";

/**
 * Four-digit PIN entry with an on-screen keypad. Calls onComplete when four digits are
 * entered; physical keyboards work too. reset clears it (e.g. after a wrong PIN).
 */
export function PinPad({ onComplete, disabled, resetKey }: { onComplete: (pin: string) => void; disabled?: boolean; resetKey?: unknown }) {
  const [pin, setPin] = useState("");
  const [seenReset, setSeenReset] = useState(resetKey);
  if (resetKey !== seenReset) {
    setSeenReset(resetKey);
    setPin("");
  }
  const press = (d: string) => {
    if (disabled) return;
    const next = (pin + d).slice(0, 4);
    setPin(next);
    if (next.length === 4) onComplete(next);
  };
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (/^\d$/.test(e.key)) press(e.key);
      else if (e.key === "Backspace") setPin((p) => p.slice(0, -1));
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });
  return (
    <div className="flex flex-col items-center gap-5">
      <div className="flex gap-3" role="status" aria-label={`${pin.length} of 4 digits entered`}>
        {[0, 1, 2, 3].map((i) => (
          <span key={i} className={i < pin.length ? "size-4 rounded-full bg-accent" : "size-4 rounded-full border-2 border-border"} />
        ))}
      </div>
      <div className="grid grid-cols-3 gap-3">
        {["1", "2", "3", "4", "5", "6", "7", "8", "9", "", "0", "⌫"].map((k) =>
          k === "" ? (
            <span key="blank" />
          ) : (
            <button
              key={k}
              type="button"
              disabled={disabled}
              onClick={() => (k === "⌫" ? setPin(pin.slice(0, -1)) : press(k))}
              className="flex size-16 items-center justify-center rounded-full bg-surface-2 text-xl font-semibold hover:bg-surface-3 disabled:opacity-50"
              aria-label={k === "⌫" ? "Delete digit" : k}
            >
              {k === "⌫" ? <Delete className="size-5" /> : k}
            </button>
          ),
        )}
      </div>
    </div>
  );
}
