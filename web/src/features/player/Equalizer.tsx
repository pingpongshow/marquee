import { clsx } from "clsx";
import { X } from "lucide-react";
import { useEffect } from "react";
import { EQ_BANDS, EQ_MAX_DB, EQ_PRESETS } from "./eqDsp";
import { useMusicState } from "./MusicPlayer";

const bandLabel = (hz: number) => (hz >= 1000 ? `${hz / 1000}k` : String(hz));

/** The music equaliser: on/off, presets and ten bands of ±12 dB, kept in this browser. */
export function EqualizerSheet({ onClose }: { onClose: () => void }) {
  const m = useMusicState();
  const { eq, setEq } = m;
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopImmediatePropagation();
        onClose();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [onClose]);

  return (
    <div
      className="absolute inset-0 z-10 flex items-end justify-center bg-black/50 sm:items-center"
      onClick={(e) => e.target === e.currentTarget && onClose()}
    >
      <div
        role="dialog"
        aria-label="Equaliser"
        className="w-full max-w-xl rounded-t-2xl border border-white/15 bg-neutral-900/95 p-5 text-white shadow-2xl backdrop-blur sm:rounded-2xl"
      >
        <div className="mb-4 flex items-center gap-3">
          <h2 className="flex-1 text-lg font-semibold">Equaliser</h2>
          <button
            type="button"
            role="switch"
            aria-checked={eq.enabled}
            aria-label="Equaliser on"
            disabled={!m.eqSupported}
            onClick={() => setEq({ ...eq, enabled: !eq.enabled })}
            className={clsx(
              "relative h-6 w-11 shrink-0 rounded-full transition-colors disabled:opacity-40",
              eq.enabled ? "bg-accent" : "bg-white/20",
            )}
          >
            <span
              className={clsx(
                "absolute top-0.5 left-0.5 size-5 rounded-full bg-white shadow transition-transform",
                eq.enabled && "translate-x-5",
              )}
            />
          </button>
          <button
            onClick={onClose}
            className="rounded-full p-1.5 hover:bg-white/10"
            aria-label="Close equaliser"
          >
            <X className="size-5" />
          </button>
        </div>
        {!m.eqSupported && (
          <p className="mb-3 text-sm text-white/70">
            This browser can't adjust the sound, so the equaliser is
            unavailable.
          </p>
        )}
        <div className="mb-4 flex items-center gap-3">
          <select
            aria-label="Equaliser preset"
            value={eq.preset in EQ_PRESETS ? eq.preset : "Custom"}
            onChange={(e) =>
              EQ_PRESETS[e.target.value] &&
              setEq({
                enabled: true,
                preset: e.target.value,
                gains: EQ_PRESETS[e.target.value]!,
              })
            }
            className="flex-1 rounded-md border border-white/20 bg-black/40 px-2 py-1.5 text-sm"
          >
            {Object.keys(EQ_PRESETS).map((p) => (
              <option key={p} value={p}>
                {p}
              </option>
            ))}
            <option value="Custom" disabled>
              Custom
            </option>
          </select>
          <button
            onClick={() =>
              setEq({ ...eq, preset: "Flat", gains: EQ_PRESETS.Flat! })
            }
            className="rounded-md px-3 py-1.5 text-sm text-white/80 hover:bg-white/10"
          >
            Reset
          </button>
        </div>
        <div
          className={clsx(
            "flex justify-between gap-1",
            !eq.enabled && "opacity-50",
          )}
        >
          {EQ_BANDS.map((hz, i) => (
            <div key={hz} className="flex flex-col items-center gap-1">
              <span className="text-[10px] text-white/60 tabular-nums">
                {eq.gains[i]! > 0 ? "+" : ""}
                {eq.gains[i]}
              </span>
              <input
                type="range"
                min={-EQ_MAX_DB}
                max={EQ_MAX_DB}
                step={0.5}
                value={eq.gains[i]}
                aria-label={`${bandLabel(hz)}Hz`}
                aria-valuetext={`${eq.gains[i]} dB`}
                onChange={(e) =>
                  setEq({
                    enabled: true,
                    preset: "Custom",
                    gains: eq.gains.map((g, j) =>
                      j === i ? Number(e.target.value) : g,
                    ),
                  })
                }
                className="h-36 w-6 accent-[var(--color-accent)]"
                style={{ writingMode: "vertical-lr", direction: "rtl" }}
              />
              <span className="text-[10px] text-white/60">{bandLabel(hz)}</span>
            </div>
          ))}
        </div>
        <p className="mt-3 text-xs text-white/50">
          Saved in this browser. ±{EQ_MAX_DB} dB per band.
        </p>
      </div>
    </div>
  );
}
