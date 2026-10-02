import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "./client";
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
export const libraryItemsQuery = (libraryId, sort, offset, limit) => queryOptions({
    queryKey: ["libraries", libraryId, "items", sort, offset, limit],
    queryFn: () => unwrap(api.GET("/libraries/{libraryId}/items", { params: { path: { libraryId }, query: { sort, offset, limit } } })),
});
export const itemQuery = (itemId) => queryOptions({
    queryKey: ["items", itemId],
    queryFn: () => unwrap(api.GET("/items/{itemId}", { params: { path: { itemId } } })),
});
export const itemChildrenQuery = (itemId) => queryOptions({
    queryKey: ["items", itemId, "children"],
    queryFn: () => unwrap(api.GET("/items/{itemId}/children", { params: { path: { itemId }, query: { limit: 500 } } })),
});
export const browseQuery = (path) => queryOptions({
    queryKey: ["browse", path],
    queryFn: () => unwrap(api.GET("/filesystem/browse", { params: { query: path ? { path } : {} } })),
});
export function useUpdateSettings() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (body) => unwrap(api.PATCH("/settings", { body })),
        onSuccess: (data) => {
            qc.setQueryData(settingsQuery.queryKey, data);
            qc.invalidateQueries({ queryKey: systemInfoQuery.queryKey });
        },
    });
}
export function useCreateLibrary() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (body) => unwrap(api.POST("/libraries", { body })),
        onSuccess: () => qc.invalidateQueries({ queryKey: librariesQuery.queryKey }),
    });
}
export function useUpdateLibrary() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, body }) => unwrap(api.PATCH("/libraries/{libraryId}", { params: { path: { libraryId: id } }, body })),
        onSuccess: () => qc.invalidateQueries({ queryKey: librariesQuery.queryKey }),
    });
}
export function useScanLibrary() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id) => unwrap(api.POST("/libraries/{libraryId}/scan", { params: { path: { libraryId: id } } })),
        onSuccess: () => qc.invalidateQueries({ queryKey: librariesQuery.queryKey }),
    });
}
export function useCancelScan() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id) => unwrap(api.DELETE("/libraries/{libraryId}/scan", { params: { path: { libraryId: id } } })),
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
        mutationFn: (id) => unwrap(api.DELETE("/libraries/{libraryId}", { params: { path: { libraryId: id } } })),
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
export const searchQuery = (q, limit = 10) => queryOptions({
    queryKey: ["search", q, limit],
    queryFn: () => unwrap(api.GET("/search", { params: { query: { q, limit } } })),
    enabled: q.trim().length > 0,
    staleTime: 30_000,
});
export function useCreateUser() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (body) => unwrap(api.POST("/users", { body })),
        onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
    });
}
export function useUpdateUser() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, body }) => unwrap(api.PATCH("/users/{userId}", { params: { path: { userId: id } }, body })),
        onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
    });
}
export function useDeleteUser() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id) => unwrap(api.DELETE("/users/{userId}", { params: { path: { userId: id } } })),
        onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
    });
}
export function useUpdateMe() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (body) => unwrap(api.PATCH("/me", { body })),
        onSuccess: (me) => qc.setQueryData(meQuery.queryKey, me),
    });
}
export function useRevokeDevice() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id) => unwrap(api.DELETE("/devices/{deviceId}", { params: { path: { deviceId: id } } })),
        onSuccess: () => qc.invalidateQueries({ queryKey: ["devices"] }),
    });
}
