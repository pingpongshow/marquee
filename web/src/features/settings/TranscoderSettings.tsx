import { ArrowDown, ArrowUp, Trash2 } from "lucide-react";
import type { EncoderKind, QualityRung } from "@/api/types";
import { Button, Card, Field, Input, Select, Spinner, Toggle } from "@/components/ui";
import { SaveBar } from "./SaveBar";
import { useSectionDraft } from "./useSectionDraft";

const encoderInfo: Record<EncoderKind, { label: string; help: string }> = {
  nvenc: { label: "NVIDIA NVENC", help: "GPU encoder. Fastest, handles many streams at once." },
  qsv: { label: "Intel Quick Sync", help: "Intel integrated graphics. Used when the NVIDIA GPU is busy or unavailable." },
  vaapi: { label: "VA-API", help: "Generic Linux hardware encoding (AMD / Intel)." },
  software: { label: "Software (CPU)", help: "Always available. Slowest; used as the last resort." },
};
const allEncoders: EncoderKind[] = ["nvenc", "qsv", "vaapi", "software"];

function move<T>(arr: T[], i: number, by: number): T[] {
  const out = [...arr];
  const j = i + by;
  if (j < 0 || j >= out.length) return out;
  [out[i], out[j]] = [out[j]!, out[i]!];
  return out;
}

export function TranscoderSettings() {
  // Keep the ladder sorted best-first so the server and UI agree on order.
  const s = useSectionDraft("transcoder", (d) => ({ ...d, remoteLadder: [...(d.remoteLadder ?? [])].sort((a, b) => b.videoKbps - a.videoKbps) }));
  if (!s.draft) return <Spinner />;
  const d = s.draft;
  const order = d.encoderOrder ?? [];
  const ladder = d.remoteLadder ?? [];
  const setLadder = (l: QualityRung[]) => s.update({ remoteLadder: l });

  return (
    <>
      <div className="space-y-6">
        <Card title="Hardware encoders" description="Tried in this order. If one fails or is at capacity, the next is used automatically.">
          <ol className="space-y-2">
            {order.map((e, i) => (
              <li key={e} className="flex items-center gap-3 rounded-md border border-border bg-surface-2 px-3 py-2">
                <span className="w-5 text-center text-sm font-semibold text-accent">{i + 1}</span>
                <div className="flex-1">
                  <div className="text-sm font-medium">{encoderInfo[e].label}</div>
                  <div className="text-xs text-muted">{encoderInfo[e].help}</div>
                </div>
                <Button size="sm" variant="ghost" aria-label={`Move ${encoderInfo[e].label} up`} disabled={i === 0} onClick={() => s.update({ encoderOrder: move(order, i, -1) })}>
                  <ArrowUp className="size-4" />
                </Button>
                <Button size="sm" variant="ghost" aria-label={`Move ${encoderInfo[e].label} down`} disabled={i === order.length - 1} onClick={() => s.update({ encoderOrder: move(order, i, 1) })}>
                  <ArrowDown className="size-4" />
                </Button>
                <Button size="sm" variant="ghost" aria-label={`Disable ${encoderInfo[e].label}`} disabled={order.length === 1} onClick={() => s.update({ encoderOrder: order.filter((x) => x !== e) })}>
                  <Trash2 className="size-4" />
                </Button>
              </li>
            ))}
          </ol>
          {allEncoders.some((e) => !order.includes(e)) && (
            <div className="flex flex-wrap gap-2">
              {allEncoders
                .filter((e) => !order.includes(e))
                .map((e) => (
                  <Button key={e} size="sm" variant="ghost" onClick={() => s.update({ encoderOrder: [...order, e] })}>
                    + {encoderInfo[e].label}
                  </Button>
                ))}
            </div>
          )}
        </Card>

        <Card title="Quality">
          <div className="grid gap-5 sm:grid-cols-2">
            <Field label="Encoder preset" help="Quality uses more GPU/CPU time per stream.">
              {(id) => (
                <Select id={id} value={d.preset} onChange={(e) => s.update({ preset: e.target.value as typeof d.preset })}>
                  <option value="speed">Prefer speed</option>
                  <option value="balanced">Balanced</option>
                  <option value="quality">Prefer quality</option>
                </Select>
              )}
            </Field>
            <Field label="Maximum simultaneous transcodes" help="Further streams wait or fall back to lower quality.">
              {(id) => <Input id={id} type="number" min={1} value={d.maxConcurrentTranscodes ?? 1} onChange={(e) => s.update({ maxConcurrentTranscodes: Number(e.target.value) })} />}
            </Field>
          </div>
          <Toggle label="Use HEVC for remote streams" help="About 40% less bandwidth at the same quality on devices that support it (all recent Apple devices)." checked={!!d.preferHevcRemote} onChange={(v) => s.update({ preferHevcRemote: v })} />
          <Toggle label="HDR tone mapping" help="Converts HDR to SDR when a device can’t display HDR, so colours aren’t washed out." checked={!!d.toneMapping} onChange={(v) => s.update({ toneMapping: v })} />
          <Field label="Transcode ahead (segments)" help="How far ahead of the viewer the transcoder works before pausing. Each segment is about 4 seconds.">
            {(id) => <Input id={id} type="number" min={2} value={d.throttleSegmentsAhead ?? 10} onChange={(e) => s.update({ throttleSegmentsAhead: Number(e.target.value) })} />}
          </Field>
        </Card>

        <Card title="Remote quality ladder" description="Steps offered to remote devices. Marquee starts at the highest step the connection supports and moves between steps as bandwidth changes.">
          <div className="space-y-2">
            <div className="grid grid-cols-[1fr_6rem_7rem_2.5rem] gap-2 px-1 text-xs font-medium text-faint">
              <span>Label</span>
              <span>Max height</span>
              <span>Video kbps</span>
              <span />
            </div>
            {ladder.map((r, i) => (
              <div key={i} className="grid grid-cols-[1fr_6rem_7rem_2.5rem] gap-2">
                <Input aria-label="Label" value={r.label} onChange={(e) => setLadder(ladder.map((x, j) => (j === i ? { ...x, label: e.target.value } : x)))} />
                <Input aria-label="Max height" type="number" min={144} value={r.maxHeight} onChange={(e) => setLadder(ladder.map((x, j) => (j === i ? { ...x, maxHeight: Number(e.target.value) } : x)))} />
                <Input aria-label="Video kbps" type="number" min={100} value={r.videoKbps} onChange={(e) => setLadder(ladder.map((x, j) => (j === i ? { ...x, videoKbps: Number(e.target.value) } : x)))} />
                <Button variant="ghost" aria-label="Remove step" onClick={() => setLadder(ladder.filter((_, j) => j !== i))}>
                  <Trash2 className="size-4" />
                </Button>
              </div>
            ))}
            <Button size="sm" variant="ghost" onClick={() => setLadder([...ladder, { label: "New step", maxHeight: 720, videoKbps: 3000 }])}>
              + Add step
            </Button>
          </div>
        </Card>
      </div>
      <SaveBar dirty={s.dirty} saving={s.saving} error={s.error} savedAt={s.savedAt} onSave={s.save} onReset={s.reset} />
    </>
  );
}
