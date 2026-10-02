import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useId, useState } from "react";
import { api, unwrap } from "@/api/client";
import type { ItemDetail, SmartCollectionRules } from "@/api/types";
import { Alert, Button, Dialog, Field, Input, Select, Toggle } from "@/components/ui";
import { sortOptions } from "./sorts";

/** Drops unset fields (empty text, NaN numbers, a zero limit) so the server stores only real rules. */
export function cleanRules(r: SmartCollectionRules): SmartCollectionRules {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(r)) {
    if (v === undefined || v === null || v === "" || v === false) continue;
    if (typeof v === "number" && (!Number.isFinite(v) || v === 0)) continue;
    if (k === "sort" && v === "title") continue;
    out[k] = typeof v === "string" ? v.trim() : v;
  }
  return out as SmartCollectionRules;
}

/** Plain-language summary of a smart collection's rules, e.g. "Unwatched · Action · 1990s". */
export function describeRules(r: SmartCollectionRules) {
  const parts: string[] = [];
  if (r.watch) parts.push({ unwatched: "Unwatched", watched: "Watched", in_progress: "In progress" }[r.watch]);
  if (r.genre) parts.push(r.genre);
  if (r.decade) parts.push(`${r.decade}s`);
  if (r.yearFrom || r.yearTo) parts.push(`${r.yearFrom ?? "…"}–${r.yearTo ?? "…"}`);
  if (r.contentRating) parts.push(r.contentRating);
  if (r.resolution) parts.push(r.resolution === "4k" ? "4K" : r.resolution === "sd" ? "SD" : `${r.resolution}p`);
  if (r.hdr) parts.push("HDR");
  if (r.studio) parts.push(r.studio);
  if (r.minRating) parts.push(`Rated ${r.minRating}+`);
  if (r.addedDays) parts.push(`Added in the last ${r.addedDays} days`);
  if (r.personId) parts.push(`Person #${r.personId}`);
  if (r.limit) parts.push(`First ${r.limit}`);
  const sort = sortOptions.find((o) => o.value === (r.sort ?? "title"));
  if (sort && sort.value !== "title") parts.push(`By ${sort.label.toLowerCase()}`);
  return parts.join(" · ") || `All ${r.itemType === "show" ? "shows" : "movies"}`;
}

const num = (v: string) => (v === "" ? undefined : Number(v));

/** Every smart collection rule as a form (META-7). Genre and rating suggest the library's values. */
function RulesForm({ libraryId, value, onChange }: { libraryId: number; value: SmartCollectionRules; onChange: (r: SmartCollectionRules) => void }) {
  const set = (patch: Partial<SmartCollectionRules>) => onChange({ ...value, ...patch });
  const ids = useId();
  const filters = useQuery({
    queryKey: ["libraries", libraryId, "filters"],
    queryFn: () => unwrap(api.GET("/libraries/{libraryId}/filters", { params: { path: { libraryId } } })),
  });
  return (
    <div className="grid gap-4 sm:grid-cols-2">
      <Field label="Lists">
        {(id) => (
          <Select id={id} value={value.itemType} onChange={(e) => set({ itemType: e.target.value as SmartCollectionRules["itemType"] })}>
            <option value="movie">Movies</option>
            <option value="show">Shows</option>
          </Select>
        )}
      </Field>
      <Field label="Sort by">
        {(id) => (
          <Select id={id} value={value.sort ?? "title"} onChange={(e) => set({ sort: e.target.value })}>
            {sortOptions.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </Select>
        )}
      </Field>
      <Field label="Watched state">
        {(id) => (
          <Select id={id} value={value.watch ?? ""} onChange={(e) => set({ watch: (e.target.value || undefined) as SmartCollectionRules["watch"] })}>
            <option value="">Watched or not</option>
            <option value="unwatched">Unwatched</option>
            <option value="in_progress">In progress</option>
            <option value="watched">Watched</option>
          </Select>
        )}
      </Field>
      <Field label="Genre">
        {(id) => (
          <>
            <Input id={id} list={`${ids}-genres`} value={value.genre ?? ""} placeholder="Any genre" onChange={(e) => set({ genre: e.target.value || undefined })} />
            <datalist id={`${ids}-genres`}>
              {filters.data?.genres.map((g) => <option key={g.value} value={g.value} />)}
            </datalist>
          </>
        )}
      </Field>
      <Field label="Decade">
        {(id) => (
          <Select id={id} value={value.decade ?? ""} onChange={(e) => set({ decade: num(e.target.value) })}>
            <option value="">Any decade</option>
            {[...new Set([...(filters.data?.decades.map((d) => Number(d.value)) ?? []), ...(value.decade ? [value.decade] : [])])].map((d) => (
              <option key={d} value={d}>
                {d}s
              </option>
            ))}
          </Select>
        )}
      </Field>
      <Field label="Content rating">
        {(id) => (
          <>
            <Input id={id} list={`${ids}-ratings`} value={value.contentRating ?? ""} placeholder="Any rating" onChange={(e) => set({ contentRating: e.target.value || undefined })} />
            <datalist id={`${ids}-ratings`}>
              {filters.data?.contentRatings.map((r) => <option key={r.value} value={r.value} />)}
            </datalist>
          </>
        )}
      </Field>
      <Field label="Year from">
        {(id) => <Input id={id} type="number" min={1880} max={2100} value={value.yearFrom ?? ""} onChange={(e) => set({ yearFrom: num(e.target.value) })} />}
      </Field>
      <Field label="Year to">
        {(id) => <Input id={id} type="number" min={1880} max={2100} value={value.yearTo ?? ""} onChange={(e) => set({ yearTo: num(e.target.value) })} />}
      </Field>
      <Field label="Resolution">
        {(id) => (
          <Select id={id} value={value.resolution ?? ""} onChange={(e) => set({ resolution: (e.target.value || undefined) as SmartCollectionRules["resolution"] })}>
            <option value="">Any resolution</option>
            <option value="4k">4K</option>
            <option value="1080">1080p</option>
            <option value="720">720p</option>
            <option value="sd">SD</option>
          </Select>
        )}
      </Field>
      <Field label="Minimum rating" help="Audience, IMDb or critic rating, 0–10.">
        {(id) => <Input id={id} type="number" min={0} max={10} step={0.5} value={value.minRating ?? ""} onChange={(e) => set({ minRating: num(e.target.value) })} />}
      </Field>
      <Field label="Studio or network">
        {(id) => <Input id={id} value={value.studio ?? ""} onChange={(e) => set({ studio: e.target.value || undefined })} />}
      </Field>
      <Field label="Added in the last (days)">
        {(id) => <Input id={id} type="number" min={0} value={value.addedDays ?? ""} onChange={(e) => set({ addedDays: num(e.target.value) })} />}
      </Field>
      <Field label="Person (ID)" help="In the cast or crew; the number in a person page's address.">
        {(id) => <Input id={id} type="number" min={1} value={value.personId ?? ""} onChange={(e) => set({ personId: num(e.target.value) })} />}
      </Field>
      <Field label="Limit" help="At most this many; empty for all.">
        {(id) => <Input id={id} type="number" min={0} value={value.limit || ""} onChange={(e) => set({ limit: num(e.target.value) })} />}
      </Field>
      <div className="sm:col-span-2">
        <Toggle label="HDR only" checked={!!value.hdr} onChange={(v) => set({ hdr: v || undefined })} />
      </div>
    </div>
  );
}

/** Admins save a library's current filters and sort as a smart collection (META-7). */
export function SaveSmartCollectionDialog({ libraryId, rules, onClose }: { libraryId: number; rules: SmartCollectionRules; onClose: () => void }) {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [title, setTitle] = useState(() => (describeRules(rules).startsWith("All ") ? "" : describeRules(rules)));
  const create = useMutation({
    mutationFn: () => unwrap(api.POST("/libraries/{libraryId}/smart-collections", { params: { path: { libraryId } }, body: { title: title.trim(), rules: cleanRules(rules) } })),
    onSuccess: (c) => {
      qc.invalidateQueries({ queryKey: ["libraries", libraryId] });
      navigate({ to: "/item/$itemId", params: { itemId: String(c.id) } });
    },
  });
  return (
    <Dialog
      open
      onClose={onClose}
      title="Save as smart collection"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" form="smart-save" type="submit" disabled={!title.trim()} loading={create.isPending}>
            Save
          </Button>
        </>
      }
    >
      <form
        id="smart-save"
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          if (title.trim()) create.mutate();
        }}
      >
        {create.isError && <Alert tone="error">{create.error.message}</Alert>}
        <Field label="Name">{(id) => <Input id={id} autoFocus maxLength={200} value={title} onChange={(e) => setTitle(e.target.value)} />}</Field>
        <p className="text-sm text-muted">
          Lists {rules.itemType === "show" ? "shows" : "movies"} matching: <span className="text-text">{describeRules(rules)}</span>. It stays up to date as the library changes.
        </p>
      </form>
    </Dialog>
  );
}

/** "Edit rules" on a smart collection's page (admins). */
export function SmartRulesDialog({ collection, onClose }: { collection: ItemDetail; onClose: () => void }) {
  const qc = useQueryClient();
  const [title, setTitle] = useState(collection.title);
  const [rules, setRules] = useState<SmartCollectionRules>(collection.smartRules ?? { itemType: "movie" });
  const save = useMutation({
    mutationFn: () =>
      unwrap(
        api.PUT("/collections/{collectionId}/rules", {
          params: { path: { collectionId: collection.id } },
          body: { title: title.trim() && title.trim() !== collection.title ? title.trim() : undefined, rules: cleanRules(rules) },
        }),
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["items", collection.id] });
      onClose();
    },
  });
  return (
    <Dialog
      open
      wide
      onClose={onClose}
      title="Edit smart collection"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" disabled={!title.trim()} loading={save.isPending} onClick={() => save.mutate()}>
            Save rules
          </Button>
        </>
      }
    >
      <div className="space-y-5">
        {save.isError && <Alert tone="error">{save.error.message}</Alert>}
        <Field label="Name">{(id) => <Input id={id} maxLength={200} value={title} onChange={(e) => setTitle(e.target.value)} />}</Field>
        <RulesForm libraryId={collection.libraryId} value={rules} onChange={setRules} />
      </div>
    </Dialog>
  );
}
