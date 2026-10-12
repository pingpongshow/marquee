import { useMutation, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Plus, RefreshCw, Trash2 } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { settingsQuery, useUpdateSettings } from "@/api/queries";
import type { components } from "@/api/schema.gen";
import { Alert, Button, Card, Field, Input, Spinner, Toggle } from "@/components/ui";
import { vodStatusQuery } from "../livetv/vodApi";
import { SaveBar } from "./SaveBar";

type Source = components["schemas"]["VodSource"];
type Draft = { enabled: boolean; sources: Source[] };

/** Movies on demand (LIVE-6): film playlists such as Pluto TV's, shown under Live TV. */
export function VodSettings() {
  const query = useQuery(settingsQuery);
  // While films are being matched, keep the counts moving.
  const status = useQuery({
    ...vodStatusQuery,
    refetchInterval: (q) => (q.state.data?.matching ? 3000 : false),
  });
  const save = useUpdateSettings();
  const server: Draft = {
    enabled: !!query.data?.integrations?.vodEnabled,
    sources: query.data?.integrations?.vodSources ?? [],
  };
  const [draft, setDraft] = useState<Draft | null>(null);
  const [savedAt, setSavedAt] = useState<number | null>(null);
  const refresh = useMutation({
    mutationFn: () => unwrap(api.POST("/livetv/vod/refresh")),
    onSuccess: () => status.refetch(),
  });
  if (query.isPending) return <Spinner />;
  const d = draft ?? server;
  const dirty = draft !== null && JSON.stringify(draft) !== JSON.stringify(server);
  const edit = (i: number, patch: Partial<Source>) =>
    setDraft({ ...d, sources: d.sources.map((s, j) => (j === i ? { ...s, ...patch } : s)) });
  const state = (id?: string) => status.data?.sources.find((s) => s.id === id);
  const st = status.data;

  return (
    <div className="space-y-6">
      <p className="text-sm text-muted">
        Films from an M3U playlist, such as Pluto TV&apos;s free movies, appear as a Movies tab in Live TV for everyone who
        may watch Live TV (and within each profile&apos;s rating limit). Nothing is downloaded: a film streams from the
        source while someone watches it. Marquee reloads the lists every six hours and matches each film to TMDB.
      </p>
      <Card>
        <Toggle
          label="Show movies on demand"
          help="Off hides the Movies tab everywhere; the films and everyone's progress are kept."
          checked={d.enabled}
          onChange={(v) => setDraft({ ...d, enabled: v })}
        />
      </Card>
      {st?.tmdbConfigured === false && (
        <Alert>
          Films are matched to TMDB for posters, cast and ratings once a TMDB API key is set in{" "}
          <Link to="/settings/$section" params={{ section: "metadata" }} className="underline">
            Settings → Metadata
          </Link>
          . Until then they show the source&apos;s own details.
        </Alert>
      )}
      {d.sources.map((s, i) => {
        const ss = state(s.id);
        return (
          <Card
            key={s.id ?? `new-${i}`}
            title={s.name || "New source"}
            description={
              ss ? (
                ss.error ? (
                  <span className="text-danger">{ss.error}</span>
                ) : (
                  `${ss.movies} films${ss.refreshedAt ? ` · loaded ${new Date(ss.refreshedAt).toLocaleString()}` : ""}`
                )
              ) : undefined
            }
            actions={
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setDraft({ ...d, sources: d.sources.filter((_, j) => j !== i) })}
                aria-label="Remove source"
              >
                <Trash2 className="size-4" />
              </Button>
            }
          >
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Name">
                {(id) => <Input id={id} value={s.name} onChange={(e) => edit(i, { name: e.target.value })} />}
              </Field>
              <Field label="Playlist URL" help="Each film's group (group-title) becomes its category.">
                {(id) => (
                  <Input
                    id={id}
                    value={s.url}
                    onChange={(e) => edit(i, { url: e.target.value })}
                    placeholder="https://…/pluto-vod-US.m3u"
                  />
                )}
              </Field>
              <Field label="User agent" help="Only if the provider needs a particular one.">
                {(id) => (
                  <Input id={id} value={s.userAgent ?? ""} onChange={(e) => edit(i, { userAgent: e.target.value })} />
                )}
              </Field>
            </div>
            <div className="mt-4">
              <Toggle label="Enabled" checked={s.enabled !== false} onChange={(v) => edit(i, { enabled: v })} />
            </div>
          </Card>
        );
      })}
      <div className="flex flex-wrap gap-3">
        <Button
          onClick={() => setDraft({ ...d, sources: [...d.sources, { name: "Pluto TV movies", url: "", enabled: true }] })}
        >
          <Plus className="size-4" /> Add source
        </Button>
        {server.enabled && server.sources.length > 0 && (
          <Button variant="ghost" onClick={() => refresh.mutate()} disabled={refresh.isPending}>
            <RefreshCw className={refresh.isPending ? "size-4 animate-spin" : "size-4"} /> Reload movies now
          </Button>
        )}
      </div>
      {st && server.sources.length > 0 && st.movies > 0 && (
        <p className="text-sm text-muted">
          {st.movies} films, {st.matched} matched to TMDB
          {st.unmatched > 0 && `, ${st.unmatched} without a match`}
          {st.matching && " · matching…"}
        </p>
      )}
      {refresh.error && <Alert tone="error">{refresh.error.message}</Alert>}
      <SaveBar
        dirty={dirty}
        saving={save.isPending}
        error={save.error}
        savedAt={savedAt}
        onSave={() =>
          save.mutate(
            { integrations: { vodEnabled: d.enabled, vodSources: d.sources } },
            {
              onSuccess: () => {
                setDraft(null);
                setSavedAt(Date.now());
                setTimeout(() => status.refetch(), 3000);
              },
            },
          )
        }
        onReset={() => setDraft(null)}
      />
    </div>
  );
}
