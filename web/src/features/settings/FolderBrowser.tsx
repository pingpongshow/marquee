import { useQuery } from "@tanstack/react-query";
import { ChevronLeft, Folder, FolderOpen } from "lucide-react";
import { useState } from "react";
import { browseQuery } from "@/api/queries";
import { Alert, Button, Spinner } from "@/components/ui";

/** Browses folders on the server (inside the configured browse roots) and picks one. */
export function FolderBrowser({ onPick, initialPath = null }: { onPick: (path: string) => void; initialPath?: string | null }) {
  const [path, setPath] = useState<string | null>(initialPath);
  const listing = useQuery(browseQuery(path));

  return (
    <div className="overflow-hidden rounded-md border border-border">
      <div className="flex items-center gap-2 border-b border-border bg-surface-2 px-3 py-2">
        <Button
          size="sm"
          variant="ghost"
          aria-label="Up one folder"
          disabled={path === null}
          onClick={() => setPath(listing.data?.parent ?? null)}
        >
          <ChevronLeft className="size-4" />
        </Button>
        <span className="min-w-0 flex-1 truncate font-mono text-sm text-muted" title={path ?? undefined}>
          {path ?? "Media roots"}
        </span>
        {path && (
          <Button size="sm" variant="primary" onClick={() => onPick(path)}>
            Use this folder
          </Button>
        )}
      </div>
      <div className="max-h-64 overflow-y-auto">
        {listing.isPending && <Spinner />}
        {listing.isError && (
          <div className="p-3">
            <Alert tone="error">{listing.error.message}</Alert>
          </div>
        )}
        {listing.data?.entries.length === 0 && <p className="p-4 text-sm text-faint">No subfolders.</p>}
        <ul>
          {listing.data?.entries.map((e) => (
            <li key={e.path}>
              <button
                type="button"
                onClick={() => setPath(e.path)}
                className="flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm hover:bg-surface-2"
              >
                {path === null ? <FolderOpen className="size-4 text-accent" aria-hidden /> : <Folder className="size-4 text-faint" aria-hidden />}
                <span className="truncate">{e.name}</span>
              </button>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
