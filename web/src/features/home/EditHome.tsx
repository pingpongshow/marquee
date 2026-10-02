import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { clsx } from "clsx";
import { ArrowDown, ArrowUp, Eye, EyeOff, GripVertical, Pin, PinOff, X } from "lucide-react";
import { useState } from "react";
import { Alert, Button, Dialog, Spinner } from "@/components/ui";
import { homeLayoutQuery, saveHomeLayout, usePinToHome, type HomeLayoutRow } from "./layout";

/** Reorder, hide and unpin Home rows (USER-12). Changes apply on Save; Reset restores the default. */
export function EditHomeDialog({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const layout = useQuery(homeLayoutQuery);
  const [rows, setRows] = useState<HomeLayoutRow[] | null>(null);
  const [drag, setDrag] = useState<number | null>(null);
  const list = rows ?? layout.data?.rows ?? [];
  const save = useMutation({
    mutationFn: (r: HomeLayoutRow[]) => saveHomeLayout(r),
    onSuccess: (saved) => {
      qc.setQueryData(homeLayoutQuery.queryKey, saved);
      qc.invalidateQueries({ queryKey: ["items", "hubs"] });
      onClose();
    },
  });
  const move = (from: number, to: number) => {
    if (to < 0 || to >= list.length || from === to) return;
    const next = [...list];
    const [row] = next.splice(from, 1);
    next.splice(to, 0, row!);
    setRows(next);
  };
  const update = (i: number, patch: Partial<HomeLayoutRow> | null) =>
    setRows(patch === null ? list.filter((_, j) => j !== i) : list.map((r, j) => (j === i ? { ...r, ...patch } : r)));

  return (
    <Dialog
      open
      wide
      onClose={onClose}
      title="Edit Home"
      footer={
        <>
          <Button variant="ghost" className="mr-auto" loading={save.isPending && save.variables?.length === 0} onClick={() => save.mutate([])}>
            Reset to default
          </Button>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" disabled={!rows} loading={save.isPending && !!save.variables?.length} onClick={() => rows && save.mutate(rows)}>
            Save
          </Button>
        </>
      }
    >
      <p className="mb-4 text-sm text-muted">Drag rows (or use the arrows) to reorder them, and hide the ones you don't want. Pin collections and playlists from their pages.</p>
      {save.isError && (
        <div className="mb-3">
          <Alert tone="error">{save.error.message}</Alert>
        </div>
      )}
      {layout.isPending && <Spinner />}
      <ol className="divide-y divide-border rounded-lg border border-border" aria-label="Home rows">
        {list.map((r, i) => (
          <li
            key={r.id}
            draggable
            onDragStart={() => setDrag(i)}
            onDragOver={(e) => e.preventDefault()}
            onDrop={(e) => {
              e.preventDefault();
              if (drag !== null) move(drag, i);
              setDrag(null);
            }}
            onDragEnd={() => setDrag(null)}
            className={clsx("flex items-center gap-2 px-3 py-2", drag === i && "opacity-40", r.hidden && "text-faint")}
          >
            <GripVertical className="size-4 shrink-0 cursor-grab text-faint" aria-hidden />
            <span className="min-w-0 flex-1 truncate">
              {r.title ?? r.id}
              {r.pinned && <Pin className="ml-1.5 inline size-3.5 text-accent" aria-label="Pinned" />}
              {r.hidden && <span className="ml-2 text-xs">Hidden</span>}
            </span>
            <Button size="sm" variant="ghost" aria-label={`Move ${r.title} up`} disabled={i === 0} onClick={() => move(i, i - 1)}>
              <ArrowUp className="size-4" />
            </Button>
            <Button size="sm" variant="ghost" aria-label={`Move ${r.title} down`} disabled={i === list.length - 1} onClick={() => move(i, i + 1)}>
              <ArrowDown className="size-4" />
            </Button>
            <Button size="sm" variant="ghost" aria-label={`${r.hidden ? "Show" : "Hide"} ${r.title}`} aria-pressed={!!r.hidden} onClick={() => update(i, { hidden: !r.hidden })}>
              {r.hidden ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
            </Button>
            {r.pinned && (
              <Button size="sm" variant="ghost" aria-label={`Remove ${r.title} from Home`} onClick={() => update(i, null)}>
                <X className="size-4" />
              </Button>
            )}
          </li>
        ))}
      </ol>
    </Dialog>
  );
}

/** "Pin to Home" / "Unpin from Home" on a collection or playlist page. */
export function PinToHomeButton({ kind, id }: { kind: "collection" | "playlist"; id: number }) {
  const pin = usePinToHome(kind, id);
  if (!pin.ready) return null;
  return (
    <Button variant="ghost" loading={pin.toggle.isPending} onClick={() => pin.toggle.mutate(!pin.pinned)} title={pin.toggle.error?.message}>
      {pin.pinned ? <PinOff className="size-4" /> : <Pin className="size-4" />} {pin.pinned ? "Unpin from Home" : "Pin to Home"}
    </Button>
  );
}
