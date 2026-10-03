import { useMutation, useQuery } from "@tanstack/react-query";
import { clsx } from "clsx";
import {
  Disc3,
  Heart,
  ListPlus,
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
import { useMusicActions } from "../player/MusicPlayer";
import { useRadio, useMuse } from "./useRadio";
import { SaveAsPlaylistDialog } from "../playlists/SaveAsPlaylist";

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

/** Soundprint status for the music home: whether radios, Muse and mixes can run. */
export function useMusicStatus() {
  const status = useQuery({
    queryKey: ["music", "status"],
    queryFn: () => unwrap(api.GET("/music/status")),
    staleTime: 60_000,
  });
  const st = status.data;
  return { status: st, ready: !!st?.enabled && st.analyzed > 0 };
}

/** The library's album genres and decades, for style tiles and decade stations. */
export function useAlbumFacets(libraryId: number) {
  return useQuery({
    queryKey: ["libraries", libraryId, "filters", "album"],
    queryFn: () =>
      unwrap(
        api.GET("/libraries/{libraryId}/filters", {
          params: { path: { libraryId }, query: { type: "album" } },
        }),
      ),
  });
}

/** "Listening to your music: n of m tracks analysed" while Soundprint catches up. */
export function AnalysisNote() {
  const { status: st } = useMusicStatus();
  if (!st?.enabled || st.analyzed >= st.total) return null;
  return (
    <p className="flex items-center gap-2 text-sm text-muted">
      <Sparkles className="size-4 text-accent" aria-hidden />
      {st.available
        ? `Listening to your music: ${st.analyzed.toLocaleString()} of ${st.total.toLocaleString()} tracks analysed. Radios and mixes improve as it goes.`
        : "The Soundprint analysis service isn't running, so radios and Muse are unavailable."}
    </p>
  );
}

/** Muse (MUSIC-5): describe a mood or moment and it plays; the result saves as a playlist. */
export function MusePanel({ libraryId }: { libraryId: number }) {
  const muse = useMuse();
  const { ready } = useMusicStatus();
  const [prompt, setPrompt] = useState("");
  const [saving, setSaving] = useState(false);
  if (!ready) return null;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (prompt.trim().length > 1)
      muse.mutate({ prompt: prompt.trim(), libraryId });
  };
  return (
    <>
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
            placeholder="Describe a mood or moment…"
            maxLength={300}
            className="h-11 min-w-0 flex-1 rounded-lg border border-border bg-surface-2 px-4 placeholder:text-faint focus:border-accent focus:outline-none"
          />
          <button
            type="submit"
            disabled={muse.isPending || prompt.trim().length < 2}
            className="flex h-11 shrink-0 items-center gap-2 rounded-lg bg-accent px-4 font-medium text-black disabled:opacity-50 sm:px-5"
          >
            {muse.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Play className="size-4 fill-current" />
            )}{" "}
            Play
          </button>
        </div>
        {muse.error && <div className="mt-3"><Alert tone="error">{muse.error.message}</Alert></div>}
        {muse.isSuccess && muse.data.items.length > 0 && (
          <div className="mt-3 flex flex-wrap items-center gap-3 text-sm" data-testid="muse-playing">
            <span className="min-w-0 truncate text-muted">
              Playing <strong className="text-text">{muse.data.title}</strong> · {muse.data.items.length} tracks
            </span>
            <button
              type="button"
              onClick={() => setSaving(true)}
              className="flex items-center gap-1.5 rounded-full bg-surface-2 px-3 py-1 text-xs font-medium hover:bg-surface-3"
            >
              <ListPlus className="size-4" /> Save as playlist
            </button>
          </div>
        )}
        <div className="mt-3 flex flex-wrap gap-2">
          {musePrompts.map((p) => (
            <button
              key={p}
              type="button"
              onClick={() => muse.mutate({ prompt: p, libraryId })}
              disabled={muse.isPending}
              className="rounded-full bg-surface-2 px-3 py-1 text-xs text-muted hover:text-text"
            >
              {p}
            </button>
          ))}
        </div>
      </form>
      {saving && muse.data && (
        <SaveAsPlaylistDialog defaultTitle={muse.data.title} itemIds={muse.data.items.map((i) => i.id)} onClose={() => setSaving(false)} />
      )}
    </>
  );
}

/** Station chips: library, favourites, moods and decades (MUSIC-3). */
export function StationChips({ libraryId }: { libraryId: number }) {
  const radio = useRadio();
  const { ready } = useMusicStatus();
  const decades = useAlbumFacets(libraryId);
  if (!ready) return null;
  const busy = radio.isPending;
  return (
    <div className="space-y-3">
      {radio.error && <Alert tone="error">{radio.error.message}</Alert>}
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
              radio.mutate({ seed: "mood", value: m.toLowerCase(), libraryId })
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
  );
}

/** Daily mixes as cards for a shelf (MUSIC-4); empty until there are mixes. */
export function useMixes(libraryId: number) {
  return useQuery({
    queryKey: ["music", "mixes", libraryId],
    queryFn: () =>
      unwrap(api.GET("/music/mixes", { params: { query: { libraryId } } })),
  });
}

export function MixCard({ mix }: { mix: Station }) {
  const music = useMusicActions();
  const playMix = useMutation({
    mutationFn: async (st: Station) => music.playStation(st),
  });
  return (
    <li className="w-44 shrink-0">
      <button
        onClick={() => playMix.mutate(mix)}
        className="group block w-full text-left"
      >
        <div className="relative">
          <MixArt
            station={mix}
            className="group-hover:ring-2 group-hover:ring-accent"
          />
          <span className="absolute right-2 bottom-2 rounded-full bg-accent p-2 text-black opacity-0 shadow-lg transition-opacity group-hover:opacity-100">
            <Play className="size-4 fill-current" />
          </span>
        </div>
        <div className="mt-2 truncate text-sm font-medium">{mix.title}</div>
        <div className="line-clamp-2 text-xs text-muted">{mix.description}</div>
      </button>
    </li>
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

/** Mood and style tiles (MUSIC-18); each opens a page with its radio and music. */
export function BrowseMoodsAndStyles({
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
