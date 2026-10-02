import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, Play, RotateCcw, Save, Trash2 } from "lucide-react";
import { useState } from "react";
import { api, session, unwrap } from "@/api/client";
import type { Backup } from "@/api/types";
import { Alert, Badge, Button, Card, Dialog, Field, Input, Spinner } from "@/components/ui";
import { timeAgo } from "@/lib/time";
import { formatBytes } from "../browse/format";
import { SaveBar } from "./SaveBar";
import { useSectionDraft } from "./useSectionDraft";

const kindLabel: Record<Backup["kind"], string> = { scheduled: "Daily", manual: "Manual", "pre-migration": "Before upgrade", "pre-restore": "Before restore" };

/** Waits for the server to come back after a restart, then reloads the page. */
async function waitForRestart() {
  await new Promise((r) => setTimeout(r, 2000));
  for (let i = 0; i < 60; i++) {
    try {
      const r = await fetch("/api/v1/system/health", { cache: "no-store" });
      if (r.ok) {
        window.location.reload();
        return;
      }
    } catch {
      /* still restarting */
    }
    await new Promise((r) => setTimeout(r, 1000));
  }
}

function MaintenanceWindow() {
  const s = useSectionDraft("tasks");
  if (!s.draft) return <Spinner />;
  return (
    <>
      <Card title="Maintenance" description="Heavy jobs (metadata refresh, thumbnail and intro detection, backups) run inside this window.">
        <div className="grid gap-5 sm:grid-cols-2">
          <Field label="Window starts at">
            {(id) => <Input id={id} type="time" value={s.draft!.maintenanceWindowStart ?? "03:00"} onChange={(e) => s.update({ maintenanceWindowStart: e.target.value })} />}
          </Field>
          <Field label="Window length (hours)">
            {(id) => <Input id={id} type="number" min={1} max={12} value={s.draft!.maintenanceWindowHours ?? 4} onChange={(e) => s.update({ maintenanceWindowHours: Number(e.target.value) })} />}
          </Field>
        </div>
        <Field label="Database backups to keep" help="A backup is taken daily and before every upgrade.">
          {(id) => <Input id={id} type="number" min={1} max={60} value={s.draft!.backupRetention ?? 7} onChange={(e) => s.update({ backupRetention: Number(e.target.value) })} />}
        </Field>
      </Card>
      <SaveBar dirty={s.dirty} saving={s.saving} error={s.error} savedAt={s.savedAt} onSave={s.save} onReset={s.reset} />
    </>
  );
}

function Tasks() {
  const qc = useQueryClient();
  const tasks = useQuery({
    queryKey: ["tasks"],
    queryFn: () => unwrap(api.GET("/tasks")),
    refetchInterval: (q) => (q.state.data?.some((t) => t.running) ? 1500 : 15_000),
  });
  const run = useMutation({
    mutationFn: (taskId: string) => unwrap(api.POST("/tasks/{taskId}/run", { params: { path: { taskId } } })),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: ["tasks"] });
      qc.invalidateQueries({ queryKey: ["backups"] });
    },
  });
  return (
    <Card title="Tasks" description="Maintenance jobs and when they last ran.">
      {tasks.isPending && <Spinner />}
      {run.isError && <Alert tone="error">{run.error.message}</Alert>}
      <ul className="-my-2 divide-y divide-border">
        {tasks.data?.map((t) => (
          <li key={t.id} className="flex flex-wrap items-center gap-4 py-3">
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2 font-medium">
                {t.name}
                {t.running && <Badge tone="accent">Running</Badge>}
                {!t.running && t.lastRun?.status === "failed" && <Badge tone="danger">Failed</Badge>}
              </div>
              <div className="text-sm text-muted">{t.description}</div>
              <div className="mt-0.5 text-xs text-faint">
                {t.schedule} · Last run {timeAgo(t.lastRun?.finishedAt ?? t.lastRun?.startedAt)}
                {t.lastRun?.message ? ` · ${t.lastRun.message}` : ""}
              </div>
            </div>
            <Button size="sm" disabled={t.running} loading={run.isPending && run.variables === t.id} onClick={() => run.mutate(t.id)}>
              <Play className="size-4" /> Run now
            </Button>
          </li>
        ))}
      </ul>
    </Card>
  );
}

function Backups() {
  const qc = useQueryClient();
  const backups = useQuery({ queryKey: ["backups"], queryFn: () => unwrap(api.GET("/backups")) });
  const [restoring, setRestoring] = useState<Backup | null>(null);
  const [restarting, setRestarting] = useState(false);
  const create = useMutation({ mutationFn: () => unwrap(api.POST("/backups")), onSuccess: () => qc.invalidateQueries({ queryKey: ["backups"] }) });
  const del = useMutation({
    mutationFn: (name: string) => unwrap(api.DELETE("/backups/{name}", { params: { path: { name } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["backups"] }),
  });
  const restore = useMutation({
    mutationFn: (name: string) => unwrap(api.POST("/backups/{name}/restore", { params: { path: { name } } })),
    onSuccess: () => {
      setRestarting(true);
      void waitForRestart();
    },
  });
  // Downloads go through fetch so the auth header is sent, then save via a blob link.
  const download = async (name: string) => {
    const r = await fetch(`/api/v1/backups/${encodeURIComponent(name)}`, { headers: { Authorization: `Bearer ${session.token}` } });
    if (!r.ok) return;
    const url = URL.createObjectURL(await r.blob());
    const a = document.createElement("a");
    a.href = url;
    a.download = name;
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 10_000);
  };
  return (
    <Card
      title="Database backups"
      description="Copies of Marquee's database: libraries, users, watch history and settings. Media files aren't included."
      actions={
        <Button size="sm" variant="primary" loading={create.isPending} onClick={() => create.mutate()}>
          <Save className="size-4" /> Back up now
        </Button>
      }
    >
      {restarting && <Alert tone="info">Restoring the backup. The server is restarting; this page reloads when it's back.</Alert>}
      {(create.isError || del.isError || restore.isError) && <Alert tone="error">{(create.error ?? del.error ?? restore.error)?.message}</Alert>}
      {backups.isPending && <Spinner />}
      {backups.data?.length === 0 && <p className="text-sm text-muted">No backups yet. One is taken every day in the maintenance window.</p>}
      <ul className="-my-2 divide-y divide-border">
        {backups.data?.map((b) => (
          <li key={b.name} className="flex flex-wrap items-center gap-3 py-2.5">
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2 text-sm">
                <span className="font-medium">{new Date(b.createdAt).toLocaleString()}</span>
                <Badge>{kindLabel[b.kind]}</Badge>
              </div>
              <div className="text-xs text-faint">
                {b.name} · {formatBytes(b.size)}
              </div>
            </div>
            <Button size="sm" variant="ghost" onClick={() => void download(b.name)} aria-label={`Download ${b.name}`}>
              <Download className="size-4" />
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setRestoring(b)}>
              <RotateCcw className="size-4" /> Restore
            </Button>
            <Button size="sm" variant="ghost" onClick={() => del.mutate(b.name)} aria-label={`Delete ${b.name}`}>
              <Trash2 className="size-4" />
            </Button>
          </li>
        ))}
      </ul>
      <Dialog
        open={!!restoring}
        onClose={() => setRestoring(null)}
        title="Restore this backup?"
        footer={
          <>
            <Button variant="ghost" onClick={() => setRestoring(null)}>
              Cancel
            </Button>
            <Button
              variant="danger"
              loading={restore.isPending}
              onClick={() => {
                if (restoring) restore.mutate(restoring.name);
                setRestoring(null);
              }}
            >
              Restore and restart
            </Button>
          </>
        }
      >
        <p className="text-sm text-muted">
          Marquee will restart and go back to the state from {restoring && new Date(restoring.createdAt).toLocaleString()}. Anything that changed since then (watch history, playlists, settings) is lost. The current
          database is kept as a “Before restore” backup, so you can undo this.
        </p>
      </Dialog>
    </Card>
  );
}

export function ScheduledTasksSettings() {
  return (
    <div className="space-y-6">
      <MaintenanceWindow />
      <Tasks />
      <Backups />
    </div>
  );
}

export function RestartServer() {
  const [confirm, setConfirm] = useState(false);
  const [restarting, setRestarting] = useState(false);
  const restart = useMutation({
    mutationFn: () => unwrap(api.POST("/system/restart")),
    onSuccess: () => {
      setRestarting(true);
      void waitForRestart();
    },
  });
  return (
    <Card title="Restart" description="Stops all playback and restarts the server. Docker starts it again within a few seconds.">
      {restarting ? (
        <Alert tone="info">Restarting… this page reloads when the server is back.</Alert>
      ) : (
        <div>
          <Button onClick={() => setConfirm(true)} loading={restart.isPending}>
            <RotateCcw className="size-4" /> Restart server
          </Button>
        </div>
      )}
      <Dialog
        open={confirm}
        onClose={() => setConfirm(false)}
        title="Restart the server?"
        footer={
          <>
            <Button variant="ghost" onClick={() => setConfirm(false)}>
              Cancel
            </Button>
            <Button
              variant="danger"
              onClick={() => {
                setConfirm(false);
                restart.mutate();
              }}
            >
              Restart
            </Button>
          </>
        }
      >
        <p className="text-sm text-muted">Anyone watching or listening will be interrupted.</p>
      </Dialog>
    </Card>
  );
}
