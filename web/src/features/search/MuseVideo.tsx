import { useMutation, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ListPlus, Loader2, Sparkles } from "lucide-react";
import { useState, type FormEvent } from "react";
import { api, unwrap } from "@/api/client";
import { librariesQuery } from "@/api/queries";
import { Alert, Button } from "@/components/ui";
import { toast } from "@/components/Toast";
import { Poster } from "../browse/Poster";
import { subtitleFor } from "../browse/format";
import { vodStatusQuery } from "../livetv/vodApi";
import { VodSuggestionTile } from "../livetv/VodSuggestions";

const includeVodKey = "marquee.muse.includeVod";

function readIncludeVod() {
  try {
    return localStorage.getItem(includeVodKey) === "1";
  } catch {
    return false;
  }
}

export const museVideoPrompts = ["90s sci-fi with time travel", "feel-good comedy under 90 minutes", "acclaimed war films I haven't seen", "family animation"];

/** Muse for movies and shows (USER-15): describe what you'd like to watch and get matches from your libraries. */
export function MuseVideo({ libraryId, initialPrompt = "" }: { libraryId?: number; initialPrompt?: string }) {
  const [prompt, setPrompt] = useState(initialPrompt);
  const libraries = useQuery(librariesQuery);
  const library = libraries.data?.find((l) => l.id === libraryId);
  // Include VOD: films and series on demand that fit, beside the library's (not within one library).
  const vodStatus = useQuery(vodStatusQuery);
  const canVod = !!vodStatus.data?.enabled && !libraryId;
  const [includeVod, setIncludeVod] = useState(readIncludeVod);
  const toggleVod = (on: boolean) => {
    setIncludeVod(on);
    try {
      localStorage.setItem(includeVodKey, on ? "1" : "0");
    } catch {
      // private mode: just this visit
    }
  };
  const muse = useMutation({
    mutationFn: (p: string) => unwrap(api.POST("/muse/video", { body: { prompt: p, libraryId, includeVod: canVod && includeVod } })),
  });
  const save = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/playlists", {
          body: { title: `Muse: ${muse.variables ?? ""}`.slice(0, 200), kind: "video", itemIds: (muse.data?.items ?? []).map((i) => i.id) },
        }),
      ),
    onSuccess: (pl) => toast(`Saved “${pl.title}”.`, { link: { label: "Open playlist", to: "/playlist/$playlistId", params: { playlistId: String(pl.id) } } }),
  });
  const ask = (p: string) => {
    const t = p.trim();
    if (t.length < 2) return;
    setPrompt(t);
    save.reset();
    muse.mutate(t);
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    ask(prompt);
  };
  const r = muse.data;
  return (
    <div className="space-y-6">
      <form onSubmit={submit} className="max-w-3xl rounded-xl border border-border bg-gradient-to-br from-accent/10 via-surface to-surface p-5">
        <label htmlFor="muse-video" className="mb-2 flex items-center gap-2 text-sm font-semibold">
          <Sparkles className="size-4 text-accent" aria-hidden /> Muse{library ? ` · ${library.name}` : ""}
        </label>
        <div className="flex gap-2">
          <input
            id="muse-video"
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder="Describe what you want to watch…"
            maxLength={300}
            autoFocus
            className="h-11 min-w-0 flex-1 rounded-lg border border-border bg-surface-2 px-4 placeholder:text-faint focus:border-accent focus:outline-none"
          />
          <button
            type="submit"
            disabled={muse.isPending || prompt.trim().length < 2}
            className="flex h-11 shrink-0 items-center gap-2 rounded-lg bg-accent px-4 font-medium text-black disabled:opacity-50 sm:px-5"
          >
            {muse.isPending ? <Loader2 className="size-4 animate-spin" /> : <Sparkles className="size-4" />} Find
          </button>
        </div>
        {canVod && (
          <label className="mt-3 flex w-fit items-center gap-2 text-sm text-muted">
            <input
              type="checkbox"
              className="size-4 accent-[var(--color-accent)]"
              checked={includeVod}
              onChange={(e) => toggleVod(e.target.checked)}
            />
            Include VOD
          </label>
        )}
        <div className="mt-3 flex flex-wrap gap-2">
          {museVideoPrompts.map((p) => (
            <button key={p} type="button" onClick={() => ask(p)} disabled={muse.isPending} className="rounded-full bg-surface-2 px-3 py-1 text-xs text-muted hover:text-text">
              {p}
            </button>
          ))}
        </div>
      </form>
      {muse.isError && <Alert tone="error">{muse.error.message}</Alert>}
      {r && (
        <section aria-label="Muse results">
          <div className="mb-4 flex flex-wrap items-center gap-3">
            <p className="text-sm text-muted" data-testid="muse-understood">
              <Sparkles className="mr-1.5 inline size-4 text-accent" aria-hidden />
              {r.understood}
            </p>
            {r.items.length > 0 && (
              <Button size="sm" className="ml-auto" loading={save.isPending} onClick={() => save.mutate()}>
                <ListPlus className="size-4" /> Save as playlist
              </Button>
            )}
          </div>
          {r.analysed !== undefined && r.analysed < 1 && <p className="mb-4 text-sm text-faint">Muse is still learning your library ({Math.floor(r.analysed * 100)}%).</p>}
          {save.isError && (
            <div className="mb-4">
              <Alert tone="error">{save.error.message}</Alert>
            </div>
          )}
          {r.items.length === 0 ? (
            <p className="text-muted">
              Nothing in your libraries matches that.{r.vod?.length ? "" : " Try describing it differently."}
            </p>
          ) : (
            <ul className="grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-x-4 gap-y-6">
              {r.items.map((it) => (
                <li key={it.id}>
                  <Link to="/item/$itemId" params={{ itemId: String(it.id) }} className="group block">
                    <Poster item={it} className="group-hover:ring-2 group-hover:ring-accent" />
                    <div className="mt-2 truncate text-sm font-medium">{it.title}</div>
                    <div className="truncate text-xs text-muted">{subtitleFor(it)}</div>
                  </Link>
                </li>
              ))}
            </ul>
          )}
          {r.vod && r.vod.length > 0 && (
            <div className="mt-8">
              <h2 className="mb-3 text-lg font-semibold">On VOD</h2>
              <ul className="grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-x-4 gap-y-6">
                {r.vod.map((s) => (
                  <VodSuggestionTile key={s.key} s={s} className="min-w-0" />
                ))}
              </ul>
            </div>
          )}
        </section>
      )}
    </div>
  );
}

/** The sparkle button that opens Muse (Home and video library pages). */
export function MuseButton({ libraryId }: { libraryId?: number }) {
  return (
    <Link
      to="/search"
      search={{ q: "", mode: "muse", lib: libraryId }}
      className="inline-flex h-8 items-center gap-2 rounded-md bg-surface-3 px-3 text-sm font-medium text-text hover:bg-border"
      title="Describe what you want to watch"
    >
      <Sparkles className="size-4 text-accent" aria-hidden /> Muse
    </Link>
  );
}
