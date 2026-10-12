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

export const vodMoviesQuery = (category: string, q: string, sort: VodSort) =>
  infiniteQueryOptions({
    queryKey: ["vod", "movies", category, q, sort],
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
