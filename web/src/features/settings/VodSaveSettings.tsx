import { Link } from "@tanstack/react-router";
import { HardDriveDownload } from "lucide-react";
import { useState } from "react";
import type { components } from "@/api/schema.gen";
import { Button, Card, Field, Input, Select, Spinner, Toggle } from "@/components/ui";
import { FolderBrowser } from "./FolderBrowser";
import { SaveBar } from "./SaveBar";
import { useSectionDraft } from "./useSectionDraft";

type Draft = components["schemas"]["VodSaveSettings"];

function FolderField({ label, help, value, onChange }: { label: string; help: string; value: string; onChange: (v: string) => void }) {
  const [browsing, setBrowsing] = useState(false);
  return (
    <Field label={label} help={help}>
      {(id) => (
        <div className="space-y-2">
          <div className="flex gap-2">
            <Input id={id} value={value} placeholder="Not set: saving is off" onChange={(e) => onChange(e.target.value)} className="font-mono" />
            <Button size="sm" variant="secondary" className="h-10" onClick={() => setBrowsing((b) => !b)}>
              {browsing ? "Close" : "Browse"}
            </Button>
            {value && (
              <Button size="sm" variant="ghost" className="h-10" onClick={() => onChange("")}>
                Clear
              </Button>
            )}
          </div>
          {browsing && (
            <FolderBrowser
              initialPath={value || null}
              onPick={(p) => {
                onChange(p);
                setBrowsing(false);
              }}
            />
          )}
        </div>
      )}
    </Field>
  );
}

/** Settings → Movies on demand → Saving to library (admins only). */
export function VodSaveSettings() {
  const s = useSectionDraft("vodSave");
  if (!s.draft) return <Spinner />;
  const d = s.draft as Draft;
  const update = (p: Partial<Draft>) => s.update(p);
  const transcode = d.quality === "transcode";
  return (
    <>
      <Card
        title="Saving to library"
        description={
          <>
            Administrators can save films and episodes from Dispatcharr (Xtream Codes) sources into the library: a queue
            downloads them, verifies each file and moves it into these folders under the library&apos;s naming, then scans the
            library. See the{" "}
            <Link to="/vod/saves" className="underline">
              save queue
            </Link>
            .
          </>
        }
        actions={
          <Link to="/vod/saves" className="inline-flex items-center gap-1.5 text-sm text-muted hover:text-text">
            <HardDriveDownload className="size-4" /> Saves
          </Link>
        }
      >
        <div className="space-y-4">
          <FolderField
            label="Movies folder"
            help="Films go into “Title (Year)/Title (Year) WEBDL-1080p.mkv” here. Use a folder of a movies library so they're scanned."
            value={d.moviesFolder ?? ""}
            onChange={(v) => update({ moviesFolder: v })}
          />
          <FolderField
            label="Shows folder"
            help="Episodes go into “Show (Year)/Season 1/Show - S01E01 - Title WEBDL-1080p.mkv” here; an existing show folder is reused."
            value={d.showsFolder ?? ""}
            onChange={(v) => update({ showsFolder: v })}
          />
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Quality" help="Original keeps the file as it is (an MPEG-TS or other unusual container becomes MKV).">
              {(id) => (
                <Select id={id} value={d.quality ?? "original"} onChange={(e) => update({ quality: e.target.value as Draft["quality"] })}>
                  <option value="original">Original</option>
                  <option value="remux">Remux to MKV (every stream copied)</option>
                  <option value="transcode">Transcode</option>
                </Select>
              )}
            </Field>
            <Field label="Saves at once per source" help="Providers often allow one or two connections.">
              {(id) => (
                <Select id={id} value={d.concurrentPerSource ?? 1} onChange={(e) => update({ concurrentPerSource: Number(e.target.value) })}>
                  {[1, 2, 3, 4, 5].map((n) => (
                    <option key={n} value={n}>
                      {n}
                    </option>
                  ))}
                </Select>
              )}
            </Field>
          </div>
          {transcode && (
            <div className="grid gap-4 rounded-md border border-border p-4 sm:grid-cols-2">
              <Field label="Resolution" help="A cap: smaller videos aren't upscaled.">
                {(id) => (
                  <Select id={id} value={d.maxHeight ?? 1080} onChange={(e) => update({ maxHeight: Number(e.target.value) as Draft["maxHeight"] })}>
                    <option value={2160}>2160p (4K)</option>
                    <option value={1080}>1080p</option>
                    <option value={720}>720p</option>
                    <option value={480}>480p</option>
                  </Select>
                )}
              </Field>
              <Field label="Video codec" help="HEVC is about half the size of H.264 at the same quality. Uses the transcoder's encoders (NVENC, Quick Sync, else the CPU).">
                {(id) => (
                  <Select id={id} value={d.videoCodec ?? "hevc"} onChange={(e) => update({ videoCodec: e.target.value as Draft["videoCodec"] })}>
                    <option value="hevc">HEVC (H.265)</option>
                    <option value="h264">H.264</option>
                  </Select>
                )}
              </Field>
              <Field label="Video quality" help="Constant quality (CRF/CQ), unless a bitrate is set.">
                {(id) => (
                  <Select id={id} value={d.videoQuality ?? "balanced"} onChange={(e) => update({ videoQuality: e.target.value as Draft["videoQuality"] })}>
                    <option value="high">High</option>
                    <option value="balanced">Balanced</option>
                    <option value="small">Small files</option>
                  </Select>
                )}
              </Field>
              <Field label="Video bitrate (kbps)" help="Optional; empty uses the quality above.">
                {(id) => (
                  <Input
                    id={id}
                    type="number"
                    min={0}
                    max={200000}
                    value={d.videoKbps || ""}
                    placeholder="Use the quality"
                    onChange={(e) => update({ videoKbps: Number(e.target.value) || 0 })}
                  />
                )}
              </Field>
              <Field label="Audio">
                {(id) => (
                  <Select id={id} value={d.audio ?? "copy"} onChange={(e) => update({ audio: e.target.value as Draft["audio"] })}>
                    <option value="copy">Copy (as it is)</option>
                    <option value="aac_stereo">AAC stereo</option>
                    <option value="aac_51">AAC up to 5.1</option>
                  </Select>
                )}
              </Field>
            </div>
          )}
          <Toggle
            label="Scan the library after saving"
            help="Scans the library whose folder holds the saved file."
            checked={d.scanAfterSave ?? true}
            onChange={(v) => update({ scanAfterSave: v })}
          />
          <Toggle
            label="Skip titles already in the library"
            help="By TMDB id, or when the file is already in its folder."
            checked={d.skipExisting ?? true}
            onChange={(v) => update({ skipExisting: v })}
          />
          <Toggle
            label="Pause while someone is watching"
            help="Saves wait while something plays, and go on a minute after it stops."
            checked={d.pauseWhileWatching ?? true}
            onChange={(v) => update({ pauseWhileWatching: v })}
          />
          {(d.pauseWhileWatching ?? true) && (
            <Field label="Pause for">
              {(id) => (
                <Select id={id} value={d.pauseScope ?? "source"} onChange={(e) => update({ pauseScope: e.target.value as Draft["pauseScope"] })}>
                  <option value="source">Watching from the same source (including its Live TV)</option>
                  <option value="any">Any playback on the server (saves bandwidth and GPU)</option>
                </Select>
              )}
            </Field>
          )}
          <Toggle
            label="Retry failed saves"
            help="Every few hours, failed saves are tried again from where they stopped."
            checked={d.retryFailed ?? true}
            onChange={(v) => update({ retryFailed: v })}
          />
        </div>
      </Card>
      <SaveBar dirty={s.dirty} saving={s.saving} error={s.error} savedAt={s.savedAt} onSave={s.save} onReset={s.reset} />
    </>
  );
}
