import { useQuery } from "@tanstack/react-query";
import { RotateCcw, X } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { imageUrl } from "@/api/client";
import { itemQuery } from "@/api/queries";
import type { ItemDetail, ItemSummary } from "@/api/types";

/** How long the countdown runs before the next episode starts. */
export const UP_NEXT_SECONDS = 10;

const episodeLine = (it: Pick<ItemSummary, "type" | "index" | "parentTitle">) =>
  it.type === "episode" ? `${it.parentTitle ?? "Season"} - Episode ${it.index ?? ""}`.toUpperCase() : "";

/**
 * Up Next (PLAY-5): shown once an episode finishes. The one that just played on the left
 * (select it to watch again), the next one with a countdown in the middle, its details on
 * the right. The next one plays when the countdown runs out, or when it's selected.
 */
export function UpNextScreen({
  previous,
  next,
  onPlay,
  onReplay,
  onClose,
  held,
  children,
}: {
  previous: ItemDetail;
  next: ItemSummary;
  onPlay: () => void;
  onReplay: () => void;
  onClose: () => void;
  /** The countdown waits while the person answers the rating card. */
  held?: boolean;
  /** Shown under the details (the rating card). */
  children?: ReactNode;
}) {
  const detail = useQuery(itemQuery(next.id));
  const [left, setLeft] = useState(UP_NEXT_SECONDS);
  const nextRef = useRef<HTMLButtonElement>(null);
  const play = useRef(onPlay);
  useEffect(() => {
    play.current = onPlay;
  });

  useEffect(() => nextRef.current?.focus(), []);
  useEffect(() => {
    if (held) return;
    if (left <= 0) {
      play.current();
      return;
    }
    const t = window.setTimeout(() => setLeft((s) => s - 1), 1000);
    return () => window.clearTimeout(t);
  }, [left, held]);
  // Ahead of the player's own shortcuts: Escape leaves, the rest do nothing here.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
      } else if (e.key === " " || e.key.startsWith("Arrow") || e.key === "f" || e.key === "m") {
        if (e.key === "ArrowLeft" || e.key === "ArrowRight") e.preventDefault();
        e.stopPropagation();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [onClose]);

  const nd = detail.data;
  const background = nd?.images?.backdrop ?? next.images?.backdrop ?? previous.images?.backdrop;
  const show = next.type === "episode" ? (next.grandparentTitle ?? next.parentTitle) : next.title;
  const r = 44;
  const circumference = 2 * Math.PI * r;

  return (
    <div className="absolute inset-0 z-30 flex flex-col overflow-y-auto bg-black text-white" role="dialog" aria-label="Up next">
      {background && (
        <img src={imageUrl(background, 1280)} alt="" className="absolute inset-0 size-full scale-105 object-cover opacity-40 blur-sm" />
      )}
      <div className="absolute inset-0 bg-gradient-to-t from-black via-black/70 to-black/40" />
      <button
        onClick={onClose}
        className="absolute top-4 right-4 z-10 rounded-full p-2 text-white/80 hover:bg-white/10 hover:text-white"
        aria-label="Close"
      >
        <X className="size-6" />
      </button>

      <div className="relative m-auto flex w-full max-w-6xl flex-col items-center gap-8 px-6 py-16 lg:flex-row lg:items-end">
        {/* Previous */}
        <div className="hidden w-56 shrink-0 sm:block">
          <div className="mb-2 text-sm font-medium text-white/70">Previous</div>
          <button
            onClick={onReplay}
            className="group block w-full overflow-hidden rounded-lg bg-white/5 text-left shadow-lg ring-white/80 transition hover:ring-2 focus-visible:ring-2 focus-visible:outline-none"
            aria-label={`Watch ${previous.title} again`}
          >
            <span className="relative block aspect-video bg-white/10">
              {(previous.images?.thumb ?? previous.images?.backdrop) && (
                <img
                  src={imageUrl((previous.images?.thumb ?? previous.images?.backdrop)!, 240)}
                  alt=""
                  className="size-full object-cover opacity-70 transition group-hover:opacity-90"
                />
              )}
              <span className="absolute inset-0 grid place-items-center">
                <span className="grid size-11 place-items-center rounded-full bg-black/60 backdrop-blur-sm">
                  <RotateCcw className="size-5" />
                </span>
              </span>
            </span>
            <span className="block space-y-0.5 p-2.5">
              <span className="block truncate text-sm font-semibold">
                {previous.type === "episode" ? previous.grandparentTitle : previous.title}
              </span>
              {previous.type === "episode" && <span className="block truncate text-xs text-white/70">{previous.title}</span>}
              <span className="block truncate text-[11px] tracking-wide text-white/50">{episodeLine(previous)}</span>
            </span>
          </button>
        </div>

        {/* Playing next */}
        <div className="w-full max-w-md shrink-0">
          <div className="mb-2 text-sm font-medium text-white/70">{held ? "Up next" : "Playing next"}</div>
          <button
            ref={nextRef}
            onClick={onPlay}
            className="group relative block aspect-video w-full overflow-hidden rounded-xl bg-white/10 shadow-2xl ring-accent transition hover:ring-4 focus-visible:ring-4 focus-visible:outline-none"
            aria-label={`Play ${next.title} now`}
          >
            {(next.images?.thumb ?? next.images?.backdrop) && (
              <img src={imageUrl((next.images?.thumb ?? next.images?.backdrop)!, 480)} alt="" className="size-full object-cover" />
            )}
            <span className="absolute inset-0 grid place-items-center bg-black/25 transition group-hover:bg-black/10">
              <span className="relative grid size-24 place-items-center">
                <svg viewBox="0 0 100 100" className="absolute inset-0 -rotate-90" aria-hidden>
                  <circle cx="50" cy="50" r={r + 4} className="fill-black/55" />
                  <circle cx="50" cy="50" r={r} fill="none" strokeWidth="5" className="stroke-white/20" />
                  <circle
                    cx="50"
                    cy="50"
                    r={r}
                    fill="none"
                    strokeWidth="5"
                    strokeLinecap="round"
                    className="stroke-accent transition-[stroke-dashoffset] duration-1000 ease-linear"
                    strokeDasharray={circumference}
                    strokeDashoffset={circumference * (1 - left / UP_NEXT_SECONDS)}
                  />
                </svg>
                <span className="relative text-4xl font-semibold tabular-nums">{held ? "II" : left}</span>
              </span>
            </span>
          </button>
        </div>

        {/* Details */}
        <div className="w-full max-w-md min-w-0 space-y-2 lg:max-w-sm lg:self-center">
          <h2 className="text-2xl font-bold sm:text-3xl">{show}</h2>
          {next.type === "episode" && (
            <>
              <div className="text-lg text-white/85">{next.title}</div>
              <div className="text-xs font-medium tracking-wider text-white/60">{episodeLine(next)}</div>
            </>
          )}
          {nd?.summary && <p className="line-clamp-4 text-sm leading-relaxed text-white/75">{nd.summary}</p>}
          <div className="flex gap-2 pt-2">
            <button onClick={onPlay} className="rounded-md bg-accent px-4 py-2 text-sm font-semibold text-black hover:brightness-110">
              Play now
            </button>
            <button onClick={onClose} className="rounded-md bg-white/10 px-4 py-2 text-sm font-medium hover:bg-white/20">
              Cancel
            </button>
          </div>
          {children && <div className="pt-3">{children}</div>}
        </div>
      </div>
    </div>
  );
}
