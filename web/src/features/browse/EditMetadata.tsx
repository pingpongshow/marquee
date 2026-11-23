import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { clsx } from "clsx";
import { Check, ImagePlus, Link2, Lock, RotateCcw, Unlock, X } from "lucide-react";
import { useRef, useState, type KeyboardEvent } from "react";
import { api, ApiError, imageUrl, session, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import type { ItemDetail } from "@/api/types";
import { Alert, Button, Dialog, Field, Input, Spinner } from "@/components/ui";

/**
 * The full metadata editor (META-13), like Plex's: General details, Tags (genres, and styles and
 * moods for music, countries for films and shows) and each kind of artwork. Edited details and
 * tag lists are locked so metadata refreshes leave them alone; a lock hands one back. Artwork
 * changes apply at once: one of the item's own, one of TMDB's alternatives, a link or an upload,
 * kept through refreshes until set back to automatic.
 */

type Edit = components["schemas"]["ItemEdit"];
type LockField = NonNullable<Edit["unlock"]>[number];
type ArtworkKind = components["schemas"]["ArtworkKind"];
type ArtworkOption = components["schemas"]["ArtworkOption"];

type TextField = { key: LockField; label: string; multiline?: boolean; type?: string; placeholder?: string };

const isMusic = (t: ItemDetail["type"]) => t === "artist" || t === "album" || t === "track";

/** The details each kind of item has. */
function generalFields(item: ItemDetail): TextField[] {
  const t = item.type;
  const title: TextField = { key: "title", label: t === "artist" ? "Name" : "Title" };
  const sort: TextField = { key: "sortTitle", label: t === "artist" ? "Sort name" : "Sort title", placeholder: "Automatic" };
  const summary: TextField = { key: "summary", label: t === "artist" ? "Biography" : "Summary", multiline: true };
  const date: TextField = { key: "originallyAvailableAt", label: t === "episode" ? "Air date" : "Release date", type: "date" };
  switch (t) {
    case "track":
      return [title, { key: "index", label: "Track number", type: "number" }, { key: "artistCredit", label: "Artist" }];
    case "album":
      return [title, sort, { key: "artistCredit", label: "Album artist" }, date, { key: "studio", label: "Record label" }, summary];
    case "artist":
      return [title, sort, summary];
    case "season":
      return [title, { key: "index", label: "Season number", type: "number" }, summary];
    case "episode":
      return [title, { key: "index", label: "Episode number", type: "number" }, date, { key: "contentRating", label: "Content rating" }, summary];
    case "show":
      return [title, sort, { key: "originalTitle", label: "Original title" }, date, { key: "contentRating", label: "Content rating" },
        { key: "studio", label: "Network" }, { key: "tagline", label: "Tagline" }, summary];
    default:
      return [title, sort, { key: "originalTitle", label: "Original title" }, date, { key: "contentRating", label: "Content rating" },
        { key: "studio", label: "Studio" }, { key: "tagline", label: "Tagline" }, summary];
  }
}

type TagKey = "genres" | "styles" | "moods" | "countries";

function tagFields(item: ItemDetail): { key: TagKey; label: string }[] {
  const t = item.type;
  if (t === "season" || t === "episode") return [];
  if (isMusic(t)) return t === "track" ? [{ key: "genres", label: "Genres" }, { key: "moods", label: "Moods" }] : [
    { key: "genres", label: "Genres" }, { key: "styles", label: "Styles" }, { key: "moods", label: "Moods" }];
  return [{ key: "genres", label: "Genres" }, { key: "countries", label: "Countries" }];
}

function artworkTabs(item: ItemDetail): { kind: ArtworkKind; label: string }[] {
  switch (item.type) {
    case "album":
      return [{ kind: "poster", label: "Cover" }];
    case "track":
      return [];
    case "artist":
      return [{ kind: "poster", label: "Picture" }, { kind: "backdrop", label: "Background" }];
    case "episode":
      return [{ kind: "thumb", label: "Thumbnail" }];
    case "season":
      return [{ kind: "poster", label: "Poster" }];
    case "collection":
      return [{ kind: "poster", label: "Poster" }, { kind: "backdrop", label: "Background" }];
    default:
      return [{ kind: "poster", label: "Poster" }, { kind: "backdrop", label: "Background" }, { kind: "logo", label: "Logo" }];
  }
}

function currentText(item: ItemDetail, key: LockField): string {
  const it = item as Record<string, unknown>;
  switch (key) {
    case "sortTitle":
      return "";
    case "index":
      return item.index != null ? String(item.index) : "";
    case "originallyAvailableAt":
      return item.originallyAvailableAt ?? "";
    case "summary":
      return item.summary ?? "";
    default:
      return (it[key] as string | undefined) ?? "";
  }
}

function useItemUpdated() {
  const qc = useQueryClient();
  return (item: ItemDetail) => {
    qc.setQueryData(["items", item.id], item);
    qc.invalidateQueries({ queryKey: ["items"] });
    qc.invalidateQueries({ queryKey: ["libraries"] });
    qc.invalidateQueries({ queryKey: ["hubs"] });
  };
}

export function EditMetadataDialog({ item, onClose }: { item: ItemDetail; onClose: () => void }) {
  const updated = useItemUpdated();
  const fields = generalFields(item);
  const tags = tagFields(item);
  const art = artworkTabs(item);
  const tabs = [
    { id: "general", label: "General" },
    ...(tags.length ? [{ id: "tags", label: "Tags" }] : []),
    ...art.map((a) => ({ id: a.kind, label: a.label })),
  ];
  const [tab, setTab] = useState("general");
  const [values, setValues] = useState<Partial<Record<LockField, string>>>({});
  const [tagValues, setTagValues] = useState<Partial<Record<TagKey, string[]>>>({});
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
    const b = body as Record<string, unknown>;
    for (const [k, v] of Object.entries(values)) {
      // A number or date can't be sent empty (the server refuses the whole save): leave it out.
      if (k === "index") b[k] = v === "" ? undefined : Number(v);
      else if (k === "originallyAvailableAt" && v === "") continue;
      else b[k] = v;
    }
    for (const [k, v] of Object.entries(tagValues)) b[k] = v;
    save.mutate(body);
  };
  const dirty = Object.keys(values).length > 0 || Object.keys(tagValues).length > 0 || unlock.length > 0;
  const lockButton = (key: LockField, label: string, edited: boolean, drop: () => void) => {
    const isLocked = (locked.has(key) || edited) && !unlock.includes(key);
    return (
      <button
        type="button"
        disabled={!isLocked}
        onClick={() => {
          drop();
          setUnlock([...unlock, key]);
        }}
        title={isLocked ? "Locked: metadata refreshes leave it alone. Click to hand it back." : "Not locked"}
        aria-label={isLocked ? `Unlock ${label}` : `${label} not locked`}
        className={isLocked ? "shrink-0 self-start rounded p-2 text-accent hover:bg-surface-2" : "shrink-0 self-start p-2 text-faint"}
      >
        {isLocked ? <Lock className="size-4" /> : <Unlock className="size-4" />}
      </button>
    );
  };
  const onArtTab = art.some((a) => a.kind === tab);
  return (
    <Dialog
      open
      wide
      onClose={onClose}
      title={`Edit ${item.title}`}
      footer={
        onArtTab ? (
          <Button variant="ghost" onClick={onClose}>
            Done
          </Button>
        ) : (
          <>
            <Button variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button variant="primary" disabled={!dirty} loading={save.isPending} onClick={submit}>
              Save
            </Button>
          </>
        )
      }
    >
      <div className="mb-5 flex flex-wrap rounded-md bg-surface-2 p-0.5 text-sm" role="tablist" aria-label="Edit">
        {tabs.map((t) => (
          <button
            key={t.id}
            role="tab"
            type="button"
            aria-selected={tab === t.id}
            onClick={() => setTab(t.id)}
            className={clsx("rounded px-3 py-1", tab === t.id ? "bg-surface-3 text-text" : "text-muted hover:text-text")}
          >
            {t.label}
          </button>
        ))}
      </div>
      {save.isError && (
        <div className="mb-4">
          <Alert tone="error">{save.error.message}</Alert>
        </div>
      )}
      {tab === "general" && (
        <>
          <p className="mb-4 text-sm text-muted">
            Edited details are locked <Lock className="inline size-3.5" /> so metadata refreshes won't change them. Click a lock to hand one back.
          </p>
          <div className="space-y-4">
            {fields.map((f) => (
              <Field key={f.key} label={f.label}>
                {(id) => (
                  <div className="flex gap-2">
                    {f.multiline ? (
                      <textarea
                        id={id}
                        rows={6}
                        className="w-full rounded-md border border-border bg-surface-2 px-3 py-2 text-sm focus:border-accent focus:outline-none"
                        value={values[f.key] ?? currentText(item, f.key)}
                        onChange={(e) => setValues({ ...values, [f.key]: e.target.value })}
                      />
                    ) : (
                      <Input
                        id={id}
                        type={f.type}
                        min={f.type === "number" ? 0 : undefined}
                        value={values[f.key] ?? currentText(item, f.key)}
                        placeholder={f.placeholder}
                        onChange={(e) => setValues({ ...values, [f.key]: e.target.value })}
                      />
                    )}
                    {lockButton(f.key, f.label, f.key in values, () => {
                      const { [f.key]: _drop, ...rest } = values;
                      void _drop;
                      setValues(rest);
                    })}
                  </div>
                )}
              </Field>
            ))}
          </div>
        </>
      )}
      {tab === "tags" && (
        <>
          <p className="mb-4 text-sm text-muted">Type and press Enter to add; edited lists are locked like details.</p>
          <div className="space-y-5">
            {tags.map((t) => (
              <Field key={t.key} label={t.label}>
                {() => (
                  <div className="flex gap-2">
                    <TagInput
                      label={t.label}
                      values={tagValues[t.key] ?? (item[t.key] as string[] | undefined) ?? []}
                      onChange={(v) => setTagValues({ ...tagValues, [t.key]: v })}
                    />
                    {lockButton(t.key, t.label, t.key in tagValues, () => {
                      const { [t.key]: _drop, ...rest } = tagValues;
                      void _drop;
                      setTagValues(rest);
                    })}
                  </div>
                )}
              </Field>
            ))}
          </div>
        </>
      )}
      {art.map((a) => tab === a.kind && <ArtworkPicker key={a.kind} item={item} kind={a.kind} label={a.label} onChanged={updated} />)}
    </Dialog>
  );
}

/** A list of tags as removable chips, with a box to add more. */
function TagInput({ label, values, onChange }: { label: string; values: string[]; onChange: (v: string[]) => void }) {
  const [draft, setDraft] = useState("");
  const add = () => {
    const v = draft.trim();
    if (v && !values.some((x) => x.toLowerCase() === v.toLowerCase())) onChange([...values, v]);
    setDraft("");
  };
  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" || e.key === ",") {
      e.preventDefault();
      add();
    } else if (e.key === "Backspace" && !draft && values.length) {
      onChange(values.slice(0, -1));
    }
  };
  return (
    <div className="flex min-h-10 w-full flex-wrap items-center gap-1.5 rounded-md border border-border bg-surface-2 px-2 py-1.5 focus-within:border-accent">
      {values.map((v) => (
        <span key={v} className="flex items-center gap-1 rounded bg-surface-3 px-2 py-0.5 text-sm">
          {v}
          <button type="button" aria-label={`Remove ${v}`} className="text-muted hover:text-text" onClick={() => onChange(values.filter((x) => x !== v))}>
            <X className="size-3.5" />
          </button>
        </span>
      ))}
      <input
        aria-label={`Add to ${label}`}
        className="min-w-32 flex-1 bg-transparent px-1 text-sm outline-none"
        value={draft}
        placeholder={values.length ? "" : `Add ${label.toLowerCase()}…`}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={onKey}
        onBlur={add}
      />
    </div>
  );
}

async function uploadArtwork(itemId: number, kind: ArtworkKind, file: File): Promise<ItemDetail> {
  const res = await fetch(`/api/v1/items/${itemId}/artwork/${kind}`, {
    method: "POST",
    body: file,
    headers: { Authorization: `Bearer ${session.token}`, "Content-Type": file.type || "application/octet-stream" },
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, data.code ?? "error", data.message ?? res.statusText);
  return data as ItemDetail;
}

const sourceLabel: Record<string, string> = {
  local: "From your files", embedded: "In the file", tmdb: "TMDB", deezer: "Deezer", fanart: "Fanart", plex: "From Plex",
  upload: "Uploaded", chosen: "Chosen", generated: "From the video", frame: "From the video",
};

/** One kind of artwork: every option to click, an upload, a link, and back to automatic. */
function ArtworkPicker({ item, kind, label, onChanged }: { item: ItemDetail; kind: ArtworkKind; label: string; onChanged: (it: ItemDetail) => void }) {
  const qc = useQueryClient();
  const file = useRef<HTMLInputElement>(null);
  const [link, setLink] = useState("");
  const options = useQuery({
    queryKey: ["items", item.id, "artwork", kind],
    queryFn: () => unwrap(api.GET("/items/{itemId}/artwork", { params: { path: { itemId: item.id }, query: { kind } } })),
  });
  const done = (it: ItemDetail) => {
    onChanged(it);
    qc.invalidateQueries({ queryKey: ["items", item.id, "artwork", kind] });
  };
  const choose = useMutation({
    mutationFn: (body: components["schemas"]["ArtworkChoice"]) =>
      unwrap(api.PUT("/items/{itemId}/artwork/{kind}", { params: { path: { itemId: item.id, kind } }, body })),
    onSuccess: (it) => {
      setLink("");
      done(it);
    },
  });
  const upload = useMutation({ mutationFn: (f: File) => uploadArtwork(item.id, kind, f), onSuccess: done });
  const reset = useMutation({
    mutationFn: () => unwrap(api.DELETE("/items/{itemId}/artwork/{kind}", { params: { path: { itemId: item.id, kind } } })),
    onSuccess: done,
  });
  const busy = choose.isPending || upload.isPending || reset.isPending;
  const error = choose.error ?? upload.error ?? reset.error;
  const wide = kind === "backdrop" || kind === "thumb";
  const logo = kind === "logo";
  const square = kind === "poster" && (item.type === "album" || item.type === "artist");
  const list = options.data ?? [];
  const custom = list.some((o) => o.selected && (o.source === "upload" || o.source === "chosen"));
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <input
          ref={file}
          type="file"
          accept="image/jpeg,image/png,image/webp"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0];
            if (f) upload.mutate(f);
            e.target.value = "";
          }}
        />
        <Button size="sm" onClick={() => file.current?.click()} loading={upload.isPending} disabled={busy}>
          <ImagePlus className="size-4" /> Upload
        </Button>
        <form
          className="flex min-w-64 flex-1 gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (link.trim()) choose.mutate({ url: link.trim() });
          }}
        >
          <Input aria-label={`${label} image link`} placeholder="Or paste an image link (https://…)" value={link} onChange={(e) => setLink(e.target.value)} />
          <Button size="sm" type="submit" disabled={!link.trim() || busy}>
            <Link2 className="size-4" /> Use
          </Button>
        </form>
        {custom && (
          <Button size="sm" variant="ghost" onClick={() => reset.mutate()} loading={reset.isPending} disabled={busy} title="Use the image from your files or the metadata agents again">
            <RotateCcw className="size-4" /> Automatic
          </Button>
        )}
      </div>
      {error && <Alert tone="error">{error.message}</Alert>}
      {options.isPending ? (
        <Spinner />
      ) : list.length === 0 ? (
        <p className="text-sm text-muted">No {label.toLowerCase()} to choose from yet. Upload one, or paste a link.</p>
      ) : (
        <ul
          className={clsx("grid gap-3", wide || logo ? "grid-cols-2 sm:grid-cols-3" : "grid-cols-3 sm:grid-cols-5")}
          aria-label={`${label} options`}
        >
          {list.map((o: ArtworkOption, i) => (
            <li key={o.id ?? o.url ?? i}>
              <button
                type="button"
                disabled={busy || o.selected}
                onClick={() => choose.mutate(o.id ? { artworkId: o.id } : { url: o.url })}
                className={clsx(
                  "group relative block w-full overflow-hidden rounded-md border-2 text-left",
                  o.selected ? "border-accent" : "border-transparent hover:border-border",
                )}
                aria-label={`${o.selected ? "Current" : "Use this"} ${label.toLowerCase()} (${sourceLabel[o.source] ?? o.source})`}
              >
                <img
                  src={o.id ? imageUrl(o.id, wide || logo ? 320 : 180) : o.previewUrl}
                  alt=""
                  loading="lazy"
                  className={clsx(
                    "w-full bg-surface-2",
                    logo ? "aspect-[2/1] object-contain p-2" : wide ? "aspect-video object-cover" : square ? "aspect-square object-cover" : "aspect-[2/3] object-cover",
                  )}
                />
                {o.selected && (
                  <span className="absolute top-1.5 right-1.5 rounded-full bg-accent p-1 text-white">
                    <Check className="size-3.5" />
                  </span>
                )}
                <span className="block truncate px-1.5 py-1 text-xs text-muted">
                  {[sourceLabel[o.source] ?? o.source, o.language?.toUpperCase(), o.width && o.height ? `${o.width}×${o.height}` : ""].filter(Boolean).join(" · ")}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
