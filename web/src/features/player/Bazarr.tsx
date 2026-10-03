import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Download, Ear, Search } from "lucide-react";
import { useEffect, useState } from "react";
import { api, unwrap } from "@/api/client";
import type { BazarrCandidate } from "@/api/types";
import { Alert, Button, Dialog, Select, Spinner } from "@/components/ui";
import { toast } from "@/components/Toast";

/** Common subtitle languages, as Bazarr's ISO 639-1 codes. */
const bazarrLanguages: [string, string][] = [
  ["en", "English"],
  ["es", "Spanish"],
  ["fr", "French"],
  ["de", "German"],
  ["it", "Italian"],
  ["pt", "Portuguese"],
  ["nl", "Dutch"],
  ["sv", "Swedish"],
  ["no", "Norwegian"],
  ["da", "Danish"],
  ["fi", "Finnish"],
  ["pl", "Polish"],
  ["ru", "Russian"],
  ["uk", "Ukrainian"],
  ["el", "Greek"],
  ["tr", "Turkish"],
  ["ar", "Arabic"],
  ["he", "Hebrew"],
  ["hi", "Hindi"],
  ["ja", "Japanese"],
  ["ko", "Korean"],
  ["zh", "Chinese"],
];

export const bazarrStatusQuery = (itemId: number) => ({
  queryKey: ["bazarr", itemId],
  queryFn: () => unwrap(api.GET("/items/{itemId}/subtitles/bazarr", { params: { path: { itemId } } })),
  retry: false,
  staleTime: 10_000,
});

/**
 * After Bazarr accepts a download (202) the subtitle arrives a little later: say so, then
 * refresh the item's tracks (and Bazarr's view) after ~20 s and again after ~40 s (META-12).
 */
function useAfterRequested(itemId: number, onRequested?: () => void) {
  const qc = useQueryClient();
  return () => {
    toast("Bazarr is fetching the subtitle. It'll appear in a moment.");
    onRequested?.();
    for (const ms of [20_000, 40_000])
      window.setTimeout(() => {
        void qc.invalidateQueries({ queryKey: ["items", itemId] });
        void qc.invalidateQueries({ queryKey: ["bazarr", itemId] });
      }, ms);
  };
}

/** Seconds since a search started, ticking while it runs. */
function useElapsed(active: boolean) {
  const [t0, setT0] = useState(0);
  const [now, setNow] = useState(0);
  useEffect(() => {
    if (!active) return;
    const t = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(t);
  }, [active]);
  return { start: () => (setT0(Date.now()), setNow(Date.now())), seconds: Math.max(0, Math.round((now - t0) / 1000)) };
}

/**
 * Bazarr's subtitles for a movie or episode (META-12): what it has, what the language profile
 * still wants, a manual search across its providers, and a download in any language.
 * Renders nothing unless Bazarr is set up and manages this item.
 */
export function BazarrSection({ itemId, onRequested }: { itemId: number; onRequested?: () => void }) {
  const status = useQuery(bazarrStatusQuery(itemId));
  const requested = useAfterRequested(itemId, onRequested);
  const [other, setOther] = useState("en");
  const [searching, setSearching] = useState(false);
  const timer = useElapsed(searching);
  const download = useMutation({
    mutationFn: (body: { language: string; forced?: boolean; hi?: boolean; key: string }) =>
      unwrap(api.POST("/items/{itemId}/subtitles/bazarr", { params: { path: { itemId } }, body: { language: body.language, forced: body.forced, hi: body.hi } })),
    onSuccess: requested,
  });
  const search = useMutation({
    mutationFn: () => unwrap(api.GET("/items/{itemId}/subtitles/bazarr/search", { params: { path: { itemId } } })),
    onMutate: () => {
      timer.start();
      setSearching(true);
    },
    onSettled: () => setSearching(false),
  });
  const pick = useMutation({
    mutationFn: (c: BazarrCandidate) =>
      unwrap(
        api.POST("/items/{itemId}/subtitles/bazarr/search", {
          params: { path: { itemId } },
          body: { provider: c.provider, subtitle: c.subtitle, hi: c.hi, forced: c.forced, originalFormat: c.originalFormat },
        }),
      ),
    onSuccess: requested,
  });
  const elapsed = timer.seconds;
  const s = status.data;
  if (status.isPending) return null;
  if (!s?.configured || (!s.managed && !s.error)) return null;
  const error = download.error ?? pick.error ?? search.error;
  return (
    <section aria-label="Bazarr" className="space-y-3 rounded-lg border border-border p-3">
      <h3 className="text-sm font-semibold">Bazarr</h3>
      {s.error && <Alert tone="error">{s.error}</Alert>}
      {error && <Alert tone="error">{error.message}</Alert>}
      {s.managed && (
        <>
          {s.languages.length > 0 && (
            <ul className="space-y-1" aria-label="Bazarr languages">
              {s.languages.map((l) => {
                const key = `${l.code2}-${l.forced}-${l.hi}`;
                const label = `${l.name}${l.forced ? " (forced)" : ""}${l.hi ? " (SDH)" : ""}`;
                return (
                  <li key={key} className="flex items-center gap-3 rounded-md px-2 py-1 text-sm">
                    <span className="min-w-0 flex-1 truncate">{label}</span>
                    {l.have ? (
                      <span className="flex items-center gap-1 text-success">
                        <Check className="size-4" aria-hidden /> Have it
                      </span>
                    ) : (
                      <>
                        <span className="text-xs text-muted">Wanted</span>
                        <Button
                          size="sm"
                          aria-label={`Download ${label}`}
                          loading={download.isPending && download.variables?.key === key}
                          disabled={download.isPending}
                          onClick={() => download.mutate({ language: l.code2, forced: l.forced, hi: l.hi, key })}
                        >
                          <Download className="size-4" /> Download
                        </Button>
                      </>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
          <div className="flex flex-wrap items-center gap-2">
            <div className="w-44">
              <Select aria-label="Another language" className="h-8" value={other} onChange={(e) => setOther(e.target.value)}>
                {bazarrLanguages.map(([code, name]) => (
                  <option key={code} value={code}>
                    {name}
                  </option>
                ))}
              </Select>
            </div>
            <Button
              size="sm"
              loading={download.isPending && download.variables?.key === "other"}
              disabled={download.isPending}
              onClick={() => download.mutate({ language: other, key: "other" })}
            >
              <Download className="size-4" /> Download this language
            </Button>
            <Button size="sm" variant="ghost" className="ml-auto" loading={search.isPending} onClick={() => search.mutate()}>
              <Search className="size-4" /> Search all providers
            </Button>
          </div>
          {search.isPending && (
            <div className="space-y-1" role="status">
              <p className="text-sm text-muted">Asking Bazarr's providers… this can take up to a minute ({elapsed} s).</p>
              <div className="h-1 overflow-hidden rounded-full bg-surface-3">
                <div className="h-full bg-accent transition-[width] duration-1000" style={{ width: `${Math.min(100, (elapsed / 60) * 100)}%` }} />
              </div>
            </div>
          )}
          {search.data?.length === 0 && <p className="text-sm text-muted">Bazarr's providers found nothing.</p>}
          {!!search.data?.length && (
            <ul className="max-h-72 space-y-1 overflow-y-auto" aria-label="Bazarr results">
              {search.data.map((c) => (
                <li key={`${c.provider}-${c.subtitle}`} className="flex items-center gap-3 rounded-md p-2 hover:bg-surface-2">
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm font-medium" title={c.release}>
                      {c.release || c.provider}
                    </span>
                    <span className="flex flex-wrap items-center gap-x-3 text-xs text-muted">
                      <span>{c.provider}</span>
                      <span>{c.language}</span>
                      <span>Score {c.score}</span>
                      {c.hi && (
                        <span className="flex items-center gap-1">
                          <Ear className="size-3.5" /> SDH
                        </span>
                      )}
                      {c.forced && <span>Forced</span>}
                    </span>
                  </span>
                  <Button
                    size="sm"
                    aria-label={`Download from ${c.provider}: ${c.release || c.subtitle}`}
                    loading={pick.isPending && pick.variables?.subtitle === c.subtitle}
                    disabled={pick.isPending}
                    onClick={() => pick.mutate(c)}
                  >
                    <Download className="size-4" /> Download
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </section>
  );
}

/** Bazarr on its own, e.g. from Library Health's missing subtitles (ADM-11). */
export function BazarrDialog({ itemId, title, onClose }: { itemId: number; title: string; onClose: () => void }) {
  const status = useQuery(bazarrStatusQuery(itemId));
  return (
    <Dialog open wide onClose={onClose} title={`Subtitles for ${title}`}>
      {status.isPending && <Spinner />}
      {status.data && !status.data.managed && !status.data.error && <p className="text-sm text-muted">Bazarr doesn't manage this title.</p>}
      <BazarrSection itemId={itemId} />
    </Dialog>
  );
}
