import { clsx } from "clsx";
import { Check, HardDriveDownload, Loader2 } from "lucide-react";
import { useState } from "react";
import { toast } from "@/components/Toast";
import { saveActive, useSaveVod } from "../livetv/vodApi";
import { saveLabel, toastResult, useIsAdmin } from "../livetv/VodSaves";
import type { DiscoverItem } from "./api";

/**
 * Download from VOD: queues the same title in the VOD download tool (admins and people allowed
 * VOD downloads, for sources that can save), into its default folder: a film, or a show's episodes the library doesn't have.
 * A film already on its way or saved shows that instead.
 */
export function DownloadFromVod({
  item,
  size = "md",
}: {
  item: DiscoverItem;
  size?: "xs" | "sm" | "md";
}) {
  const v = item.vod;
  const save = useSaveVod();
  const admin = useIsAdmin();
  const [queued, setQueued] = useState<string | null>(null);
  if (!v?.canSave) return null;
  const state = v.save?.state;
  // A film's save on its way or finished (a failed one can be tried again).
  const status =
    queued ?? (state && state !== "failed" ? saveLabel(v.save) : null);
  const busy = save.isPending || saveActive(state);
  const cls = clsx(
    "inline-flex items-center gap-1.5 bg-surface-3 font-medium text-text hover:bg-border disabled:opacity-70 disabled:hover:bg-surface-3",
    size === "xs"
      ? "justify-center rounded px-2 py-0.5 text-[11px]"
      : size === "sm"
        ? "rounded-md px-2 py-1 text-xs"
        : "rounded-md px-4 py-2 text-sm",
  );
  const icon = size === "xs" ? "size-3" : "size-3.5";
  const run = () =>
    save.mutate(v.kind === "movie" ? { movieId: v.id } : { seriesId: v.id }, {
      onSuccess: (r) => {
        toastResult(r, admin);
        setQueued(
          r.queued ? "Queued" : r.already ? "Already queued" : "In library",
        );
      },
      onError: (e) => toast(e.message, { tone: "error" }),
    });
  const what =
    v.kind === "movie" ? "this film" : "the episodes the library doesn't have";
  return (
    <button
      type="button"
      className={cls}
      onClick={run}
      disabled={!!status || save.isPending}
      title={
        status
          ? `${item.title}: ${status}`
          : `Queue ${what} in the VOD download tool`
      }
      aria-label={
        status ? `${item.title}: ${status}` : `Download ${item.title} from VOD`
      }
    >
      {busy ? (
        <Loader2 className={clsx(icon, "animate-spin")} aria-hidden />
      ) : status ? (
        <Check className={icon} aria-hidden />
      ) : (
        <HardDriveDownload className={icon} aria-hidden />
      )}
      {status ?? (size === "md" ? "Download from VOD" : "Download")}
    </button>
  );
}
