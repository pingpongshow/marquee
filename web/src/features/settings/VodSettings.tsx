import { useMutation, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Plus, RefreshCw, Trash2 } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { settingsQuery, useUpdateSettings } from "@/api/queries";
import type { components } from "@/api/schema.gen";
import { Alert, Button, Card, Field, Input, Select, Spinner, Toggle } from "@/components/ui";
import { vodStatusQuery } from "../livetv/vodApi";
import { SaveBar } from "./SaveBar";
import { VodSaveSettings } from "./VodSaveSettings";
import { VodDownloadsPanel } from "@/features/livetv/VodSaves";

type Source = components["schemas"]["VodSource"];
type Draft = { enabled: boolean; sources: Source[]; autoRefresh: boolean; refreshHours: number };

const syncHours = [1, 2, 3, 4, 6, 8, 12, 24, 48];

/** Checks a source as it stands in the form (a saved password is used when none is typed). */
function SourceTest({ source }: { source: Source }) {
  const test = useMutation({
    mutationFn: () => unwrap(api.POST("/livetv/vod/sources/test", { body: source })),
  });
  const r = test.data;
  return (
    <div className="flex flex-wrap items-center gap-3">
      <Button size="sm" variant="ghost" onClick={() => test.mutate()} disabled={test.isPending || !source.url}>
        {test.isPending ? "Testing…" : "Test"}
      </Button>
      {r?.ok && (
        <span className="text-sm text-success">
          {[
            r.account && `Account ${r.account.toLowerCase()}`,
            r.expiresAt && `expires ${new Date(r.expiresAt).toLocaleDateString()}`,
            `${r.movies ?? 0} films`,
            r.series !== undefined && `${r.series} shows`,
            r.categories !== undefined && `${r.categories} categories`,
          ]
            .filter(Boolean)
            .join(" · ")}
        </span>
      )}
      {r && !r.ok && <span className="text-sm text-danger">{r.error}</span>}
      {test.error && <span className="text-sm text-danger">{test.error.message}</span>}
    </div>
  );
}

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
    autoRefresh: query.data?.integrations?.vodAutoRefresh ?? true,
    refreshHours: query.data?.integrations?.vodRefreshHours ?? 6,
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
        Films from an M3U playlist, such as Pluto TV&apos;s free movies, or from a Dispatcharr (Xtream Codes) server appear
        on the VOD Movies page (and a Dispatcharr server&apos;s series on VOD Shows) for everyone who may watch Live TV (and within each profile&apos;s rating limit). Titles
        stream from the source while someone watches; nothing is downloaded unless an admin saves it to the library
        (below). Marquee reloads the lists on the schedule below and
        matches each film to TMDB.
      </p>
      <VodDownloadsPanel />
      <Card>
        <Toggle
          label="Show VOD"
          help="Off hides VOD Movies and VOD Shows everywhere; the titles and everyone's progress are kept."
          checked={d.enabled}
          onChange={(v) => setDraft({ ...d, enabled: v })}
        />
      </Card>
      <Card title="Sync" description="When the sources' film and show lists are reloaded. Each source shows when it last loaded.">
        <div className="space-y-4">
          <Toggle
            label="Sync automatically"
            help="Off: the lists reload only when you press Refresh now (and once at start for a source that has never loaded). Links from some sources, such as Pluto TV, expire after about a day."
            checked={d.autoRefresh}
            onChange={(v) => setDraft({ ...d, autoRefresh: v })}
          />
          {d.autoRefresh && (
            <Field label="Every" help="Counted from the last successful sync.">
              {(id) => (
                <Select id={id} value={d.refreshHours} onChange={(e) => setDraft({ ...d, refreshHours: Number(e.target.value) })}>
                  {(syncHours.includes(d.refreshHours) ? syncHours : [...syncHours, d.refreshHours].sort((a, b) => a - b)).map((h) => (
                    <option key={h} value={h}>
                      {h === 1 ? "1 hour" : `${h} hours`}
                    </option>
                  ))}
                </Select>
              )}
            </Field>
          )}
        </div>
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
                  `${ss.movies} films${ss.series ? ` · ${ss.series} shows` : ""}${ss.refreshedAt ? ` · loaded ${new Date(ss.refreshedAt).toLocaleString()}` : ""}`
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
              <Field label="Type">
                {(id) => (
                  <Select
                    id={id}
                    value={s.kind ?? "m3u"}
                    onChange={(e) => edit(i, { kind: e.target.value as Source["kind"] })}
                  >
                    <option value="m3u">M3U playlist</option>
                    <option value="xtream">Dispatcharr / Xtream Codes</option>
                  </Select>
                )}
              </Field>
              {s.kind === "xtream" ? (
                <>
                  <Field label="Server URL" help="Dispatcharr's address, as the apps use it.">
                    {(id) => (
                      <Input
                        id={id}
                        value={s.url}
                        onChange={(e) => edit(i, { url: e.target.value })}
                        placeholder="http://dispatcharr:9191"
                      />
                    )}
                  </Field>
                  <Field label="Username" help="A Dispatcharr user with an XC password.">
                    {(id) => (
                      <Input
                        id={id}
                        value={s.username ?? ""}
                        autoComplete="off"
                        onChange={(e) => edit(i, { username: e.target.value })}
                      />
                    )}
                  </Field>
                  <Field label="Password" help="The user's XC password.">
                    {(id) => (
                      <Input
                        id={id}
                        type="password"
                        autoComplete="new-password"
                        value={s.password ?? ""}
                        placeholder={s.passwordSet ? "Saved — type to change" : ""}
                        onChange={(e) => edit(i, { password: e.target.value || undefined })}
                      />
                    )}
                  </Field>
                </>
              ) : (
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
              )}
              <Field label="User agent" help="Only if the provider needs a particular one.">
                {(id) => (
                  <Input id={id} value={s.userAgent ?? ""} onChange={(e) => edit(i, { userAgent: e.target.value })} />
                )}
              </Field>
            </div>
            <div className="mt-4">
              <SourceTest source={s} />
            </div>
            <div className="mt-4">
              <Toggle label="Enabled" checked={s.enabled !== false} onChange={(v) => edit(i, { enabled: v })} />
            </div>
          </Card>
        );
      })}
      <div className="flex flex-wrap gap-3">
        <Button
          onClick={() => setDraft({ ...d, sources: [...d.sources, { name: "Pluto TV movies", kind: "m3u", url: "", enabled: true }] })}
        >
          <Plus className="size-4" /> Add source
        </Button>
        {server.enabled && server.sources.length > 0 && (
          <Button variant="ghost" onClick={() => refresh.mutate()} disabled={refresh.isPending}>
            <RefreshCw className={refresh.isPending ? "size-4 animate-spin" : "size-4"} /> Refresh now
          </Button>
        )}
      </div>
      {st && server.sources.length > 0 && st.movies > 0 && (
        <p className="text-sm text-muted">
          {st.movies} films, {st.matched} matched to TMDB
          {st.unmatched > 0 && `, ${st.unmatched} without a match`}
          {st.series > 0 && ` · ${st.series} shows`}
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
            {
              integrations: {
                vodEnabled: d.enabled,
                vodSources: d.sources,
                vodAutoRefresh: d.autoRefresh,
                vodRefreshHours: d.refreshHours,
              },
            },
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
      <VodSaveSettings />
    </div>
  );
}
