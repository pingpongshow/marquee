import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";

export type DiscoverItem = components["schemas"]["DiscoverItem"];
export type MediaRequest = components["schemas"]["MediaRequest"];
export type Availability = components["schemas"]["Availability"];

export const requestsStatusQuery = queryOptions({
  queryKey: ["requests", "status"],
  queryFn: () => unwrap(api.GET("/requests/status")),
  staleTime: 60_000,
});

export const myRequestsQuery = queryOptions({
  queryKey: ["requests", "list", "mine"],
  queryFn: () =>
    unwrap(api.GET("/requests", { params: { query: { scope: "mine" } } })),
});

export const allRequestsQuery = queryOptions({
  queryKey: ["requests", "list", "all"],
  queryFn: () =>
    unwrap(api.GET("/requests", { params: { query: { scope: "all" } } })),
});

/** Everything about requests is refreshed together after a change. */
export function useRequestAction<T>(fn: (v: T) => Promise<unknown>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["requests"] }),
  });
}

export const availabilityLabel: Record<Availability, string> = {
  none: "",
  pending: "Waiting for approval",
  requested: "Requested",
  processing: "On its way",
  partial: "Partly available",
  available: "In library",
};

export const statusLabel: Record<MediaRequest["status"], string> = {
  pending: "Waiting for approval",
  approved: "Approved",
  declined: "Declined",
  failed: "Failed",
  available: "Available",
};
