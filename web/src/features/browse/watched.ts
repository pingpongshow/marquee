import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api, unwrap } from "@/api/client";
import type { ItemSummary } from "@/api/types";

type Watchable = Pick<ItemSummary, "id" | "type"> & Partial<Pick<ItemSummary, "viewCount" | "viewOffsetMs" | "leafCount" | "watchedLeafCount">>;

const leafTypes = new Set(["movie", "episode", "video"]);
const parentTypes = new Set(["show", "season"]);
/** Types the watched toggle applies to. */
export const markableTypes = new Set([...leafTypes, ...parentTypes]);

/**
 * Whether an item counts as watched: a played leaf that isn't part-way through a rewatch (as the
 * poster tick shows it), or a show or season with every episode played.
 */
export function isWatched(item: Watchable) {
  if (parentTypes.has(item.type)) return (item.leafCount ?? 0) > 0 && item.watchedLeafCount === item.leafCount;
  return (item.viewCount ?? 0) > 0 && !item.viewOffsetMs;
}

type Obj = Record<string, unknown>;
const isObj = (v: unknown): v is Obj => typeof v === "object" && v !== null;
// An item summary or detail: has an id, a video type and an availability flag (libraries and playlists don't).
const isVideoItem = (o: Obj) => typeof o.id === "number" && typeof o.available === "boolean" && markableTypes.has(o.type as string);

function patchItem(o: Obj, watched: boolean): Obj {
  if (parentTypes.has(o.type as string)) return { ...o, watchedLeafCount: watched ? (o.leafCount ?? 0) : 0 };
  const viewCount = (o.viewCount as number | undefined) ?? 0;
  return { ...o, viewCount: watched ? Math.max(1, viewCount) : 0, viewOffsetMs: 0 };
}

/**
 * Returns `data` with the item marked watched or unwatched wherever it appears, along with
 * its seasons and episodes (an item whose parent or grandparent is the target). Untouched
 * branches keep their identity so React Query sees only real changes.
 */
export function applyWatched<T>(data: T, targetId: number, watched: boolean): T {
  const walk = (v: unknown): unknown => {
    if (Array.isArray(v)) {
      let changed = false;
      const out = v.map((x) => {
        const y = walk(x);
        if (y !== x) changed = true;
        return y;
      });
      return changed ? out : v;
    }
    if (!isObj(v)) return v;
    let out: Obj = v;
    for (const [k, x] of Object.entries(v)) {
      const y = walk(x);
      if (y !== x) {
        if (out === v) out = { ...v };
        out[k] = y;
      }
    }
    if (isVideoItem(v) && (v.id === targetId || v.parentId === targetId || v.grandparentId === targetId)) out = patchItem(out, watched);
    return out;
  };
  return walk(data) as T;
}

type Hub = { id: string; items: ItemSummary[]; entries?: { item?: ItemSummary }[] };

/** Takes an item (or, for a show, its episodes) out of Continue Watching. */
export function withoutContinueWatching(hubs: Hub[] | undefined, targetId: number): Hub[] | undefined {
  if (!hubs) return hubs;
  const keep = (it: ItemSummary) => it.id !== targetId && it.grandparentId !== targetId && it.parentId !== targetId;
  return hubs.map((h) =>
    h.id === "continue-watching"
      ? {
          ...h,
          items: h.items.filter(keep),
          // The row's entries (library and on demand): library ones go the same way.
          entries: h.entries?.filter((e) => !e.item || keep(e.item)),
        }
      : h,
  );
}

export const hubsKey = ["items", "hubs"] as const;

/** Refreshes everything that shows watch state: item pages, Home rows, library grids, playlists, search. */
export function refreshWatchState(qc: QueryClient) {
  for (const key of [["items"], ["libraries"], ["playlists"], ["search"], ["continue-watching"]]) void qc.invalidateQueries({ queryKey: key });
}

/**
 * Marks an item watched or unwatched (a show or season marks all its episodes).
 * The change shows at once everywhere the item is cached, then the server's state is fetched again.
 */
export function useMarkWatched() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ item, watched }: { item: Pick<ItemSummary, "id" | "type">; watched: boolean }) =>
      watched
        ? unwrap(api.POST("/items/{itemId}/watched", { params: { path: { itemId: item.id } } }))
        : unwrap(api.DELETE("/items/{itemId}/watched", { params: { path: { itemId: item.id } } })),
    onMutate: async ({ item, watched }) => {
      const cache = qc.getQueryCache();
      const snapshot = cache.getAll().map((q) => [q.queryKey, q.state.data] as const);
      for (const q of cache.getAll()) {
        if (q.state.data === undefined) continue;
        const next = applyWatched(q.state.data, item.id, watched);
        if (next !== q.state.data) qc.setQueryData(q.queryKey, next);
      }
      if (watched) qc.setQueryData<Hub[]>(hubsKey, (old) => withoutContinueWatching(old, item.id));
      return { snapshot };
    },
    onError: (_e, _v, ctx) => {
      for (const [key, data] of ctx?.snapshot ?? []) qc.setQueryData(key, data);
    },
    onSettled: () => refreshWatchState(qc),
  });
}

/** Hides an item (or an episode's show) from Continue Watching until it's watched again; the position is kept. */
export function useRemoveFromContinueWatching() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (itemId: number) => unwrap(api.DELETE("/hubs/continue-watching/{itemId}", { params: { path: { itemId } } })),
    onMutate: (itemId) => {
      const old = qc.getQueryData<Hub[]>(hubsKey);
      qc.setQueryData<Hub[]>(hubsKey, (h) =>
        h?.map((x) =>
          x.id === "continue-watching"
            ? { ...x, items: x.items.filter((it) => it.id !== itemId), entries: x.entries?.filter((e) => e.item?.id !== itemId) }
            : x,
        ),
      );
      return { old };
    },
    onError: (_e, _v, ctx) => qc.setQueryData(hubsKey, ctx?.old),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: hubsKey });
      qc.invalidateQueries({ queryKey: ["continue-watching"] });
    },
  });
}
