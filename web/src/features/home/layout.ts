import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";

export type HomeLayoutRow = components["schemas"]["HomeLayoutRow"];

/** The person's Home rows, every available one, in order (USER-12). */
export const homeLayoutQuery = queryOptions({
  queryKey: ["home-layout"],
  queryFn: () => unwrap(api.GET("/me/home-layout")),
});

/** Saves the order and hidden rows; an empty list resets Home to the default. */
export function saveHomeLayout(rows: { id: string; hidden?: boolean }[]) {
  return unwrap(api.PUT("/me/home-layout", { body: { rows: rows.map(({ id, hidden }) => ({ id, hidden: !!hidden })) } }));
}

/** Pins a collection or playlist to Home (or unpins it): read the layout, add or remove the row, save. */
export function usePinToHome(kind: "collection" | "playlist", id: number) {
  const qc = useQueryClient();
  const rowId = `${kind}-${id}`;
  const layout = useQuery(homeLayoutQuery);
  const pinned = !!layout.data?.rows.some((r) => r.id === rowId);
  const toggle = useMutation({
    mutationFn: async (pin: boolean) => {
      const current = await unwrap(api.GET("/me/home-layout"));
      const rows = current.rows.filter((r) => r.id !== rowId);
      // New pins go near the top: right after Continue Watching when that leads.
      if (pin) rows.splice(rows[0]?.id === "continue-watching" ? 1 : 0, 0, { id: rowId, hidden: false });
      return saveHomeLayout(rows);
    },
    onSuccess: (saved) => {
      qc.setQueryData(homeLayoutQuery.queryKey, saved);
      qc.invalidateQueries({ queryKey: ["items", "hubs"] });
    },
  });
  return { pinned, ready: layout.isSuccess, toggle };
}

/** Where a pinned Home row leads: its collection or playlist. */
export function pinnedTarget(hubId: string): { to: "/item/$itemId"; params: { itemId: string } } | { to: "/playlist/$playlistId"; params: { playlistId: string } } | null {
  const m = /^(collection|playlist)-(\d+)$/.exec(hubId);
  if (!m) return null;
  return m[1] === "collection" ? { to: "/item/$itemId", params: { itemId: m[2]! } } : { to: "/playlist/$playlistId", params: { playlistId: m[2]! } };
}
