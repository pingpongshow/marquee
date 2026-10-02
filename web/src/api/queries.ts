import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "./client";
import type { LibraryCreate, LibraryUpdate, MeUpdate, ServerSettingsUpdate, UserCreate, UserUpdate } from "./types";

export const systemInfoQuery = queryOptions({
  queryKey: ["system", "info"],
  queryFn: () => unwrap(api.GET("/system/info")),
  staleTime: 30_000,
});

export const meQuery = queryOptions({
  queryKey: ["me"],
  queryFn: () => unwrap(api.GET("/me")),
  staleTime: 60_000,
});

export const settingsQuery = queryOptions({
  queryKey: ["settings"],
  queryFn: () => unwrap(api.GET("/settings")),
});

export const librariesQuery = queryOptions({
  queryKey: ["libraries"],
  queryFn: () => unwrap(api.GET("/libraries")),
  // Poll while any scan is queued or running so progress stays live.
  refetchInterval: (q) => (q.state.data?.some((l) => l.scanStatus !== "idle") ? 1500 : false),
});

export type ItemSort = "title" | "-title" | "added" | "-added" | "year" | "-year" | "released" | "-released";

export const libraryItemsQuery = (libraryId: number, sort: ItemSort, offset: number, limit: number) =>
  queryOptions({
    queryKey: ["libraries", libraryId, "items", sort, offset, limit],
    queryFn: () => unwrap(api.GET("/libraries/{libraryId}/items", { params: { path: { libraryId }, query: { sort, offset, limit } } })),
  });

export const itemQuery = (itemId: number) =>
  queryOptions({
    queryKey: ["items", itemId],
    queryFn: () => unwrap(api.GET("/items/{itemId}", { params: { path: { itemId } } })),
  });

export const itemChildrenQuery = (itemId: number) =>
  queryOptions({
    queryKey: ["items", itemId, "children"],
    queryFn: () => unwrap(api.GET("/items/{itemId}/children", { params: { path: { itemId }, query: { limit: 500 } } })),
  });

export const browseQuery = (path: string | null) =>
  queryOptions({
    queryKey: ["browse", path],
    queryFn: () => unwrap(api.GET("/filesystem/browse", { params: { query: path ? { path } : {} } })),
  });

export function useUpdateSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: ServerSettingsUpdate) => unwrap(api.PATCH("/settings", { body })),
    onSuccess: (data) => {
      qc.setQueryData(settingsQuery.queryKey, data);
      qc.invalidateQueries({ queryKey: systemInfoQuery.queryKey });
    },
  });
}

export function useCreateLibrary() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: LibraryCreate) => unwrap(api.POST("/libraries", { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: librariesQuery.queryKey }),
  });
}

export function useUpdateLibrary() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id: number; body: LibraryUpdate }) =>
      unwrap(api.PATCH("/libraries/{libraryId}", { params: { path: { libraryId: id } }, body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: librariesQuery.queryKey }),
  });
}

export function useScanLibrary() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => unwrap(api.POST("/libraries/{libraryId}/scan", { params: { path: { libraryId: id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: librariesQuery.queryKey }),
  });
}

export function useCancelScan() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE("/libraries/{libraryId}/scan", { params: { path: { libraryId: id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: librariesQuery.queryKey }),
  });
}

export function useScanAll() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => unwrap(api.POST("/libraries/scan")),
    onSuccess: () => qc.invalidateQueries({ queryKey: librariesQuery.queryKey }),
  });
}

export function useDeleteLibrary() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE("/libraries/{libraryId}", { params: { path: { libraryId: id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: librariesQuery.queryKey }),
  });
}

export const usersQuery = queryOptions({
  queryKey: ["users"],
  queryFn: () => unwrap(api.GET("/users")),
});

export const profilesQuery = queryOptions({
  queryKey: ["profiles"],
  queryFn: () => unwrap(api.GET("/profiles")),
});

export const devicesQuery = queryOptions({
  queryKey: ["devices"],
  queryFn: () => unwrap(api.GET("/devices")),
});

export const searchQuery = (q: string, limit = 10) =>
  queryOptions({
    queryKey: ["search", q, limit],
    queryFn: () => unwrap(api.GET("/search", { params: { query: { q, limit } } })),
    enabled: q.trim().length > 0,
    staleTime: 30_000,
  });

export function useCreateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: UserCreate) => unwrap(api.POST("/users", { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });
}

export function useUpdateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id: number; body: UserUpdate }) => unwrap(api.PATCH("/users/{userId}", { params: { path: { userId: id } }, body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });
}

export function useDeleteUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE("/users/{userId}", { params: { path: { userId: id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });
}

export function useUpdateMe() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: MeUpdate) => unwrap(api.PATCH("/me", { body })),
    onSuccess: (me) => qc.setQueryData(meQuery.queryKey, me),
  });
}

export function useRevokeDevice() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE("/devices/{deviceId}", { params: { path: { deviceId: id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["devices"] }),
  });
}

export const playlistsQuery = (kind?: "video" | "audio") =>
  queryOptions({
    queryKey: ["playlists", "list", kind ?? "all"],
    queryFn: () => unwrap(api.GET("/playlists", { params: { query: kind ? { kind } : {} } })),
  });

export const playlistQuery = (id: number) =>
  queryOptions({
    queryKey: ["playlists", id],
    queryFn: () => unwrap(api.GET("/playlists/{playlistId}", { params: { path: { playlistId: id } } })),
  });

export const playlistItemsQuery = (id: number) =>
  queryOptions({
    queryKey: ["playlists", id, "items"],
    queryFn: () => unwrap(api.GET("/playlists/{playlistId}/items", { params: { path: { playlistId: id }, query: { limit: 2000 } } })),
  });

/** Playable items under any item (itself for a movie, episode or track). */
export function fetchLeaves(itemId: number, opts?: { shuffle?: boolean; unwatched?: boolean }) {
  return unwrap(api.GET("/items/{itemId}/leaves", { params: { path: { itemId }, query: opts ?? {} } }));
}
