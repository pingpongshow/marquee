import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, CheckCircle2, Download, RefreshCw } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { usersQuery } from "@/api/queries";
import type { components } from "@/api/schema.gen";
import { Alert, Button, Card, Input, Select, Spinner, Toggle } from "@/components/ui";

type S = components["schemas"];
type Preview = S["PlexImportPreview"];
type Choice = S["PlexAccountChoice"];
type Include = Required<S["PlexImportInclude"]>;
type Report = S["PlexImportReport"];

const statusQuery = {
  queryKey: ["plex-import"],
  queryFn: () => unwrap(api.GET("/plex-import")),
};

function pct(a: number, b: number) {
  return b ? `${Math.round((a / b) * 100)}%` : "–";
}

function ReportView({ r }: { r: Report }) {
  const rows: [string, string][] = [
    ["Files paired", `${r.matchedFiles.toLocaleString()} of ${r.files.toLocaleString()}`],
    ["Users", `${r.usersLinked} linked, ${r.usersCreated} created`],
    ["Watched / resume points", r.watchStateItems.toLocaleString()],
    ["Ratings", r.ratings.toLocaleString()],
    ["Play history", `${r.historyImported.toLocaleString()} imported${r.historySkipped ? `, ${r.historySkipped} already present` : ""}`],
    ["Matches", `${r.matchesAgreed.toLocaleString()} agreed, ${r.matchesApplied} taken from Plex, ${r.matchesKept.length} kept Marquee's`],
    ["Chosen artwork", `${r.artwork} copied${r.artworkMissing ? `, ${r.artworkMissing} not found` : ""}`],
    ["Intro & credits markers", r.markers.toLocaleString()],
    ["Playlists", `${r.playlists} (${r.playlistItems.toLocaleString()} items${r.playlistItemsMissing ? `, ${r.playlistItemsMissing} not in your library` : ""})`],
  ];
  return (
    <div className="space-y-4 text-sm">
      <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-1.5">
        {rows.map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="text-muted">{k}</dt>
            <dd>{v}</dd>
          </div>
        ))}
      </dl>
      {r.usersCreated > 0 && <Alert tone="info">New users sign in by choosing their profile on the home network. Add a PIN or password for them in Settings → Users if you like.</Alert>}
      {[
        ["Kept Marquee's match (review with ⋯ → Fix match if Plex was right)", r.matchesKept],
        ["Not applied", r.matchFailures],
        ["Plex files not found in Marquee", r.unmatchedSample],
        ["Warnings", r.warnings],
      ].map(([title, list]) =>
        (list as string[]).length ? (
          <details key={title as string} className="rounded-md bg-surface-2 px-3 py-2">
            <summary className="cursor-pointer">
              {title as string} ({(list as string[]).length})
            </summary>
            <ul className="mt-2 max-h-60 space-y-0.5 overflow-y-auto font-mono text-xs text-muted">
              {(list as string[]).map((x) => (
                <li key={x}>{x}</li>
              ))}
            </ul>
          </details>
        ) : null,
      )}
      <p className="text-xs text-faint">Finished {new Date(r.finishedAt).toLocaleString()}. Running the import again updates everything without duplicating it.</p>
    </div>
  );
}

export function PlexImportSettings() {
  const qc = useQueryClient();
  const status = useQuery({ ...statusQuery, refetchInterval: (q) => (q.state.data?.running ? 1500 : false) });
  const users = useQuery(usersQuery);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [mappings, setMappings] = useState<S["PlexPathMapping"][]>([]);
  const [choices, setChoices] = useState<Record<number, Choice>>({});
  const [include, setInclude] = useState<Include>({ matches: true, watchState: true, history: true, playlists: true, markers: true, artwork: true });

  const check = useMutation({
    mutationFn: (m?: S["PlexPathMapping"][]) => unwrap(api.POST("/plex-import/preview", { body: m ? { pathMappings: m } : {} })),
    onSuccess: (pv) => {
      setPreview(pv);
      setMappings(pv.pathMappings);
      setChoices((prev) =>
        Object.fromEntries(
          pv.accounts.map((a) => [
            a.id,
            prev[a.id] ?? { plexId: a.id, action: a.suggestedAction, userId: a.suggestedUserId, username: a.suggestedUsername ?? a.name },
          ]),
        ),
      );
    },
  });
  const start = useMutation({
    mutationFn: () => unwrap(api.POST("/plex-import", { body: { pathMappings: mappings, accounts: Object.values(choices), include } })),
    onSuccess: (st) => qc.setQueryData(statusQuery.queryKey, st),
  });

  if (status.isPending) return <Spinner />;
  const st = status.data;
  if (!st?.available)
    return (
      <Card title="Plex database not found">
        <p className="text-sm text-muted">
          Mount your Plex data folder (the one containing <code className="text-text">Library</code>) read-only at <code className="text-text">/plex</code> in the Marquee container, then
          reload this page.
        </p>
      </Card>
    );

  if (st.running)
    return (
      <Card title="Importing from Plex">
        <div className="space-y-2">
          <div className="text-sm">{st.progress?.step}…</div>
          <div className="h-1.5 overflow-hidden rounded-full bg-surface-3">
            <div
              className={st.progress?.total ? "h-full bg-accent transition-all" : "h-full w-1/3 animate-pulse bg-accent"}
              style={st.progress?.total ? { width: pct(st.progress.done, st.progress.total) } : undefined}
            />
          </div>
          <p className="text-xs text-faint">You can leave this page; the import continues in the background.</p>
        </div>
      </Card>
    );

  return (
    <div className="space-y-6">
      {st.lastError && <Alert tone="error">The last import failed: {st.lastError}</Alert>}
      {st.lastReport && (
        <Card title="Last import" description={`From ${st.databasePath}`}>
          <ReportView r={st.lastReport} />
        </Card>
      )}

      <Card
        title={st.lastReport ? "Import again" : "Import from Plex"}
        description="Brings over watch status, resume points, ratings, play history, playlists, intro/credits markers, the posters you chose and Plex's matches. Plex itself is not changed."
        actions={
          <Button size="sm" variant={preview ? "ghost" : "primary"} onClick={() => check.mutate(undefined)} loading={check.isPending}>
            <RefreshCw className="size-4" /> {preview ? "Re-check" : "Check Plex library"}
          </Button>
        }
      >
        {check.isError && <Alert tone="error">{check.error.message}</Alert>}
        {check.isPending && <Spinner label="Reading a copy of the Plex database" />}
        {!preview && !check.isPending && <p className="text-sm text-muted">Marquee reads a copy of the Plex database and shows what it found before anything is imported.</p>}

        {preview && !check.isPending && (
          <div className="space-y-8">
            <section>
              <h3 className="mb-2 text-sm font-semibold">1. Folders</h3>
              <p className="mb-3 text-sm text-muted">
                {preview.matchedFiles.toLocaleString()} of {preview.files.toLocaleString()} Plex files ({pct(preview.matchedFiles, preview.files)}) were found in Marquee.
              </p>
              <div className="space-y-2">
                {mappings.map((m, i) => {
                  const sec = preview.sections.find((s) => s.roots.includes(m.plexPath));
                  return (
                    <div key={i} className="grid grid-cols-[1fr_auto_1fr_5rem] items-center gap-2">
                      <Input aria-label="Plex folder" value={m.plexPath} onChange={(e) => setMappings(mappings.map((x, j) => (j === i ? { ...x, plexPath: e.target.value } : x)))} />
                      <ArrowRight className="size-4 text-faint" aria-hidden />
                      <Input aria-label="Marquee folder" value={m.marqueePath} onChange={(e) => setMappings(mappings.map((x, j) => (j === i ? { ...x, marqueePath: e.target.value } : x)))} />
                      <span className="text-right text-xs text-muted" title={sec ? `${sec.name}: ${sec.matchedFiles} of ${sec.files} files` : undefined}>
                        {sec ? pct(sec.matchedFiles, sec.files) : ""}
                      </span>
                    </div>
                  );
                })}
              </div>
              <Button size="sm" variant="ghost" className="mt-2" onClick={() => check.mutate(mappings)}>
                Re-check with these folders
              </Button>
            </section>

            <section>
              <h3 className="mb-2 text-sm font-semibold">2. People</h3>
              <ul className="divide-y divide-border rounded-md border border-border">
                {preview.accounts.map((a) => {
                  const c = choices[a.id];
                  if (!c) return null;
                  const set = (patch: Partial<Choice>) => setChoices({ ...choices, [a.id]: { ...c, ...patch } });
                  return (
                    <li key={a.id} className="flex flex-wrap items-center gap-3 px-3 py-2.5">
                      <div className="min-w-40 flex-1">
                        <div className="text-sm font-medium">
                          {a.name} {a.isOwner && <span className="text-xs text-accent">(Plex owner)</span>}
                        </div>
                        <div className="text-xs text-muted">
                          {a.watched} watched · {a.inProgress} in progress · {a.history} plays{a.playlists ? ` · ${a.playlists} playlists` : ""}
                        </div>
                      </div>
                      <Select aria-label={`Action for ${a.name}`} className="h-8 w-36" value={c.action} onChange={(e) => set({ action: e.target.value as Choice["action"] })}>
                        <option value="link">Link to user</option>
                        <option value="create">Create user</option>
                        <option value="merge">Merge into…</option>
                        <option value="skip">Skip</option>
                      </Select>
                      {c.action === "link" && (
                        <Select aria-label="Marquee user" className="h-8 w-44" value={c.userId ?? ""} onChange={(e) => set({ userId: Number(e.target.value) || undefined })}>
                          <option value="">Choose…</option>
                          {users.data?.map((u) => (
                            <option key={u.id} value={u.id}>
                              {u.displayName}
                            </option>
                          ))}
                        </Select>
                      )}
                      {c.action === "create" && <Input aria-label="New username" className="h-8 w-44" value={c.username ?? ""} onChange={(e) => set({ username: e.target.value })} />}
                      {c.action === "merge" && (
                        <Select aria-label="Merge into account" className="h-8 w-44" value={c.mergeInto ?? ""} onChange={(e) => set({ mergeInto: Number(e.target.value) || undefined })}>
                          <option value="">Choose account…</option>
                          {preview.accounts
                            .filter((o) => o.id !== a.id && (choices[o.id]?.action === "create" || choices[o.id]?.action === "link"))
                            .map((o) => (
                              <option key={o.id} value={o.id}>
                                {o.name}
                              </option>
                            ))}
                        </Select>
                      )}
                    </li>
                  );
                })}
              </ul>
            </section>

            <section>
              <h3 className="mb-2 text-sm font-semibold">3. What to import</h3>
              <div className="grid gap-3 sm:grid-cols-2">
                {(
                  [
                    ["watchState", "Watched status, resume points and ratings"],
                    ["history", "Play history"],
                    ["playlists", "Playlists"],
                    ["markers", "Intro and credits markers"],
                    ["artwork", "Posters you chose in Plex"],
                    ["matches", "Plex's matches where they fit better"],
                  ] as [keyof Include, string][]
                ).map(([k, label]) => (
                  <Toggle key={k} label={label} checked={include[k]} onChange={(v) => setInclude({ ...include, [k]: v })} />
                ))}
              </div>
            </section>

            {start.isError && <Alert tone="error">{start.error.message}</Alert>}
            <div className="flex items-center justify-end gap-3">
              {st.lastReport && (
                <span className="flex items-center gap-1 text-xs text-muted">
                  <CheckCircle2 className="size-4 text-success" /> Safe to repeat: nothing is duplicated
                </span>
              )}
              <Button variant="primary" onClick={() => start.mutate()} loading={start.isPending} disabled={!mappings.length}>
                <Download className="size-4" /> Start import
              </Button>
            </div>
          </div>
        )}
      </Card>
    </div>
  );
}
