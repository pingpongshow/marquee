import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";

export type LiveChannel = components["schemas"]["LiveChannel"];
export type LiveProgramme = components["schemas"]["LiveProgramme"];

export const liveStatusQuery = queryOptions({
  queryKey: ["livetv", "status"],
  queryFn: () => unwrap(api.GET("/livetv/status")),
  staleTime: 60_000,
});

export const liveGroupsQuery = queryOptions({
  queryKey: ["livetv", "groups"],
  queryFn: () => unwrap(api.GET("/livetv/groups")),
  staleTime: 5 * 60_000,
});

/** "all", "favorites" or a group name. */
export type LiveFilter = string;

function filterParams(f: LiveFilter) {
  if (f === "favorites") return { favorites: true };
  if (f === "all") return {};
  return { group: f };
}

export const liveChannelsQuery = (f: LiveFilter) =>
  queryOptions({
    queryKey: ["livetv", "channels", f],
    queryFn: () =>
      unwrap(
        api.GET("/livetv/channels", { params: { query: filterParams(f) } }),
      ),
    refetchInterval: 60_000,
  });

export const liveGuideQuery = (f: LiveFilter, start: Date, end: Date) =>
  queryOptions({
    queryKey: ["livetv", "guide", f, start.toISOString(), end.toISOString()],
    queryFn: () =>
      unwrap(
        api.GET("/livetv/guide", {
          params: {
            query: {
              ...filterParams(f),
              start: start.toISOString(),
              end: end.toISOString(),
            },
          },
        }),
      ),
    staleTime: 60_000,
  });

export function useFavorite() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, on }: { id: number; on: boolean }) =>
      on
        ? unwrap(
            api.PUT("/livetv/channels/{channelId}/favorite", {
              params: { path: { channelId: id } },
            }),
          )
        : unwrap(
            api.DELETE("/livetv/channels/{channelId}/favorite", {
              params: { path: { channelId: id } },
            }),
          ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["livetv"] }),
  });
}

export function minutesLeft(p: LiveProgramme, now = Date.now()) {
  return Math.max(0, Math.round((new Date(p.end).getTime() - now) / 60_000));
}

export function timeLabel(d: Date | string) {
  return new Date(d).toLocaleTimeString([], {
    hour: "numeric",
    minute: "2-digit",
  });
}

// DVR (LIVE-5).
export type Recording = components["schemas"]["Recording"];
export type RecordingRule = components["schemas"]["RecordingRule"];

export const recordingsQuery = queryOptions({
  queryKey: ["livetv", "recordings"],
  queryFn: () => unwrap(api.GET("/livetv/recordings")),
  refetchInterval: 30_000,
});

export const recordingRulesQuery = queryOptions({
  queryKey: ["livetv", "rules"],
  queryFn: () => unwrap(api.GET("/livetv/recording-rules")),
});

export function useSchedule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: {
      channelId: number;
      start: string;
      series?: boolean;
      anyChannel?: boolean;
    }) => unwrap(api.POST("/livetv/recordings", { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["livetv"] }),
  });
}

export function useCancelRecording() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, deleteFile }: { id: number; deleteFile?: boolean }) =>
      unwrap(
        api.DELETE("/livetv/recordings/{recordingId}", {
          params: { path: { recordingId: id }, query: { deleteFile } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["livetv"] }),
  });
}

export function useDeleteRule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) =>
      unwrap(
        api.DELETE("/livetv/recording-rules/{ruleId}", {
          params: { path: { ruleId: id } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["livetv"] }),
  });
}

export function formatSize(bytes?: number) {
  if (!bytes) return "";
  const gb = bytes / 1e9;
  return gb >= 1 ? `${gb.toFixed(1)} GB` : `${Math.round(bytes / 1e6)} MB`;
}
