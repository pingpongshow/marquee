import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { clsx } from "clsx";
import { Trash2 } from "lucide-react";
import { useState, type ReactNode } from "react";
import { api, unwrap } from "@/api/client";
import { settingsQuery } from "@/api/queries";
import type { components } from "@/api/schema.gen";
import { Alert, Button, Dialog } from "@/components/ui";
import { toast } from "@/components/Toast";
import { FileName } from "../browse/FileNames";
import { channelsLabel, codecLabel, formatTrackTime, hdrLabel, resolutionLabel, splitPath } from "../browse/format";

type IssueFile = components["schemas"]["IssueFile"];
type MediaFile = components["schemas"]["MediaFile"];

type Row = {
  label: string;
  value: (f: MediaFile, x: IssueFile) => ReactNode;
  /** A score where higher is better; the best file(s) are highlighted when they differ. */
  score?: (f: MediaFile) => number;
};

const gb = (bytes: number) =>
  bytes >= 1e8 ? `${(bytes / 1e9).toFixed(bytes >= 10e9 ? 1 : 2)} GB` : bytes >= 1e6 ? `${(bytes / 1e6).toFixed(1)} MB` : `${Math.max(1, Math.round(bytes / 1e3))} KB`;
const firstAudio = (f: MediaFile) => f.streams.find((s) => s.kind === "audio");
const subtitleCount = (f: MediaFile) => f.streams.filter((s) => s.kind === "subtitle").length;
const bitrate = (f: MediaFile) => f.bitrateKbps ?? (f.durationMs ? Math.round((f.size * 8) / f.durationMs) : 0);

const rows: Row[] = [
  {
    label: "Resolution",
    value: (f) => {
      const r = resolutionLabel(f);
      return r ? `${r} · ${f.width}×${f.height}` : "Unknown";
    },
    score: (f) => (f.width ?? 0) * (f.height ?? 0),
  },
  { label: "Video", value: (f) => codecLabel(f.videoCodec) || "—" },
  { label: "HDR", value: (f) => hdrLabel(f.hdrFormat) || "SDR", score: (f) => (f.hdrFormat ? 1 : 0) },
  {
    label: "Bitrate",
    value: (f) => {
      const b = bitrate(f);
      return b ? `${(b / 1000).toFixed(1)} Mbps` : "—";
    },
    score: bitrate,
  },
  {
    label: "Audio",
    value: (f) => {
      const a = firstAudio(f);
      if (!a) return "None";
      return [codecLabel(a.codec), channelsLabel(a.channels)].filter(Boolean).join(" ");
    },
    score: (f) => firstAudio(f)?.channels ?? 0,
  },
  { label: "Subtitles", value: (f) => String(subtitleCount(f)), score: subtitleCount },
  { label: "Duration", value: (f) => formatTrackTime(f.durationMs) || "—" },
  { label: "Container", value: (f) => f.container?.toUpperCase() || "—" },
  { label: "Size", value: (f) => gb(f.size) },
  { label: "Added", value: (_f, x) => new Date(x.addedAt).toLocaleDateString() },
];

/** Whether each file has the best value in a row (only when the files differ). */
export function best(files: IssueFile[], score: (f: MediaFile) => number) {
  const scores = files.map((x) => score(x.file));
  const max = Math.max(...scores);
  const differ = scores.some((s) => s !== max);
  return scores.map((s) => differ && s === max && max > 0);
}

/**
 * Library Health duplicates: every file of every copy side by side, with the better value in each
 * row highlighted, and (when the setting allows) a way to move one copy's file to the trash (ADM-11).
 */
export function DuplicateCompare({ files }: { files: IssueFile[] }) {
  const qc = useQueryClient();
  const settings = useQuery(settingsQuery);
  const allowed = !!settings.data?.library?.allowMediaDeletion;
  const [confirm, setConfirm] = useState<IssueFile | null>(null);
  const del = useMutation({
    mutationFn: (x: IssueFile) => unwrap(api.DELETE("/files/{fileId}", { params: { path: { fileId: x.file.id } } })),
    onSuccess: (_d, x) => {
      toast(`Moved “${x.file.path ? splitPath(x.file.path).name : x.itemTitle}” to the trash.`);
      setConfirm(null);
      qc.invalidateQueries({ queryKey: ["library-health"] });
      qc.invalidateQueries({ queryKey: ["items"] });
    },
  });
  const highlights = rows.map((r) => (r.score ? best(files, r.score) : null));
  const showVersion = files.some((x) => x.versionLabel);
  const last = files.length <= 1;
  return (
    <div className="mt-2 space-y-2">
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="w-full min-w-max border-collapse text-xs" aria-label="Compare files">
          <thead>
            <tr className="bg-surface-2 align-top">
              <th scope="col" className="w-24 px-3 py-2 text-left font-medium text-muted">
                File
              </th>
              {files.map((x) => (
                <th key={x.file.id} scope="col" className="max-w-[16rem] px-3 py-2 text-left font-normal" data-testid={`compare-file-${x.file.id}`}>
                  {x.file.path ? <FileName path={x.file.path} /> : <div className="text-sm font-medium">{x.itemTitle}</div>}
                  <div className="mt-0.5 truncate text-[11px] text-muted">{x.itemTitle}</div>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {showVersion && (
              <tr className="border-t border-border">
                <th scope="row" className="px-3 py-1.5 text-left font-medium text-muted">
                  Edition
                </th>
                {files.map((x) => (
                  <td key={x.file.id} className="px-3 py-1.5">
                    {x.versionLabel || "—"}
                  </td>
                ))}
              </tr>
            )}
            {rows.map((r, i) => (
              <tr key={r.label} className="border-t border-border">
                <th scope="row" className="px-3 py-1.5 text-left font-medium text-muted">
                  {r.label}
                </th>
                {files.map((x, j) => {
                  const hi = highlights[i]?.[j];
                  return (
                    <td key={x.file.id} className={clsx("px-3 py-1.5 tabular-nums", hi ? "font-semibold text-success" : "text-text/90")} data-best={hi ? "true" : undefined}>
                      {r.value(x.file, x)}
                    </td>
                  );
                })}
              </tr>
            ))}
            {allowed && (
              <tr className="border-t border-border">
                <td />
                {files.map((x) => (
                  <td key={x.file.id} className="px-3 py-2">
                    <Button
                      size="sm"
                      variant="ghost"
                      className="text-danger hover:text-danger"
                      disabled={last}
                      title={last ? "This is the last copy" : x.file.path}
                      data-testid={`delete-file-${x.file.id}`}
                      aria-label={`Delete ${x.file.path ? splitPath(x.file.path).name : x.itemTitle}`}
                      onClick={() => {
                        del.reset();
                        setConfirm(x);
                      }}
                    >
                      <Trash2 className="size-4" /> Delete…
                    </Button>
                  </td>
                ))}
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {settings.isSuccess && !allowed && (
        <p className="text-xs text-faint">Turn on 'Allow deleting media files' in Settings → Libraries to delete from here.</p>
      )}
      <Dialog
        open={!!confirm}
        onClose={() => setConfirm(null)}
        title="Delete this file?"
        footer={
          <>
            <Button variant="ghost" onClick={() => setConfirm(null)}>
              Cancel
            </Button>
            <Button variant="danger" loading={del.isPending} onClick={() => confirm && del.mutate(confirm)}>
              Move to trash
            </Button>
          </>
        }
      >
        {confirm && (
          <div className="space-y-3 text-sm text-muted">
            <div className="rounded-lg bg-surface-2 p-3">{confirm.file.path ? <FileName path={confirm.file.path} /> : confirm.itemTitle}</div>
            <p>
              The file moves to the <code className="text-text">.marquee-trash</code> folder in its library folder and is deleted for good after 30 days. Until then you
              can move it back by hand.
            </p>
            {del.isError && <Alert tone="error">{del.error.message}</Alert>}
          </div>
        )}
      </Dialog>
    </div>
  );
}
