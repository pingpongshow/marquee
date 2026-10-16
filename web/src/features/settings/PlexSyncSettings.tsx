import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeftRight, ArrowRight, Eye, LogIn, RefreshCw } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { api, unwrap } from "@/api/client";
import { usersQuery } from "@/api/queries";
import type { components } from "@/api/schema.gen";
import { toast } from "@/components/Toast";
import { Alert, Badge, Button, Card, Field, Input, Select, Spinner, Toggle } from "@/components/ui";
import { SecretField } from "./ServerSections";

type S = components["schemas"];
type Status = S["PlexSyncStatus"];
type Report = S["PlexSyncReport"];
type Update = S["PlexSyncSettingsUpdate"];

const syncQuery = {
  queryKey: ["plex-sync"],
  queryFn: () => unwrap(api.GET("/plex-sync")),
};

const intervals: [number, string][] = [
  [5, "5 minutes"],
  [15, "15 minutes"],
  [30, "30 minutes"],
  [60, "1 hour"],
  [120, "2 hours"],
  [360, "6 hours"],
  [720, "12 hours"],
  [1440, "24 hours"],
];

const kindLabel: Record<S["PlexSyncChange"]["kind"], string> = {
  watched: "Watched",
  unwatched: "Unwatched",
  plays: "Plays",
  lastPlayed: "Last played",
  position: "Resume position",
  rating: "Rating",
  history: "Play",
};

const accountKind: Record<S["PlexSyncUser"]["kind"], string> = {
  owner: "Plex owner",
  home: "Plex Home",
  managed: "Managed",
  shared: "Shared",
  unknown: "",
};

export function summary(r: Report) {
  if (r.waiting) return "Plex wasn't reachable; the sync waits for the next run.";
  const parts = [`${r.dryRun ? "Would push" : "Pushed"} ${r.toPlex} to Plex`, `pulled ${r.toMarquee + r.historyPulled}`, `${r.conflicts} conflicts`, `${r.skipped} skipped`];
  if (r.failed) parts.push(`${r.failed} failed`);
  return parts.join(", ");
}

function ReportView({ r }: { r: Report }) {
  return (
    <div className="space-y-3 text-sm">
      <p className={r.waiting ? "text-danger" : undefined}>{summary(r)}</p>
      {!r.waiting && (
        <p className="text-xs text-muted">
          {r.usersSynced} {r.usersSynced === 1 ? "person" : "people"} · {r.unchanged.toLocaleString()} already in step
          {r.historyPulled ? ` · ${r.historyPulled.toLocaleString()} plays from Plex's history` : ""}
          {r.notInMarquee ? ` · ${r.notInMarquee.toLocaleString()} Plex items not in Marquee` : ""}
        </p>
      )}
      {r.changes.length > 0 && (
        <details className="rounded-md bg-surface-2 px-3 py-2" open={r.dryRun}>
          <summary className="cursor-pointer">
            {r.dryRun ? "What would change" : "What changed"} ({r.changes.length}
            {r.changesTruncated ? "+" : ""})
          </summary>
          <ul className="mt-2 max-h-80 space-y-1 overflow-y-auto text-xs">
            {r.changes.map((c, i) => (
              <li key={i} className="flex flex-wrap items-baseline gap-x-2">
                <span className={c.direction === "toPlex" ? "w-24 shrink-0 text-accent" : "w-24 shrink-0 text-success"}>{c.direction === "toPlex" ? "→ Plex" : "→ Marquee"}</span>
                <span className="font-medium">{c.title}</span>
                <span className="text-muted">
                  {kindLabel[c.kind]}
                  {c.from || c.to ? ": " : ""}
                  {c.from}
                  {c.from && c.to ? " → " : ""}
                  {c.kind === "history" || c.kind === "lastPlayed" ? (c.to ? new Date(c.to).toLocaleString() : "") : c.to}
                </span>
                <span className="text-faint">{c.user}</span>
                {c.conflict && <Badge tone="danger">changed on both</Badge>}
              </li>
            ))}
          </ul>
          {r.changesTruncated && <p className="mt-1 text-xs text-faint">Only the first changes are listed.</p>}
        </details>
      )}
      {r.errors.length > 0 && (
        <details className="rounded-md bg-surface-2 px-3 py-2">
          <summary className="cursor-pointer text-danger">Problems ({r.errors.length})</summary>
          <ul className="mt-2 max-h-60 space-y-0.5 overflow-y-auto font-mono text-xs text-muted">
            {r.errors.map((e, i) => (
              <li key={i}>{e}</li>
            ))}
          </ul>
        </details>
      )}
      <p className="text-xs text-faint">
        {r.dryRun ? "Checked" : "Finished"} {new Date(r.finishedAt).toLocaleString()}
      </p>
    </div>
  );
}

/** Sign in with Plex: opens plex.tv and polls the PIN until the admin approves it. */
function SignInWithPlex({ onDone }: { onDone: () => void }) {
  const [code, setCode] = useState<string | null>(null);
  const timer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearInterval(timer.current), []);
  const start = useMutation({
    mutationFn: () => unwrap(api.POST("/plex-sync/pin")),
    onSuccess: (pin) => {
      setCode(pin.code);
      window.open(pin.authUrl, "_blank", "noopener");
      const until = Date.now() + 5 * 60_000;
      window.clearInterval(timer.current);
      timer.current = window.setInterval(async () => {
        if (Date.now() > until) {
          window.clearInterval(timer.current);
          setCode(null);
          return;
        }
        try {
          const r = await unwrap(api.GET("/plex-sync/pin/{pinId}", { params: { path: { pinId: pin.id } } }));
          if (r.signedIn) {
            window.clearInterval(timer.current);
            setCode(null);
            toast("Signed in with Plex");
            onDone();
          }
        } catch {
          // keep polling; plex.tv may be slow
        }
      }, 2000);
    },
  });
  return (
    <div className="flex flex-wrap items-center gap-3">
      <Button size="sm" variant="ghost" onClick={() => start.mutate()} loading={start.isPending}>
        <LogIn className="size-4" /> Sign in with Plex
      </Button>
      {code && (
        <span className="text-xs text-muted">
          Waiting for you to approve in the Plex tab… (or enter <code className="text-text">{code}</code> at plex.tv/link)
        </span>
      )}
      {start.isError && <span className="text-xs text-danger">{start.error.message}</span>}
    </div>
  );
}

function ConnectionAndOptions({ st }: { st: Status }) {
  const qc = useQueryClient();
  const c = st.settings;
  const [draft, setDraft] = useState<Update>({});
  const v = { ...c, ...draft };
  const dirty = Object.keys(draft).length > 0;
  const set = (p: Update) => setDraft({ ...draft, ...p });
  const save = useMutation({
    mutationFn: () => unwrap(api.PATCH("/plex-sync", { body: draft })),
    onSuccess: (next) => {
      qc.setQueryData(syncQuery.queryKey, next);
      setDraft({});
    },
  });
  const mappings = v.pathMappings ?? [];
  const intervalOptions = intervals.some(([m]) => m === v.intervalMinutes) ? intervals : [...intervals, [v.intervalMinutes, `${v.intervalMinutes} minutes`] as [number, string]];

  return (
    <div className="space-y-5">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Plex server address" help="As Marquee reaches it, with the port.">
          {(id) => <Input id={id} placeholder="http://192.168.1.10:32400" value={v.url} onChange={(e) => set({ url: e.target.value })} />}
        </Field>
        <SecretField
          label="Plex token"
          help="The Plex server owner's token. Stored on the server, never shown again."
          placeholder="Paste a Plex token"
          isSet={c.tokenSet}
          value={draft.token}
          onChange={(t) => set({ token: t })}
        />
      </div>
      <SignInWithPlex onDone={() => qc.invalidateQueries({ queryKey: syncQuery.queryKey })} />

      <div className="grid gap-3 sm:grid-cols-2">
        <Toggle label="Watched status and plays" help="Watched or unwatched, play counts and when last played." checked={v.watchState} onChange={(x) => set({ watchState: x })} />
        <Toggle label="Ratings" checked={v.ratings} onChange={(x) => set({ ratings: x })} />
        <Toggle label="Resume positions" help="Movies and episodes." checked={v.resumePositions} onChange={(x) => set({ resumePositions: x })} />
        <Toggle
          label="Play history"
          help="Plays in Plex appear in Marquee's history. Plays in Marquee reach Plex when the sync runs, so Plex shows the sync time, not when you played them."
          checked={v.playHistory}
          onChange={(x) => set({ playHistory: x })}
        />
      </div>

      <div className="flex flex-wrap items-end gap-4">
        <Toggle label="Automatic sync" help="Off until you turn it on. Sync now works either way." checked={v.enabled} onChange={(x) => set({ enabled: x })} />
        <Field label="Every">
          {(id) => (
            <Select id={id} className="w-36" value={v.intervalMinutes} disabled={!v.enabled} onChange={(e) => set({ intervalMinutes: Number(e.target.value) })}>
              {intervalOptions.map(([m, label]) => (
                <option key={m} value={m}>
                  {label}
                </option>
              ))}
            </Select>
          )}
        </Field>
      </div>

      <details className="rounded-md bg-surface-2 px-3 py-2">
        <summary className="cursor-pointer text-sm">Folders ({mappings.length ? `${mappings.length} mapped` : "suggested automatically"})</summary>
        <p className="mt-2 text-xs text-muted">Items pair up through their files: each Plex folder and the Marquee folder with the same files. The Plex import saves its folders here.</p>
        <div className="mt-2 space-y-2">
          {mappings.map((m, i) => (
            <div key={i} className="grid grid-cols-[1fr_auto_1fr_auto] items-center gap-2">
              <Input aria-label="Plex folder" value={m.plexPath} onChange={(e) => set({ pathMappings: mappings.map((x, j) => (j === i ? { ...x, plexPath: e.target.value } : x)) })} />
              <ArrowRight className="size-4 text-faint" aria-hidden />
              <Input aria-label="Marquee folder" value={m.marqueePath} onChange={(e) => set({ pathMappings: mappings.map((x, j) => (j === i ? { ...x, marqueePath: e.target.value } : x)) })} />
              <Button size="sm" variant="ghost" onClick={() => set({ pathMappings: mappings.filter((_, j) => j !== i) })}>
                Remove
              </Button>
            </div>
          ))}
          <Button size="sm" variant="ghost" onClick={() => set({ pathMappings: [...mappings, { plexPath: "", marqueePath: "" }] })}>
            Add folder
          </Button>
        </div>
      </details>

      {save.isError && <Alert tone="error">{save.error.message}</Alert>}
      {dirty && (
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={() => setDraft({})}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => save.mutate()} loading={save.isPending}>
            Save
          </Button>
        </div>
      )}
    </div>
  );
}

function People({ st }: { st: Status }) {
  const qc = useQueryClient();
  const users = useQuery(usersQuery);
  const link = useMutation({
    mutationFn: ({ plexAccountId, userId }: { plexAccountId: number; userId?: number }) =>
      unwrap(api.PUT("/plex-sync/users/{plexAccountId}", { params: { path: { plexAccountId } }, body: userId ? { userId } : {} })),
    onSuccess: (next) => qc.setQueryData(syncQuery.queryKey, next),
  });
  if (!st.users.length) return <p className="text-sm text-muted">Plex accounts appear here after the Plex import, a preview or a sync.</p>;
  return (
    <>
      {link.isError && <Alert tone="error">{link.error.message}</Alert>}
      <ul className="divide-y divide-border rounded-md border border-border">
        {st.users.map((u) => (
          <li key={u.plexAccountId} className="flex flex-wrap items-center gap-3 px-3 py-2.5">
            <div className="min-w-40 flex-1">
              <div className="text-sm font-medium">
                {u.name || `Plex account ${u.plexAccountId}`} {accountKind[u.kind] && <span className="text-xs text-muted">({accountKind[u.kind]})</span>}
              </div>
              {u.reason && <div className="text-xs text-muted">{u.reason}</div>}
            </div>
            <Select
              aria-label={`Marquee user for ${u.name}`}
              className="h-8 w-44"
              value={u.userId ?? ""}
              onChange={(e) => link.mutate({ plexAccountId: u.plexAccountId, userId: Number(e.target.value) || undefined })}
            >
              <option value="">Don't sync</option>
              {users.data?.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.displayName}
                </option>
              ))}
            </Select>
          </li>
        ))}
      </ul>
    </>
  );
}

export function PlexSyncSettings() {
  const qc = useQueryClient();
  const status = useQuery({ ...syncQuery, refetchInterval: (q) => (q.state.data?.running ? 1500 : false) });
  const [preview, setPreview] = useState<Report | null>(null);
  const run = useMutation({
    mutationFn: () => unwrap(api.POST("/plex-sync/run")),
    onSuccess: (st) => {
      setPreview(null);
      qc.setQueryData(syncQuery.queryKey, st);
    },
  });
  const check = useMutation({
    mutationFn: () => unwrap(api.POST("/plex-sync/preview")),
    onSuccess: (r) => {
      setPreview(r);
      qc.invalidateQueries({ queryKey: syncQuery.queryKey }); // the Plex accounts were refreshed
    },
  });

  if (status.isPending) return <Spinner />;
  if (status.isError || !status.data) return <Alert tone="error">{status.error?.message ?? "Couldn't load Plex sync"}</Alert>;
  const st = status.data;
  const configured = !!st.settings.url && st.settings.tokenSet;
  const busy = st.running || run.isPending || check.isPending;

  return (
    <Card
      title="Two-way sync"
      description="Keeps watched status, plays, resume positions and ratings the same in Marquee and Plex. When something changed on one side, it's copied to the other; when it changed on both, the newer change wins. Plex is changed only through its own API: its database, libraries and files are never touched, and nothing in Plex is deleted."
      actions={
        configured ? (
          st.reachable === false ? (
            <Badge tone="danger">Plex isn't reachable</Badge>
          ) : st.reachable ? (
            <Badge tone="accent">Connected</Badge>
          ) : undefined
        ) : undefined
      }
    >
      <div className="space-y-8">
        {configured && st.reachable === false && (
          <Alert tone="error">Marquee can't reach Plex at {st.settings.url}. Is it running? The sync waits and carries on when Plex is back; nothing is lost.</Alert>
        )}
        {st.lastError && <Alert tone="error">The last sync failed: {st.lastError}</Alert>}

        <section>
          <h3 className="mb-3 text-sm font-semibold">Connection and what to sync</h3>
          <ConnectionAndOptions st={st} key={JSON.stringify(st.settings)} />
        </section>

        <section>
          <h3 className="mb-2 text-sm font-semibold">People</h3>
          <p className="mb-3 text-xs text-muted">
            Each Plex account syncs with the Marquee user you choose. Plex Home users sync through the owner's sign-in; friends you share the server with can only sync if Plex lets the owner act for
            them.
          </p>
          <People st={st} />
        </section>

        <section className="space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <Button variant={st.lastRun ? "ghost" : "primary"} onClick={() => check.mutate()} loading={check.isPending} disabled={!configured || busy}>
              <Eye className="size-4" /> Preview
            </Button>
            <Button variant={st.lastRun ? "primary" : "ghost"} onClick={() => run.mutate()} loading={run.isPending} disabled={!configured || busy}>
              <ArrowLeftRight className="size-4" /> Sync now
            </Button>
            {st.nextRunAt && !st.running && <span className="text-xs text-muted">Next automatic sync {new Date(st.nextRunAt).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" })}</span>}
          </div>
          {!configured && <p className="text-xs text-muted">Set the Plex server address and token (or sign in with Plex) first.</p>}
          {!st.lastRun && configured && !preview && <p className="text-xs text-muted">Try Preview first: it lists what would change each way without changing anything.</p>}
          {run.isError && <Alert tone="error">{run.error.message}</Alert>}
          {check.isError && <Alert tone="error">{check.error.message}</Alert>}
          {check.isPending && <Spinner label="Comparing Marquee and Plex" />}
          {st.running && (
            <div className="flex items-center gap-2 text-sm">
              <RefreshCw className="size-4 animate-spin text-accent" /> {st.progress?.step ?? "Syncing"}…{st.progress?.done ? ` (${st.progress.done.toLocaleString()} items)` : ""}
            </div>
          )}
          {preview && !check.isPending && (
            <div className="rounded-md border border-border p-3">
              <h4 className="mb-2 text-sm font-semibold">Preview</h4>
              <ReportView r={preview} />
            </div>
          )}
          {st.lastRun && !st.running && (
            <div className="rounded-md border border-border p-3">
              <h4 className="mb-2 text-sm font-semibold">Last sync</h4>
              <ReportView r={st.lastRun} />
            </div>
          )}
        </section>
      </div>
    </Card>
  );
}
