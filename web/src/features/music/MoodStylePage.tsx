import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { Loader2, Play, Radio } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Alert, Spinner } from "@/components/ui";
import { Poster } from "../browse/Poster";
import { useMusic } from "../player/MusicPlayer";
import { formatTrackTime } from "../browse/format";
import { useRadio } from "./useRadio";

/** A mood or style (genre) of a music library (MUSIC-18): its radio, albums and a track sampler. */
export function MoodStylePage() {
  const { libraryId, kind, name } = useParams({
    from: "/music/$libraryId/$kind/$name",
  });
  const lib = Number(libraryId);
  const value = decodeURIComponent(name);
  const isMood = kind === "mood";
  const seed = isMood
    ? ({ seed: "mood", value: value.toLowerCase(), libraryId: lib } as const)
    : ({ seed: "genre", value, libraryId: lib } as const);
  const radio = useRadio();
  const music = useMusic();
  // A taste of the station: its first tracks, shown as a list (and their albums for moods).
  const sampler = useQuery({
    queryKey: ["music", "sampler", kind, value, lib],
    queryFn: () =>
      unwrap(api.POST("/music/radio", { body: { ...seed, limit: 30 } })),
    staleTime: 10 * 60_000,
  });
  const albums = useQuery({
    queryKey: ["music", "style-albums", lib, value],
    queryFn: () =>
      unwrap(
        api.GET("/libraries/{libraryId}/items", {
          params: {
            path: { libraryId: lib },
            query: { type: "album", genre: value, sort: "-rating", limit: 60 },
          },
        }),
      ),
    enabled: !isMood,
  });
  const tracks = sampler.data?.items ?? [];
  const moodAlbums = isMood
    ? [
        ...new Map(
          tracks.filter((t) => t.parentId).map((t) => [t.parentId, t]),
        ).values(),
      ].slice(0, 18)
    : [];

  return (
    <div className="space-y-8 p-4 lg:p-8">
      <header className="flex flex-wrap items-end gap-4">
        <div>
          <div className="text-xs font-semibold tracking-wider text-accent uppercase">
            {isMood ? "Mood" : "Style"}
          </div>
          <h1 className="text-3xl font-bold">{value}</h1>
        </div>
        <button
          onClick={() => radio.mutate(seed)}
          disabled={radio.isPending}
          className="ml-auto flex h-11 items-center gap-2 rounded-full bg-accent px-6 font-medium text-black disabled:opacity-50"
        >
          {radio.isPending ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            <Radio className="size-4" />
          )}{" "}
          Play {value} Radio
        </button>
      </header>
      {(radio.error ?? sampler.error) && (
        <Alert tone="error">{(radio.error ?? sampler.error)!.message}</Alert>
      )}

      {!isMood && (
        <section>
          <h2 className="mb-3 text-lg font-semibold">Albums</h2>
          {albums.isPending ? (
            <Spinner />
          ) : (
            <ul className="grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-4">
              {albums.data?.items.map((a) => (
                <li key={a.id}>
                  <Link
                    to="/item/$itemId"
                    params={{ itemId: String(a.id) }}
                    className="block space-y-1.5"
                  >
                    <Poster item={a} shape="square" width={300} />
                    <div className="truncate text-sm font-medium">
                      {a.title}
                    </div>
                    <div className="truncate text-xs text-muted">
                      {a.parentTitle ?? a.year}
                    </div>
                  </Link>
                </li>
              ))}
              {albums.data?.items.length === 0 && (
                <p className="text-muted">No albums are tagged {value}.</p>
              )}
            </ul>
          )}
        </section>
      )}

      {isMood && moodAlbums.length > 0 && (
        <section>
          <h2 className="mb-3 text-lg font-semibold">Albums with this feel</h2>
          <ul className="flex gap-4 overflow-x-auto pb-2">
            {moodAlbums.map((t) => (
              <li key={t.parentId} className="w-40 shrink-0">
                <Link
                  to="/item/$itemId"
                  params={{ itemId: String(t.parentId) }}
                  className="block space-y-1.5"
                >
                  <Poster
                    item={{
                      ...t,
                      title: t.parentTitle ?? t.title,
                      type: "album",
                    }}
                    shape="square"
                    width={300}
                  />
                  <div className="truncate text-sm font-medium">
                    {t.parentTitle}
                  </div>
                  <div className="truncate text-xs text-muted">
                    {t.artistCredit ?? t.grandparentTitle}
                  </div>
                </Link>
              </li>
            ))}
          </ul>
        </section>
      )}

      <section>
        <h2 className="mb-3 text-lg font-semibold">
          {isMood ? "Tracks" : `A taste of ${value}`}
        </h2>
        {sampler.isPending ? (
          <Spinner />
        ) : (
          <ol className="divide-y divide-border rounded-lg bg-surface">
            {tracks.map((t, i) => (
              <li key={t.id}>
                <button
                  onClick={() =>
                    music.play(tracks, i, {
                      source: `${value}${isMood ? "" : " Radio"}`,
                    })
                  }
                  className="flex w-full items-center gap-3 px-3 py-2 text-left hover:bg-surface-2"
                >
                  <span className="w-6 text-right text-xs text-faint">
                    {i + 1}
                  </span>
                  <Poster
                    item={t}
                    shape="square"
                    width={80}
                    compact
                    className="!w-10 shrink-0"
                  />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm font-medium">
                      {t.title}
                    </span>
                    <span className="block truncate text-xs text-muted">
                      {t.artistCredit ?? t.grandparentTitle} · {t.parentTitle}
                    </span>
                  </span>
                  <span className="text-xs text-faint">
                    {t.durationMs ? formatTrackTime(t.durationMs) : ""}
                  </span>
                  <Play className="size-4 text-muted" aria-hidden />
                </button>
              </li>
            ))}
          </ol>
        )}
      </section>
    </div>
  );
}
