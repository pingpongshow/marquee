import {
  infiniteQueryOptions,
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";

// Movies on demand under Live TV (LIVE-6).

export type VodMovie = components["schemas"]["VodMovie"];
export type VodMovieDetail = components["schemas"]["VodMovieDetail"];
export type VodSort = "title" | "added" | "year" | "rating";
export type VodSeries = components["schemas"]["VodSeries"];
export type VodSeriesDetail = components["schemas"]["VodSeriesDetail"];
export type VodEpisode = components["schemas"]["VodEpisode"];

/** List filters shared by films and shows (remembered per page). */
export interface VodFilters {
  genre?: string;
  /** A decade's first year, e.g. 1980. */
  decade?: number;
  minRating?: number;
  watched?: "unwatched" | "inProgress" | "watched";
  sourceId?: string;
  /** Only titles the library already has ("yes") or doesn't ("no"). */
  inLibrary?: "yes" | "no";
}

function filterQuery(f: VodFilters) {
  return {
    genre: f.genre || undefined,
    yearFrom: f.decade || undefined,
    yearTo: f.decade ? f.decade + 9 : undefined,
    minRating: f.minRating || undefined,
    watched: f.watched || undefined,
    sourceId: f.sourceId || undefined,
    inLibrary: f.inLibrary ? f.inLibrary === "yes" : undefined,
  };
}

export const vodStatusQuery = queryOptions({
  queryKey: ["vod", "status"],
  queryFn: () => unwrap(api.GET("/livetv/vod/status")),
  staleTime: 60_000,
  // Like Live TV, shown only while usable; that can change while the tab is in the background.
  refetchOnWindowFocus: "always",
  refetchOnReconnect: "always",
});

export const vodCategoriesQuery = queryOptions({
  queryKey: ["vod", "categories"],
  queryFn: () => unwrap(api.GET("/livetv/vod/categories")),
  staleTime: 5 * 60_000,
});

export const vodContinueQuery = queryOptions({
  queryKey: ["vod", "continue"],
  queryFn: () => unwrap(api.GET("/livetv/vod/continue")),
});

const PAGE = 120;

export const vodGenresQuery = queryOptions({
  queryKey: ["vod", "genres"],
  queryFn: () => unwrap(api.GET("/livetv/vod/genres")),
  staleTime: 5 * 60_000,
});

export const vodMoviesQuery = (category: string, q: string, sort: VodSort, filters: VodFilters = {}) =>
  infiniteQueryOptions({
    queryKey: ["vod", "movies", category, q, sort, filters],
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET("/livetv/vod/movies", {
          params: {
            query: {
              category: category || undefined,
              q: q || undefined,
              sort,
              limit: PAGE,
              offset: pageParam,
              ...filterQuery(filters),
            },
          },
        }),
      ),
    initialPageParam: 0,
    getNextPageParam: (last, pages) => {
      const loaded = pages.reduce((n, p) => n + p.movies.length, 0);
      return loaded < last.total ? loaded : undefined;
    },
    staleTime: 60_000,
  });

export const vodMovieQuery = (id: number) =>
  queryOptions({
    queryKey: ["vod", "movie", id],
    queryFn: () =>
      unwrap(
        api.GET("/livetv/vod/movies/{movieId}", {
          params: { path: { movieId: id } },
        }),
      ),
  });

export function useVodWatched(id: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (watched: boolean) => {
      const params = { params: { path: { movieId: id } } };
      return watched
        ? unwrap(api.PUT("/livetv/vod/movies/{movieId}/watched", params))
        : unwrap(api.DELETE("/livetv/vod/movies/{movieId}/watched", params));
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ["vod"] }),
  });
}

// ---- shows (series from Xtream Codes sources)

export const vodSeriesCategoriesQuery = queryOptions({
  queryKey: ["vod", "series-categories"],
  queryFn: () => unwrap(api.GET("/livetv/vod/series/categories")),
  staleTime: 5 * 60_000,
});

export const vodSeriesGenresQuery = queryOptions({
  queryKey: ["vod", "series-genres"],
  queryFn: () => unwrap(api.GET("/livetv/vod/series/genres")),
  staleTime: 5 * 60_000,
});

export const vodSeriesContinueQuery = queryOptions({
  queryKey: ["vod", "series-continue"],
  queryFn: () => unwrap(api.GET("/livetv/vod/series/continue")),
});

export const vodSeriesListQuery = (category: string, q: string, sort: VodSort, filters: VodFilters = {}) =>
  infiniteQueryOptions({
    queryKey: ["vod", "series", category, q, sort, filters],
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET("/livetv/vod/series", {
          params: {
            query: {
              category: category || undefined,
              q: q || undefined,
              sort,
              limit: PAGE,
              offset: pageParam,
              ...filterQuery(filters),
            },
          },
        }),
      ),
    initialPageParam: 0,
    getNextPageParam: (last, pages) => {
      const loaded = pages.reduce((n, p) => n + p.series.length, 0);
      return loaded < last.total ? loaded : undefined;
    },
    staleTime: 60_000,
  });

export const vodSeriesQuery = (id: number) =>
  queryOptions({
    queryKey: ["vod", "show", id],
    queryFn: () => unwrap(api.GET("/livetv/vod/series/{seriesId}", { params: { path: { seriesId: id } } })),
  });

/** Marks a show (or one season) watched or unwatched. */
export function useVodSeriesWatched(id: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ watched, season }: { watched: boolean; season?: number }) => {
      const params = { params: { path: { seriesId: id }, query: { season } } };
      return watched
        ? unwrap(api.PUT("/livetv/vod/series/{seriesId}/watched", params))
        : unwrap(api.DELETE("/livetv/vod/series/{seriesId}/watched", params));
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ["vod"] }),
  });
}

export function useVodEpisodeWatched() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, watched }: { id: number; watched: boolean }) => {
      const params = { params: { path: { episodeId: id } } };
      return watched
        ? unwrap(api.PUT("/livetv/vod/episodes/{episodeId}/watched", params))
        : unwrap(api.DELETE("/livetv/vod/episodes/{episodeId}/watched", params));
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ["vod"] }),
  });
}

// ---- saving into the library (admins only)

export type VodSave = components["schemas"]["VodSave"];
export type VodSaveState = components["schemas"]["VodSaveState"];
export type VodSaveStateName = components["schemas"]["VodSaveStateName"];
export type VodLibraryMatch = components["schemas"]["VodLibraryMatch"];
export type VodLibraryRollup = components["schemas"]["VodLibraryRollup"];
export type VodSaveDestinationRef = components["schemas"]["VodSaveDestinationRef"];

/** States in which a save is still on its way (the page polls while one is). */
export const saveActive = (s?: VodSaveStateName) =>
  s === "queued" || s === "downloading" || s === "processing" || s === "verifying";

export const vodSavesQuery = queryOptions({
  queryKey: ["vod", "saves"],
  queryFn: () => unwrap(api.GET("/livetv/vod/saves")),
  refetchInterval: (q) => (q.state.data?.active ? 2000 : 15_000),
});

/**
 * Queues a film, an episode, a season or a whole show. upgrade replaces the library's copy
 * of a film or episode that's better on demand; a season's or show's includeUpgrades also
 * queues its episodes that are better than the library's.
 */
export function useSaveVod() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (
      what:
        | { movieId: number; upgrade?: boolean; destination?: string }
        | { episodeId: number; upgrade?: boolean; destination?: string }
        | { seriesId: number; season?: number; includeUpgrades?: boolean; destination?: string },
    ) => {
      const destination = what.destination || undefined;
      if ("movieId" in what)
        return unwrap(
          api.POST("/livetv/vod/movies/{movieId}/save", {
            params: { path: { movieId: what.movieId }, query: { upgrade: what.upgrade || undefined, destination } },
          }),
        );
      if ("episodeId" in what)
        return unwrap(
          api.POST("/livetv/vod/episodes/{episodeId}/save", {
            params: { path: { episodeId: what.episodeId }, query: { upgrade: what.upgrade || undefined, destination } },
          }),
        );
      return unwrap(
        api.POST("/livetv/vod/series/{seriesId}/save", {
          params: {
            path: { seriesId: what.seriesId },
            query: { season: what.season, includeUpgrades: what.includeUpgrades || undefined, destination },
          },
        }),
      );
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ["vod"] }),
  });
}

type SaveAction = "cancel" | "retry" | "top" | "pause" | "resume";

/** One action on a save, or (id omitted) pausing, resuming or clearing the whole queue. */
export function useSaveAction() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, action }: { id?: number; action: SaveAction | "clear" }) => {
      if (id == null) {
        if (action === "clear") return unwrap(api.POST("/livetv/vod/saves/clear"));
        if (action === "pause") return unwrap(api.POST("/livetv/vod/saves/pause"));
        return unwrap(api.POST("/livetv/vod/saves/resume"));
      }
      const params = { params: { path: { saveId: id } } };
      switch (action) {
        case "cancel":
          return unwrap(api.DELETE("/livetv/vod/saves/{saveId}", params));
        case "retry":
          return unwrap(api.POST("/livetv/vod/saves/{saveId}/retry", params));
        case "top":
          return unwrap(api.POST("/livetv/vod/saves/{saveId}/top", params));
        case "pause":
          return unwrap(api.POST("/livetv/vod/saves/{saveId}/pause", params));
        default:
          return unwrap(api.POST("/livetv/vod/saves/{saveId}/resume", params));
      }
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ["vod"] }),
  });
}
