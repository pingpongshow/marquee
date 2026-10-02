import { useQuery } from "@tanstack/react-query";
import { X } from "lucide-react";
import { useState } from "react";
import { itemQuery, searchQuery } from "@/api/queries";
import { Button, Card, Field, Input, Select, Spinner } from "@/components/ui";
import { SaveBar } from "./SaveBar";
import { useSectionDraft } from "./useSectionDraft";

/** Cinema trailers before movies (PLAY-18): how many, and an optional pre-roll video. */
export function CinemaSettings() {
  const s = useSectionDraft("cinema");
  if (!s.draft) return <Spinner />;
  const d = s.draft;
  return (
    <>
      <Card description="When a movie is started from the beginning, play trailers of other movies in your library first, like at the cinema. Each person can turn this off in their account.">
        <Field label="Trailers before a movie">
          {(id) => (
            <Select id={id} value={d.trailers ?? 0} onChange={(e) => s.update({ trailers: Number(e.target.value) })}>
              <option value={0}>Off</option>
              {[1, 2, 3, 4, 5].map((n) => (
                <option key={n} value={n}>
                  {n} {n === 1 ? "trailer" : "trailers"}
                </option>
              ))}
            </Select>
          )}
        </Field>
        <PrerollPicker value={d.prerollItemId ?? 0} onChange={(v) => s.update({ prerollItemId: v })} />
      </Card>
      <SaveBar dirty={s.dirty} saving={s.saving} error={s.error} savedAt={s.savedAt} onSave={s.save} onReset={s.reset} />
    </>
  );
}

/** Chooses the pre-roll video by search, or by entering its item ID. 0 clears it. */
function PrerollPicker({ value, onChange }: { value: number; onChange: (id: number) => void }) {
  const [q, setQ] = useState("");
  const results = useQuery(searchQuery(q, 8));
  const chosen = useQuery({ ...itemQuery(value), enabled: value > 0, retry: false });
  const videos = results.data?.groups.flatMap((g) => g.items).filter((i) => ["movie", "video", "episode"].includes(i.type)) ?? [];
  return (
    <Field
      label="Pre-roll video"
      help="Played after the trailers, just before the movie: a cinema intro or your own bumper. Find it by name, or enter its item ID (the number in its page's address)."
    >
      {(id) => (
        <div className="space-y-2">
          {value > 0 ? (
            <div className="flex items-center gap-2 rounded-md bg-surface-2 px-3 py-2 text-sm">
              <span className="min-w-0 flex-1 truncate">
                {chosen.data ? chosen.data.title : chosen.isError ? "Unknown item" : "…"} <span className="text-faint">#{value}</span>
              </span>
              <Button size="sm" variant="ghost" aria-label="Clear pre-roll video" onClick={() => onChange(0)}>
                <X className="size-4" />
              </Button>
            </div>
          ) : (
            <p className="text-sm text-muted">None</p>
          )}
          <div className="flex gap-2">
            <Input id={id} placeholder="Search for a video, or enter an item ID" value={q} onChange={(e) => setQ(e.target.value)} />
            {/^\d+$/.test(q.trim()) && (
              <Button
                onClick={() => {
                  onChange(Number(q.trim()));
                  setQ("");
                }}
              >
                Use ID
              </Button>
            )}
          </div>
          {q.trim() && videos.length > 0 && (
            <ul className="divide-y divide-border rounded-md border border-border">
              {videos.map((v) => (
                <li key={v.id}>
                  <button
                    type="button"
                    className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-surface-2"
                    onClick={() => {
                      onChange(v.id);
                      setQ("");
                    }}
                  >
                    <span className="min-w-0 flex-1 truncate">{v.title}</span>
                    <span className="text-xs text-faint">{v.type === "episode" ? v.grandparentTitle : v.year}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </Field>
  );
}
