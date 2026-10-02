import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Check, Lock, Pencil, RefreshCw, Search, Trash2, Unlock } from "lucide-react";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import type { ItemDetail } from "@/api/types";
import { Alert, Button, Dialog, Field, Input, Spinner } from "@/components/ui";

type Edit = components["schemas"]["ItemEdit"];
type LockField = NonNullable<Edit["unlock"]>[number];

function useItemUpdated() {
  const qc = useQueryClient();
  return (item: ItemDetail) => {
    qc.setQueryData(["items", item.id], item);
    qc.invalidateQueries({ queryKey: ["items"] });
    qc.invalidateQueries({ queryKey: ["libraries"] });
  };
}

function FixMatchDialog({ item, onClose }: { item: ItemDetail; onClose: () => void }) {
  const updated = useItemUpdated();
  const [title, setTitle] = useState(item.title);
  const [year, setYear] = useState(item.year ? String(item.year) : "");
  const [query, setQuery] = useState({ title: item.title, year: item.year });
  const cands = useQuery({
    queryKey: ["match", item.id, query],
    queryFn: () => unwrap(api.GET("/items/{itemId}/match", { params: { path: { itemId: item.id }, query: { title: query.title, year: query.year } } })),
  });
  const apply = useMutation({
    mutationFn: (id: string) => unwrap(api.PUT("/items/{itemId}/match", { params: { path: { itemId: item.id } }, body: { provider: "tmdb", id } })),
    onSuccess: (it) => {
      updated(it);
      onClose();
    },
  });
  const search = (e: FormEvent) => {
    e.preventDefault();
    setQuery({ title: title.trim(), year: year ? Number(year) : undefined });
  };
  return (
    <Dialog open wide onClose={onClose} title="Fix match">
      <form onSubmit={search} className="mb-4 flex gap-2">
        <Input aria-label="Title" value={title} onChange={(e) => setTitle(e.target.value)} />
        <Input aria-label="Year" placeholder="Year" inputMode="numeric" className="w-24" value={year} onChange={(e) => setYear(e.target.value.replace(/\D/g, "").slice(0, 4))} />
        <Button type="submit" variant="secondary">
          <Search className="size-4" /> Search
        </Button>
      </form>
      {(cands.isError || apply.isError) && <Alert tone="error">{(cands.error ?? apply.error)?.message}</Alert>}
      {cands.isFetching && <Spinner label="Searching TMDB" />}
      {cands.data?.length === 0 && <p className="py-6 text-center text-sm text-muted">No matches. Try a different title or remove the year.</p>}
      <ul className="space-y-2">
        {cands.data?.map((c) => (
          <li key={c.id}>
            <button
              disabled={apply.isPending}
              onClick={() => apply.mutate(c.id)}
              className="flex w-full gap-4 rounded-lg border border-border p-3 text-left hover:border-accent hover:bg-surface-2 disabled:opacity-60"
            >
              <span className="h-24 w-16 shrink-0 overflow-hidden rounded bg-surface-3">{c.posterUrl && <img src={c.posterUrl} alt="" loading="lazy" className="size-full object-cover" />}</span>
              <span className="min-w-0">
                <span className="flex items-center gap-2 font-medium">
                  {c.title} {c.year && <span className="text-muted">({c.year})</span>}
                  {c.current && (
                    <span className="flex items-center gap-1 rounded bg-success/15 px-1.5 text-[11px] text-success">
                      <Check className="size-3" /> Current
                    </span>
                  )}
                </span>
                {c.originalTitle && <span className="block text-xs text-faint">{c.originalTitle}</span>}
                {c.overview && <span className="mt-1 line-clamp-2 block text-sm text-muted">{c.overview}</span>}
              </span>
            </button>
          </li>
        ))}
      </ul>
      {apply.isPending && <Spinner label="Downloading metadata" />}
    </Dialog>
  );
}

const fields: { key: LockField; label: string; multiline?: boolean; type?: string }[] = [
  { key: "title", label: "Title" },
  { key: "sortTitle", label: "Sort title" },
  { key: "originalTitle", label: "Original title" },
  { key: "originallyAvailableAt", label: "Release date", type: "date" },
  { key: "contentRating", label: "Content rating" },
  { key: "studio", label: "Studio / network" },
  { key: "tagline", label: "Tagline" },
  { key: "summary", label: "Summary", multiline: true },
];

function currentValue(item: ItemDetail, key: LockField): string {
  switch (key) {
    case "sortTitle":
      return "";
    case "year":
      return item.year ? String(item.year) : "";
    case "originallyAvailableAt":
      return item.originallyAvailableAt ?? "";
    default:
      return ((item as Record<string, unknown>)[key] as string | undefined) ?? "";
  }
}

function EditDialog({ item, onClose }: { item: ItemDetail; onClose: () => void }) {
  const updated = useItemUpdated();
  const [values, setValues] = useState<Record<string, string>>({});
  const [unlock, setUnlock] = useState<LockField[]>([]);
  const locked = new Set(item.lockedFields);
  const save = useMutation({
    mutationFn: (body: Edit) => unwrap(api.PATCH("/items/{itemId}/metadata", { params: { path: { itemId: item.id } }, body })),
    onSuccess: (it) => {
      updated(it);
      onClose();
    },
  });
  const submit = () => {
    const body: Edit = { unlock: unlock.length ? unlock : undefined };
    for (const [k, v] of Object.entries(values)) (body as Record<string, unknown>)[k] = v;
    save.mutate(body);
  };
  const dirty = Object.keys(values).length > 0 || unlock.length > 0;
  return (
    <Dialog
      open
      wide
      onClose={onClose}
      title={`Edit ${item.title}`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" disabled={!dirty} loading={save.isPending} onClick={submit}>
            Save
          </Button>
        </>
      }
    >
      {save.isError && (
        <div className="mb-4">
          <Alert tone="error">{save.error.message}</Alert>
        </div>
      )}
      <p className="mb-4 text-sm text-muted">Edited fields are locked <Lock className="inline size-3.5" /> so metadata refreshes won't change them. Click a lock to hand a field back.</p>
      <div className="space-y-4">
        {fields.map((f) => {
          const isLocked = (locked.has(f.key) || f.key in values) && !unlock.includes(f.key);
          return (
            <Field key={f.key} label={f.label}>
              {(id) => (
                <div className="flex gap-2">
                  {f.multiline ? (
                    <textarea
                      id={id}
                      rows={5}
                      className="w-full rounded-md border border-border bg-surface-2 px-3 py-2 text-sm focus:border-accent focus:outline-none"
                      value={values[f.key] ?? currentValue(item, f.key)}
                      onChange={(e) => setValues({ ...values, [f.key]: e.target.value })}
                    />
                  ) : (
                    <Input id={id} type={f.type} value={values[f.key] ?? currentValue(item, f.key)} placeholder={f.key === "sortTitle" ? "Automatic" : undefined} onChange={(e) => setValues({ ...values, [f.key]: e.target.value })} />
                  )}
                  <button
                    type="button"
                    disabled={!isLocked}
                    onClick={() => {
                      const { [f.key]: _drop, ...rest } = values;
                      void _drop;
                      setValues(rest);
                      setUnlock([...unlock, f.key]);
                    }}
                    title={isLocked ? "Locked. Click to unlock." : "Not locked"}
                    aria-label={isLocked ? `Unlock ${f.label}` : `${f.label} not locked`}
                    className={isLocked ? "shrink-0 self-start rounded p-2 text-accent hover:bg-surface-2" : "shrink-0 self-start p-2 text-faint"}
                  >
                    {isLocked ? <Lock className="size-4" /> : <Unlock className="size-4" />}
                  </button>
                </div>
              )}
            </Field>
          );
        })}
      </div>
    </Dialog>
  );
}

/** Admin actions for an item: edit, fix match, refresh. */
export function ItemActions({ item }: { item: ItemDetail }) {
  const updated = useItemUpdated();
  const [open, setOpen] = useState(false);
  const [dialog, setDialog] = useState<"edit" | "match" | null>(null);
  const ref = useRef<HTMLDivElement>(null);
  const matchable = item.type === "movie" || item.type === "show";
  const navigate = useNavigate();
  const removeCollection = useMutation({
    mutationFn: () => unwrap(api.DELETE("/collections/{collectionId}", { params: { path: { collectionId: item.id } } })),
    onSuccess: () => navigate({ to: "/library/$libraryId", params: { libraryId: String(item.libraryId) }, search: { show: "collections" } }),
  });
  const refresh = useMutation({
    mutationFn: () => unwrap(api.POST("/items/{itemId}/refresh", { params: { path: { itemId: item.id } } })),
    onSuccess: updated,
  });
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false);
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [open]);
  const itemCls = "flex w-full items-center gap-3 px-4 py-2 text-left text-sm hover:bg-surface-2 disabled:opacity-50";
  return (
    <>
      <div className="relative" ref={ref}>
        <Button variant="secondary" size="sm" onClick={() => setOpen((v) => !v)} aria-label="Edit, fix match or refresh" title="Edit, fix match or refresh" aria-expanded={open} loading={refresh.isPending}>
          <Pencil className="size-4" />
        </Button>
        {open && (
          <div className="absolute left-0 z-30 mt-2 w-52 overflow-hidden rounded-lg border border-border bg-surface py-1 shadow-2xl" onClick={() => setOpen(false)}>
            <button className={itemCls} onClick={() => setDialog("edit")}>
              <Pencil className="size-4" /> Edit
            </button>
            {matchable && (
              <button className={itemCls} onClick={() => setDialog("match")}>
                <Search className="size-4" /> Fix match…
              </button>
            )}
            {matchable && item.matchState === "matched" && (
              <button className={itemCls} onClick={() => refresh.mutate()}>
                <RefreshCw className="size-4" /> Refresh metadata
              </button>
            )}
            {item.type === "collection" && (
              <button
                className={itemCls + " text-danger"}
                onClick={() => window.confirm(`Delete the collection “${item.title}”? Its titles stay in the library.`) && removeCollection.mutate()}
              >
                <Trash2 className="size-4" /> Delete collection
              </button>
            )}
          </div>
        )}
      </div>
      {refresh.isError && <span className="text-sm text-danger">{refresh.error.message}</span>}
      {dialog === "match" && <FixMatchDialog item={item} onClose={() => setDialog(null)} />}
      {dialog === "edit" && <EditDialog item={item} onClose={() => setDialog(null)} />}
    </>
  );
}
