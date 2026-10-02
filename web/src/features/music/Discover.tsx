import { useMutation, useQuery } from "@tanstack/react-query";
import { clsx } from "clsx";
import {
  Disc3,
  Heart,
  Library as LibraryIcon,
  Loader2,
  Play,
  Radio,
  Sparkles,
} from "lucide-react";
import { Link } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { api, imageUrl, unwrap } from "@/api/client";
import type { Station } from "@/api/types";
import { Alert } from "@/components/ui";
import { useMusic } from "../player/MusicPlayer";
import { useRadio, useMuse } from "./useRadio";

const moods = [
  "Chill",
  "Energetic",
  "Focus",
  "Melancholy",
  "Party",
  "Romantic",
  "Dreamy",
  "Aggressive",
];
const musePrompts = [
  "Rainy Sunday jazz",
  "Late-night drive synthwave",
  "Upbeat 80s pop for cleaning the house",
  "Acoustic songs for a quiet evening",
];

/** A mix's cover: the first four album covers. */
function MixArt({
  station,
  className,
}: {
  station: Station;
  className?: string;
}) {
  const covers = [
    ...new Set(
      station.items
        .map((t) => t.images?.poster)
        .filter((x): x is number => !!x),
    ),
  ].slice(0, 4);
  return (
    <div
      className={clsx(
        "grid aspect-square grid-cols-2 grid-rows-2 overflow-hidden rounded-lg bg-surface-3",
        className,
      )}
    >
      {covers.length === 4 ? (
        covers.map((c) => (
          <img
            key={c}
            src={imageUrl(c, 120)}
            alt=""
            className="size-full object-cover"
          />
        ))
      ) : (
        <div className="col-span-2 row-span-2 flex items-center justify-center bg-gradient-to-br from-accent/40 to-surface-3">
          <Disc3 className="size-10 text-text/60" aria-hidden />
        </div>
      )}
    </div>
  );
}

/** The top of a music library: Muse, stations and daily mixes (M6.5). */
export function MusicDiscover({ libraryId }: { libraryId: number }) {
  const music = useMusic();
  const radio = useRadio();
  const muse = useMuse();
  const [prompt, setPrompt] = useState("");
  const status = useQuery({
    queryKey: ["music", "status"],
    queryFn: () => unwrap(api.GET("/music/status")),
    staleTime: 60_000,
  });
  const mixes = useQuery({
    queryKey: ["music", "mixes", libraryId],
    queryFn: () =>
      unwrap(api.GET("/music/mixes", { params: { query: { libraryId } } })),
  });
  const decades = useQuery({
    queryKey: ["libraries", libraryId, "filters", "album"],
    queryFn: () =>
      unwrap(
        api.GET("/libraries/{libraryId}/filters", {
          params: { path: { libraryId }, query: { type: "album" } },
        }),
      ),
  });
  const playMix = useMutation({
    mutationFn: async (st: Station) => music.playStation(st),
  });
  const st = status.data;
  const ready = !!st?.enabled && st.analyzed > 0;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (prompt.trim().length > 1)
      muse.mutate({ prompt: prompt.trim(), libraryId });
  };
  const busy = radio.isPending || muse.isPending;
  const error = radio.error ?? muse.error;

  if (st && !st.enabled) return null;
  return (
    <section className="mb-10 space-y-6">
      {st && st.analyzed < st.total && (
        <p className="flex items-center gap-2 text-sm text-muted">
          <Sparkles className="size-4 text-accent" aria-hidden />
          {st.available
            ? `Listening to your music: ${st.analyzed.toLocaleString()} of ${st.total.toLocaleString()} tracks analysed. Radios and mixes improve as it goes.`
            : "The sonic analysis service isn't running, so radios and Muse are unavailable."}
        </p>
      )}
      {error && <Alert tone="error">{error.message}</Alert>}

      {ready && (
        <form
          onSubmit={submit}
          className="rounded-xl border border-border bg-gradient-to-br from-accent/10 via-surface to-surface p-5"
        >
          <label
            htmlFor="muse"
            className="mb-2 flex items-center gap-2 text-sm font-semibold"
          >
            <Sparkles className="size-4 text-accent" aria-hidden /> Muse
          </label>
          <div className="flex gap-2">
            <input
              id="muse"
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              placeholder="Describe what you want to hear…"
              maxLength={300}
              className="h-11 flex-1 rounded-lg border border-border bg-surface-2 px-4 placeholder:text-faint focus:border-accent focus:outline-none"
            />
            <button
              type="submit"
              disabled={busy || prompt.trim().length < 2}
              className="flex h-11 items-center gap-2 rounded-lg bg-accent px-5 font-medium text-black disabled:opacity-50"
            >
              {muse.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Play className="size-4 fill-current" />
              )}{" "}
              Play
            </button>
          </div>
          <div className="mt-3 flex flex-wrap gap-2">
            {musePrompts.map((p) => (
              <button
                key={p}
                type="button"
                onClick={() => muse.mutate({ prompt: p, libraryId })}
                disabled={busy}
                className="rounded-full bg-surface-2 px-3 py-1 text-xs text-muted hover:text-text"
              >
                {p}
              </button>
            ))}
          </div>
        </form>
      )}

      {ready && (
        <div>
          <h2 className="mb-3 text-lg font-semibold">Stations</h2>
          <div className="flex flex-wrap gap-2">
            <StationChip
              icon={<LibraryIcon className="size-4" />}
              label="Library Radio"
              disabled={busy}
              onClick={() => radio.mutate({ seed: "library", libraryId })}
            />
            <StationChip
              icon={<Heart className="size-4" />}
              label="Favourites Radio"
              disabled={busy}
              onClick={() => radio.mutate({ seed: "favourites", libraryId })}
            />
            {moods.map((m) => (
              <StationChip
                key={m}
                icon={<Radio className="size-4" />}
                label={m}
                disabled={busy}
                onClick={() =>
                  radio.mutate({
                    seed: "mood",
                    value: m.toLowerCase(),
                    libraryId,
                  })
                }
              />
            ))}
            {decades.data?.decades.slice(0, 6).map((d) => (
              <StationChip
                key={d.value}
                icon={<Radio className="size-4" />}
                label={`${d.value}s`}
                disabled={busy}
                onClick={() =>
                  radio.mutate({ seed: "decade", value: d.value, libraryId })
                }
              />
            ))}
          </div>
        </div>
      )}

      {ready && (
        <BrowseMoodsAndStyles
          libraryId={libraryId}
          genres={decades.data?.genres ?? []}
        />
      )}

      {mixes.data && mixes.data.length > 0 && (
        <div>
          <h2 className="mb-3 text-lg font-semibold">Mixes for you</h2>
          <ul className="flex gap-4 overflow-x-auto pb-2">
            {mixes.data.map((m) => (
              <li key={m.id} className="w-44 shrink-0">
                <button
                  onClick={() => playMix.mutate(m)}
                  className="group block w-full text-left"
                >
                  <div className="relative">
                    <MixArt
                      station={m}
                      className="group-hover:ring-2 group-hover:ring-accent"
                    />
                    <span className="absolute right-2 bottom-2 rounded-full bg-accent p-2 text-black opacity-0 shadow-lg transition-opacity group-hover:opacity-100">
                      <Play className="size-4 fill-current" />
                    </span>
                  </div>
                  <div className="mt-2 truncate text-sm font-medium">
                    {m.title}
                  </div>
                  <div className="line-clamp-2 text-xs text-muted">
                    {m.description}
                  </div>
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}

function StationChip({
  icon,
  label,
  onClick,
  disabled,
}: {
  icon: React.ReactNode;
  label: string;
  onClick: () => void;
  disabled?: boolean;
}) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className="flex items-center gap-2 rounded-full border border-border bg-surface px-4 py-2 text-sm hover:border-accent hover:text-accent disabled:opacity-50"
    >
      {icon}
      {label}
    </button>
  );
}

const moodColours: Record<string, string> = {
  Chill: "from-sky-600 to-sky-900",
  Energetic: "from-orange-500 to-red-700",
  Focus: "from-emerald-600 to-emerald-900",
  Melancholy: "from-indigo-600 to-slate-900",
  Party: "from-fuchsia-500 to-purple-800",
  Romantic: "from-rose-500 to-rose-900",
  Dreamy: "from-violet-500 to-indigo-900",
  Aggressive: "from-red-700 to-zinc-900",
};

/** Plexamp-style mood and style tiles (MUSIC-18); each opens a page with its radio and music. */
function BrowseMoodsAndStyles({
  libraryId,
  genres,
}: {
  libraryId: number;
  genres: { value: string; count: number }[];
}) {
  const styles = [...genres].sort((a, b) => b.count - a.count).slice(0, 18);
  const tile =
    "flex h-20 items-end rounded-lg bg-gradient-to-br p-3 text-sm font-semibold text-white shadow hover:ring-2 hover:ring-accent";
  return (
    <div className="space-y-5">
      <div>
        <h2 className="mb-3 text-lg font-semibold">Moods</h2>
        <div className="grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-3">
          {moods.map((m) => (
            <Link
              key={m}
              to="/music/$libraryId/$kind/$name"
              params={{ libraryId: String(libraryId), kind: "mood", name: m }}
              className={clsx(tile, moodColours[m])}
            >
              {m}
            </Link>
          ))}
        </div>
      </div>
      {styles.length > 0 && (
        <div>
          <h2 className="mb-3 text-lg font-semibold">Styles</h2>
          <div className="grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-3">
            {styles.map((g, i) => (
              <Link
                key={g.value}
                to="/music/$libraryId/$kind/$name"
                params={{
                  libraryId: String(libraryId),
                  kind: "style",
                  name: g.value,
                }}
                className={clsx(
                  tile,
                  [
                    "from-amber-600 to-stone-900",
                    "from-teal-600 to-slate-900",
                    "from-pink-600 to-zinc-900",
                    "from-lime-600 to-stone-900",
                    "from-cyan-600 to-slate-900",
                    "from-purple-600 to-zinc-900",
                  ][i % 6],
                )}
              >
                <span>
                  {g.value}
                  <span className="block text-xs font-normal opacity-75">
                    {g.count} albums
                  </span>
                </span>
              </Link>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
