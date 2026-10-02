import { Plus, X } from "lucide-react";
import type { SmartRules } from "@/api/types";
import { Button, Input, Select } from "@/components/ui";

type Condition = SmartRules["conditions"][number];

const fields: { value: Condition["field"]; label: string; numeric?: boolean; hint?: string }[] = [
  { value: "genre", label: "Genre" },
  { value: "artist", label: "Artist" },
  { value: "album", label: "Album" },
  { value: "title", label: "Title" },
  { value: "year", label: "Year", numeric: true },
  { value: "rating", label: "Rating (0–10)", numeric: true, hint: "8 = 4 stars" },
  { value: "playCount", label: "Play count", numeric: true },
  { value: "lastPlayedDays", label: "Days since played", numeric: true },
  { value: "addedDays", label: "Days since added", numeric: true },
  { value: "bpm", label: "Tempo (BPM)", numeric: true },
  { value: "energy", label: "Energy (0 calm – 1 intense)", numeric: true },
  { value: "key", label: "Key", hint: "e.g. A minor" },
  { value: "durationSeconds", label: "Length (seconds)", numeric: true },
];

const textOps: { value: Condition["op"]; label: string }[] = [
  { value: "is", label: "is" },
  { value: "isNot", label: "is not" },
  { value: "contains", label: "contains" },
  { value: "notContains", label: "doesn't contain" },
];
const numberOps: { value: Condition["op"]; label: string }[] = [
  { value: "is", label: "is" },
  { value: "gt", label: "is more than" },
  { value: "lt", label: "is less than" },
];

const sorts: { value: NonNullable<SmartRules["sort"]>; label: string }[] = [
  { value: "artist", label: "Artist" },
  { value: "random", label: "Random" },
  { value: "-added", label: "Recently added" },
  { value: "-lastPlayed", label: "Recently played" },
  { value: "-playCount", label: "Most played" },
  { value: "-rating", label: "Highest rated" },
  { value: "-year", label: "Newest" },
  { value: "year", label: "Oldest" },
  { value: "title", label: "Title" },
];

export const defaultRules: SmartRules = { match: "all", conditions: [{ field: "rating", op: "gt", value: "7" }], sort: "random", limit: 100 };

/** Builds a smart playlist's rules (MUSIC-8). */
export function RulesEditor({ value, onChange }: { value: SmartRules; onChange: (r: SmartRules) => void }) {
  const setCond = (i: number, patch: Partial<Condition>) =>
    onChange({ ...value, conditions: value.conditions.map((c, j) => (j === i ? { ...c, ...patch } : c)) });
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2 text-sm">
        Match
        <div className="w-24">
          <Select aria-label="Match" className="h-8" value={value.match} onChange={(e) => onChange({ ...value, match: e.target.value as SmartRules["match"] })}>
            <option value="all">all</option>
            <option value="any">any</option>
          </Select>
        </div>
        of these rules:
      </div>
      {value.conditions.map((c, i) => {
        const f = fields.find((x) => x.value === c.field)!;
        const ops = f.numeric ? numberOps : textOps;
        return (
          <div key={i} className="flex flex-wrap items-center gap-2">
            <div className="w-44">
              <Select
                aria-label="Field"
                className="h-9"
                value={c.field}
                onChange={(e) => {
                  const nf = fields.find((x) => x.value === e.target.value)!;
                  setCond(i, { field: nf.value, op: nf.numeric ? "gt" : "is", value: "" });
                }}
              >
                {fields.map((x) => (
                  <option key={x.value} value={x.value}>
                    {x.label}
                  </option>
                ))}
              </Select>
            </div>
            <div className="w-40">
              <Select aria-label="Operator" className="h-9" value={c.op} onChange={(e) => setCond(i, { op: e.target.value as Condition["op"] })}>
                {ops.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </Select>
            </div>
            <div className="min-w-32 flex-1">
              <Input aria-label="Value" className="h-9" inputMode={f.numeric ? "decimal" : "text"} placeholder={f.hint} value={c.value} onChange={(e) => setCond(i, { value: e.target.value })} />
            </div>
            <button
              type="button"
              onClick={() => onChange({ ...value, conditions: value.conditions.filter((_, j) => j !== i) })}
              disabled={value.conditions.length === 1}
              className="rounded p-1 text-muted hover:text-text disabled:opacity-30"
              aria-label="Remove rule"
            >
              <X className="size-4" />
            </button>
          </div>
        );
      })}
      <Button size="sm" variant="ghost" onClick={() => onChange({ ...value, conditions: [...value.conditions, { field: "genre", op: "is", value: "" }] })} disabled={value.conditions.length >= 20}>
        <Plus className="size-4" /> Add rule
      </Button>
      <div className="flex flex-wrap items-center gap-2 border-t border-border pt-3 text-sm">
        Order by
        <div className="w-44">
          <Select aria-label="Order by" className="h-8" value={value.sort ?? "artist"} onChange={(e) => onChange({ ...value, sort: e.target.value as SmartRules["sort"] })}>
            {sorts.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </Select>
        </div>
        and keep at most
        <div className="w-24">
          <Input aria-label="Limit" type="number" min={1} max={2000} className="h-8" value={value.limit ?? 100} onChange={(e) => onChange({ ...value, limit: Math.max(1, Math.min(2000, Number(e.target.value) || 100)) })} />
        </div>
        tracks
      </div>
    </div>
  );
}

/** Rules are complete when every rule has a value. */
export const rulesValid = (r: SmartRules) => r.conditions.length > 0 && r.conditions.every((c) => c.value.trim() !== "");
