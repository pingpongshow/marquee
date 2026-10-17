import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";

// Commercials in Live TV recordings (admins): found with Comskip, and cut out on request or
// when Settings → Live TV says so. The original stays in the media trash for a while.

export type CommercialScan = components["schemas"]["CommercialScan"];

function clock(ms: number) {
  const t = Math.round(ms / 1000);
  const h = Math.floor(t / 3600);
  const m = Math.floor((t % 3600) / 60);
  const s = String(t % 60).padStart(2, "0");
  return h ? `${h}:${String(m).padStart(2, "0")}:${s}` : `${m}:${s}`;
}

/** One line on where a recording's commercials stand. */
export function commercialsText(c: CommercialScan): string {
  const n = c.breaks.length;
  const total = c.breaks.reduce((t, b) => t + (b.endMs - b.startMs), 0);
  switch (c.status) {
    case "pending":
      return "Commercials: not checked yet";
    case "queued":
      return "Commercials: queued";
    case "running":
      return "Commercials: working on it…";
    case "detected":
      return n
        ? `Commercials: found ${n} ${n === 1 ? "break" : "breaks"} (${clock(total)})`
        : "Commercials: none found";
    case "waiting":
      return "Commercials: will be removed when nobody's watching";
    case "cut":
      return `Commercials removed (${clock(c.removedMs ?? total)})`;
    case "restored":
      return "Commercials: original restored";
    case "failed":
      return `Commercials: failed. ${c.error ?? ""}`.trim();
  }
}

/** Removing is possible: Comskip is there and the recording isn't cut or busy. */
export function canRemove(c: CommercialScan) {
  return !!c.available && !["cut", "queued", "running", "waiting"].includes(c.status);
}

const busy = (c?: CommercialScan) =>
  !!c && ["queued", "running", "waiting"].includes(c.status);

/** A recording's commercials; 404s (not a recording) come back as an error. */
export function useCommercials(itemId: number, enabled: boolean) {
  return useQuery({
    queryKey: ["commercials", itemId],
    queryFn: () =>
      unwrap(
        api.GET("/items/{itemId}/commercials", {
          params: { path: { itemId } },
        }),
      ),
    enabled,
    retry: false,
    refetchInterval: (q) => (busy(q.state.data) ? 3000 : false),
  });
}

function useDone(itemId: number, onDone?: (c: CommercialScan) => void) {
  const qc = useQueryClient();
  return (c: CommercialScan) => {
    onDone?.(c);
    qc.setQueryData(["commercials", itemId], c);
    qc.invalidateQueries({ queryKey: ["livetv", "recordings"] });
    qc.invalidateQueries({ queryKey: ["items", itemId] });
  };
}

export function useRemoveCommercials(itemId: number, onDone?: (c: CommercialScan) => void) {
  const done = useDone(itemId, onDone);
  return useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/items/{itemId}/commercials/remove", {
          params: { path: { itemId } },
        }),
      ),
    onSuccess: done,
  });
}

export function useRestoreCommercials(itemId: number, onDone?: (c: CommercialScan) => void) {
  const done = useDone(itemId, onDone);
  return useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/items/{itemId}/commercials/restore", {
          params: { path: { itemId } },
        }),
      ),
    onSuccess: done,
  });
}

export const removeConfirm =
  "Cut the commercials out of this recording? The original goes to the media trash, where it can be restored from for 30 days.";
export const restoreConfirm =
  "Put the original recording back, with its commercials? The cut version goes to the media trash.";
