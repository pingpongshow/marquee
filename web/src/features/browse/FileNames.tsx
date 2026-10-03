import { FileVideo } from "lucide-react";
import type { ItemDetail } from "@/api/types";
import { splitPath } from "./format";

/** One file: its name, with the folder in small text below; hover shows the full path. */
export function FileName({ path, className }: { path: string; className?: string }) {
  const { name, folder } = splitPath(path);
  return (
    <div className={className ? `min-w-0 ${className}` : "min-w-0"} title={path}>
      <div className="truncate text-sm font-medium text-text" data-testid="file-name">
        {name}
      </div>
      {/* Long folders lose their start, not the end that tells copies apart. */}
      {folder && (
        <div className="truncate text-left font-mono text-[11px] text-faint [direction:rtl]">
          {"\u200E"}
          {folder}
          {"\u200E"}
        </div>
      )}
    </div>
  );
}

/** The files behind an item (admins get paths), so Fix Match shows what is being matched. */
export function ItemFiles({ item }: { item: ItemDetail }) {
  const rows = item.versions.flatMap((v) =>
    v.files
      .map((f, i) => ({ id: f.id, path: f.path, label: item.versions.length > 1 ? v.label : "", part: v.files.length > 1 ? i + 1 : 0 }))
      .filter((r): r is typeof r & { path: string } => !!r.path),
  );
  if (!rows.length) return null;
  return (
    <ul className="mb-4 space-y-2 rounded-lg bg-surface-2 p-3" aria-label="Files">
      {rows.map((r) => (
        <li key={r.id} className="flex items-start gap-2">
          <FileVideo className="mt-0.5 size-4 shrink-0 text-muted" aria-hidden />
          <FileName path={r.path} className="flex-1" />
          {(r.label || r.part > 0) && (
            <span className="shrink-0 text-xs text-muted">{[r.label, r.part ? `Part ${r.part}` : ""].filter(Boolean).join(" · ")}</span>
          )}
        </li>
      ))}
    </ul>
  );
}
