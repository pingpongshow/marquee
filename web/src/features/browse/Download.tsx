import { ChevronDown, Download as DownloadIcon } from "lucide-react";
import { useState, type MouseEvent } from "react";
import { session } from "@/api/client";
import type { ItemDetail } from "@/api/types";
import { Menu, MenuItem } from "@/components/Menu";
import { toast } from "@/components/Toast";
import { Button, Dialog } from "@/components/ui";
import { formatBytes, versionLabel } from "./format";

const auth = () => `token=${encodeURIComponent(session.token ?? "")}`;

/** Original file of a movie or episode; fileId picks a version (the best one otherwise). */
export function originalDownloadUrl(itemId: number, fileId?: number) {
  return `/api/v1/download/original/${itemId}?${fileId ? `fileId=${fileId}&` : ""}${auth()}`;
}

/** Every episode of a season or show as one uncompressed zip. */
export function zipDownloadUrl(itemId: number) {
  return `/api/v1/download/zip/${itemId}?${auth()}`;
}

/**
 * Checks the account may download from here before handing the link to the browser, so a
 * refusal shows a message instead of a broken download. Probes the single-file endpoint with
 * HEAD (cheap even for a show: the permission check comes before the file lookup).
 */
async function allowed(itemId: number): Promise<boolean> {
  try {
    const r = await fetch(`/api/v1/download/original/${itemId}?${auth()}`, { method: "HEAD" });
    if (r.status === 403) {
      toast("Downloading away from home isn't allowed for this account.", { tone: "error" });
      return false;
    }
    if (r.status === 401) {
      toast("Your session has ended. Sign in again to download.", { tone: "error" });
      return false;
    }
  } catch {
    /* offline or blocked: let the browser try and report it */
  }
  return true;
}

function start(href: string) {
  const a = document.createElement("a");
  a.href = href;
  a.download = "";
  document.body.appendChild(a);
  a.click();
  a.remove();
}

/** Intercepts a download link's click to run the permission check first. */
function guarded(itemId: number, href: string, before?: () => boolean) {
  return (e: MouseEvent<HTMLAnchorElement>) => {
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    if (before && !before()) return;
    void allowed(itemId).then((ok) => ok && start(href));
  };
}

const linkClass =
  "inline-flex h-10 items-center justify-center gap-2 rounded-md bg-surface-3 px-4 text-sm font-medium text-text hover:bg-border";

/** "Download" for a movie or episode, with the file size; a version menu when there are several. */
export function DownloadOriginal({ item }: { item: ItemDetail }) {
  const versions = item.versions.filter((v) => v.files.length > 0);
  if (!item.available || versions.length === 0) return null;
  // The server picks the tallest version when none is given; show that one's size.
  const best = [...versions].sort((a, b) => (b.files[0]?.height ?? 0) - (a.files[0]?.height ?? 0))[0];
  const size = best?.files[0]?.size;
  const href = originalDownloadUrl(item.id);
  return (
    <span className="inline-flex items-center gap-1">
      <a href={href} download onClick={guarded(item.id, href)} className={linkClass} data-testid="download-original">
        <DownloadIcon className="size-4" aria-hidden /> Download
        {size ? <span className="text-muted">{formatBytes(size)}</span> : null}
      </a>
      {versions.length > 1 && (
        <Menu
          label="Download version"
          trigger={<ChevronDown className="size-4" aria-hidden />}
        >
          {versions.map((v) => {
            const f = v.files[0]!;
            const url = originalDownloadUrl(item.id, f.id);
            return (
              <MenuItem key={v.id} icon={<DownloadIcon />} onClick={() => void allowed(item.id).then((ok) => ok && start(url))}>
                {versionLabel(v)}
                {f.size ? <span className="ml-auto pl-3 text-muted">{formatBytes(f.size)}</span> : null}
              </MenuItem>
            );
          })}
        </Menu>
      )}
    </span>
  );
}

/** Small icon link on an episode row. */
export function EpisodeDownloadLink({ itemId, title }: { itemId: number; title: string }) {
  const href = originalDownloadUrl(itemId);
  return (
    <a
      href={href}
      download
      onClick={guarded(itemId, href)}
      className="shrink-0 rounded p-2 text-muted hover:bg-surface-3 hover:text-text"
      aria-label={`Download ${title}`}
      title="Download"
      data-testid="download-episode"
    >
      <DownloadIcon className="size-4" aria-hidden />
    </a>
  );
}

/** "Download season" / "Download all episodes": confirms the size of the job, then a zip. */
export function DownloadZip({ item }: { item: ItemDetail }) {
  const [confirming, setConfirming] = useState(false);
  if (item.leafCount === 0) return null;
  const href = zipDownloadUrl(item.id);
  const label = item.type === "show" ? "Download all episodes" : "Download season";
  const count = `${item.leafCount} episode${item.leafCount === 1 ? "" : "s"}`;
  return (
    <>
      <a
        href={href}
        download
        onClick={(e) => {
          if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
          e.preventDefault();
          setConfirming(true);
        }}
        className={linkClass}
        data-testid="download-zip"
      >
        <DownloadIcon className="size-4" aria-hidden /> {label}
      </a>
      <Dialog
        open={confirming}
        onClose={() => setConfirming(false)}
        title={label}
        footer={
          <>
            <Button variant="ghost" onClick={() => setConfirming(false)}>
              Cancel
            </Button>
            <Button
              variant="primary"
              onClick={() => {
                setConfirming(false);
                void allowed(item.id).then((ok) => ok && start(href));
              }}
            >
              Download {count}
            </Button>
          </>
        }
      >
        <p className="text-sm text-text/90">
          {count} of {item.type === "show" ? item.title : [item.parentTitle, item.title].filter(Boolean).join(" · ")} as one zip of
          the original files{item.type === "show" ? ", with a folder per season" : ""}.
        </p>
        <p className="mt-2 text-sm text-muted">Video files are large, so this can take a while. The zip isn't compressed.</p>
      </Dialog>
    </>
  );
}
