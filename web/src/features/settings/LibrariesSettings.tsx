import { useQuery } from "@tanstack/react-query";
import { Folder, Pencil, Plus, RefreshCw, Square, Trash2 } from "lucide-react";
import { useState } from "react";
import { librariesQuery, useCancelScan, useDeleteLibrary, useScanAll, useScanLibrary } from "@/api/queries";
import type { Library } from "@/api/types";
import { libraryIcons } from "@/app/Shell";
import { Alert, Button, Card, Dialog, Field, Input, Spinner, StringListEditor, Toggle } from "@/components/ui";
import { LibraryDialog } from "./LibraryDialog";
import { SaveBar } from "./SaveBar";
import { useSectionDraft } from "./useSectionDraft";

const typeLabels: Record<Library["type"], string> = { movies: "Movies", shows: "TV Shows", anime: "Anime", music: "Music", videos: "Other Videos", photos: "Photos" };

function ScanStatus({ lib }: { lib: Library }) {
  const p = lib.scanProgress;
  if (lib.scanStatus === "queued") return <div className="mt-2 text-xs text-accent">Waiting to scan…</div>;
  if (lib.scanStatus === "scanning") {
    const pct = p && p.total > 0 ? Math.round((p.done / p.total) * 100) : null;
    const label =
      p?.phase === "walking"
        ? "Finding files…"
        : p?.phase === "cleanup"
          ? "Finishing up…"
          : p?.phase === "metadata"
            ? `Getting metadata ${p.done} of ${p.total}`
            : `Scanning ${p?.done ?? 0} of ${p?.total ?? 0}`;
    return (
      <div className="mt-2 space-y-1">
        <div className="flex justify-between text-xs text-accent">
          <span>{label}</span>
          {pct !== null && <span>{pct}%</span>}
        </div>
        <div className="h-1.5 overflow-hidden rounded-full bg-surface-3" role="progressbar" aria-valuenow={pct ?? undefined} aria-valuemin={0} aria-valuemax={100}>
          <div className={pct === null ? "h-full w-1/3 animate-pulse bg-accent" : "h-full bg-accent transition-all"} style={pct === null ? undefined : { width: `${pct}%` }} />
        </div>
        {p?.current && <div className="truncate text-xs text-faint">{p.current}</div>}
      </div>
    );
  }
  return (
    <div className="mt-1 text-xs text-faint">
      {lib.itemCount} items · {lib.lastScannedAt ? `Scanned ${new Date(lib.lastScannedAt).toLocaleString()}` : "Not scanned yet"}
      {lib.lastScanError && <div className="text-danger">Last scan failed: {lib.lastScanError}</div>}
      {lib.lastScanResult && !lib.lastScanError && <div>{lib.lastScanResult}</div>}
    </div>
  );
}

export function LibrariesSettings() {
  const libraries = useQuery(librariesQuery);
  const del = useDeleteLibrary();
  const scan = useScanLibrary();
  const cancel = useCancelScan();
  const scanAll = useScanAll();
  const [dialog, setDialog] = useState<{ library?: Library; key: number } | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<Library | null>(null);
  const scanning = useSectionDraft("library");

  return (
    <div className="space-y-6">
      <Card
        title="Libraries"
        actions={
          <div className="flex gap-2">
            {!!libraries.data?.length && (
              <Button size="sm" variant="ghost" onClick={() => scanAll.mutate()} loading={scanAll.isPending}>
                <RefreshCw className="size-4" /> Scan all
              </Button>
            )}
            <Button variant="primary" size="sm" onClick={() => setDialog({ key: Date.now() })}>
              <Plus className="size-4" /> Add library
            </Button>
          </div>
        }
      >
        {libraries.isPending && <Spinner />}
        {libraries.isError && <Alert tone="error">{libraries.error.message}</Alert>}
        {libraries.data?.length === 0 && <p className="text-sm text-muted">No libraries yet. Add one to get started.</p>}
        <ul className="-my-2 divide-y divide-border">
          {libraries.data?.map((lib) => {
            const Icon = libraryIcons[lib.type];
            return (
              <li key={lib.id} className="flex items-start gap-4 py-4">
                <div className="flex size-10 shrink-0 items-center justify-center rounded-md bg-surface-3">
                  <Icon className="size-5 text-accent" aria-hidden />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-baseline gap-2">
                    <span className="font-medium">{lib.name}</span>
                    <span className="text-xs text-faint">{typeLabels[lib.type]}</span>
                  </div>
                  <ul className="mt-1 space-y-0.5">
                    {lib.paths.map((p) => (
                      <li key={p} className="flex items-center gap-1.5 truncate font-mono text-xs text-muted">
                        <Folder className="size-3 shrink-0" aria-hidden /> {p}
                      </li>
                    ))}
                  </ul>
                  <ScanStatus lib={lib} />
                </div>
                <div className="flex shrink-0 gap-1">
                  {lib.scanStatus === "idle" ? (
                    <Button size="sm" variant="ghost" aria-label={`Scan ${lib.name}`} title="Scan library files" onClick={() => scan.mutate(lib.id)}>
                      <RefreshCw className="size-4" />
                    </Button>
                  ) : (
                    <Button size="sm" variant="ghost" aria-label={`Stop scanning ${lib.name}`} title="Stop scan" onClick={() => cancel.mutate(lib.id)}>
                      <Square className="size-4" />
                    </Button>
                  )}
                  <Button size="sm" variant="ghost" aria-label={`Edit ${lib.name}`} onClick={() => setDialog({ library: lib, key: Date.now() })}>
                    <Pencil className="size-4" />
                  </Button>
                  <Button size="sm" variant="ghost" aria-label={`Delete ${lib.name}`} onClick={() => setConfirmDelete(lib)}>
                    <Trash2 className="size-4" />
                  </Button>
                </div>
              </li>
            );
          })}
        </ul>
      </Card>

      {scanning.draft && (
        <>
          <Card title="Scanning" description="Applies to all libraries.">
            <Toggle label="Scan automatically when files change" help="Watches library folders and picks up new, renamed and deleted files within seconds." checked={!!scanning.draft.watchFilesystem} onChange={(v) => scanning.update({ watchFilesystem: v })} />
            <Toggle label="Scan all libraries when the server starts" checked={!!scanning.draft.scanOnStartup} onChange={(v) => scanning.update({ scanOnStartup: v })} />
            <Toggle
              label="Find intros and credits"
              help="Compares the audio of episodes in each season, in the maintenance window, so Skip Intro and Skip Credits work on new episodes. Markers imported from Plex are kept."
              checked={!!scanning.draft.detectIntros}
              onChange={(v) => scanning.update({ detectIntros: v })}
            />
            <Toggle
              label="Make seek previews"
              help="Thumbnails shown while seeking through videos, made in the maintenance window. Uses roughly 3–7 MB per movie; a large library takes a few nights."
              checked={!!scanning.draft.trickplay}
              onChange={(v) => scanning.update({ trickplay: v })}
            />
            <Field label="Mark as watched after (%)" help="Progress past this point counts as watched.">
              {(id) => <Input id={id} type="number" min={50} max={100} value={scanning.draft!.watchedThresholdPercent ?? 90} onChange={(e) => scanning.update({ watchedThresholdPercent: Number(e.target.value) })} />}
            </Field>
            <Field label="Ignore files matching" help="Temporary and partial downloads are skipped. Patterns use * and ? wildcards.">
              {() => <StringListEditor values={scanning.draft!.ignorePatterns ?? []} onChange={(v) => scanning.update({ ignorePatterns: v })} placeholder="*_staging.*" addLabel="Add pattern" />}
            </Field>
            <Field label="Folders the folder picker can browse" help="Paths inside the Marquee container (e.g. /media).">
              {() => <StringListEditor values={scanning.draft!.browseRoots ?? []} onChange={(v) => scanning.update({ browseRoots: v })} placeholder="/media" addLabel="Add folder" />}
            </Field>
          </Card>
          <SaveBar dirty={scanning.dirty} saving={scanning.saving} error={scanning.error} savedAt={scanning.savedAt} onSave={scanning.save} onReset={scanning.reset} />
        </>
      )}

      {dialog && <LibraryDialog key={dialog.key} open library={dialog.library} onClose={() => setDialog(null)} />}

      <Dialog
        open={!!confirmDelete}
        onClose={() => setConfirmDelete(null)}
        title="Delete library?"
        footer={
          <>
            <Button variant="ghost" onClick={() => setConfirmDelete(null)}>
              Cancel
            </Button>
            <Button
              variant="danger"
              loading={del.isPending}
              onClick={() => confirmDelete && del.mutate(confirmDelete.id, { onSuccess: () => setConfirmDelete(null) })}
            >
              Delete library
            </Button>
          </>
        }
      >
        <p className="text-sm text-muted">
          <strong className="text-text">{confirmDelete?.name}</strong> and its metadata, artwork and watch history will be removed from Marquee. Your media files are not touched.
        </p>
        {del.isError && (
          <div className="mt-3">
            <Alert tone="error">{del.error.message}</Alert>
          </div>
        )}
      </Dialog>
    </div>
  );
}
