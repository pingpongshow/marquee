import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { clsx } from "clsx";
import { Cpu, PlugZap, RotateCcw } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import type { AiStatus } from "@/api/types";
import { Alert, Button, Card, Field, Input, Select, Spinner, Toggle } from "@/components/ui";
import { SaveBar } from "./SaveBar";
import { useSectionDraft } from "./useSectionDraft";

const OTHER = "__other__";

function gb(bytes?: number) {
  return bytes ? `${(bytes / 1e9).toFixed(1)} GB` : "";
}

/** Settings → AI: the optional local Ollama behind smarter Muse and recommendations. */
export function AiSettings() {
  const s = useSectionDraft("ai");
  const qc = useQueryClient();
  const status = useQuery({
    queryKey: ["ai", "status"],
    queryFn: () => unwrap(api.GET("/ai/status")),
    refetchInterval: (q) => (q.state.data?.lyrics?.running || q.state.data?.loaded.length ? 5000 : 30_000),
  });
  const test = useMutation({
    mutationFn: () =>
      unwrap(api.POST("/ai/test", { body: { url: s.draft?.url, chatModel: s.draft?.chatModel, embedModel: s.draft?.embedModel } })),
  });
  const rebuild = useMutation({
    mutationFn: () => unwrap(api.POST("/ai/lyrics/rebuild")),
    onSettled: () => qc.invalidateQueries({ queryKey: ["ai", "status"] }),
  });
  if (!s.draft) return <Spinner />;
  const d = s.draft;
  const st = status.data;
  // Models to offer: the last test's list (for an address not saved yet), else the saved server's.
  const models = (test.data?.reachable ? test.data.models : st?.models) ?? [];
  const on = !!d.enabled;
  return (
    <>
      <div className="space-y-6">
        <Card description="Marquee can use a language model running in Ollama on your network to understand Muse prompts, re-rank results, explain recommendations and find songs by what their lyrics are about. Everything stays on your network. With this off, everything works as before.">
          <Toggle label="Use AI (Ollama)" checked={on} onChange={(v) => s.update({ enabled: v })} />
          <Field label="Ollama address" help="Where Ollama listens, e.g. http://127.0.0.1:11434.">
            {(id) => (
              <div className="flex gap-2">
                <Input id={id} value={d.url ?? ""} placeholder="http://127.0.0.1:11434" onChange={(e) => s.update({ url: e.target.value })} />
                <Button onClick={() => test.mutate()} loading={test.isPending}>
                  <PlugZap className="size-4" /> Test connection
                </Button>
              </div>
            )}
          </Field>
          {test.data && <TestResult result={test.data} />}
          {test.error && <Alert tone="error">{test.error.message}</Alert>}
          <div className="grid gap-4 sm:grid-cols-2">
            <ModelPicker
              label="Chat model"
              help="Reads prompts, re-ranks and writes reasons. qwen3:8b is quick."
              value={d.chatModel ?? ""}
              models={models}
              onChange={(v) => s.update({ chatModel: v })}
            />
            <ModelPicker
              label="Embedding model"
              help="Indexes movies, shows and lyrics. Changing it re-indexes in the background."
              value={d.embedModel ?? ""}
              models={models}
              onChange={(v) => s.update({ embedModel: v })}
            />
          </div>
          <Field label="Unload when idle (minutes)" help="Ollama frees the GPU after this long without requests. 0 unloads right after each one.">
            {(id) => (
              <Input
                id={id}
                type="number"
                min={0}
                max={1440}
                className="w-32"
                value={d.unloadMinutes ?? 5}
                onChange={(e) => s.update({ unloadMinutes: Math.max(0, Number(e.target.value) || 0) })}
              />
            )}
          </Field>
        </Card>

        <Card title="What AI does" description={on ? undefined : "These take effect while Use AI is on."}>
          <div className={clsx("space-y-4", !on && "opacity-60")}>
            <Toggle
              label="Understand Muse prompts"
              help="Reads music and movie prompts into genres, years, people, “like …” titles and moods. Without it, simple keyword matching is used."
              checked={!!d.understandPrompts}
              onChange={(v) => s.update({ understandPrompts: v })}
            />
            <Toggle
              label="Re-rank movie Muse results"
              help="Puts the best matches for the prompt first, with a short reason each."
              checked={!!d.rerankMovies}
              onChange={(v) => s.update({ rerankMovies: v })}
            />
            <Toggle
              label="Lyrics themes for music Muse"
              help="Finds songs by what they're about (“songs about the sea”). Lyrics come from the files, .lrc files and lyrics already looked up."
              checked={!!d.lyricsThemes}
              onChange={(v) => s.update({ lyricsThemes: v })}
            />
            {st?.lyrics && (
              <div className="flex flex-wrap items-center gap-3 text-sm text-muted">
                <span>
                  {st.lyrics.indexed.toLocaleString()} of {st.lyrics.withLyrics.toLocaleString()} tracks with lyrics indexed (
                  {st.lyrics.tracks.toLocaleString()} tracks in all){st.lyrics.running ? " · indexing now" : ""}
                </span>
                <Button size="sm" variant="ghost" onClick={() => rebuild.mutate()} loading={rebuild.isPending} disabled={!on || !d.lyricsThemes}>
                  <RotateCcw className="size-4" /> Rebuild
                </Button>
              </div>
            )}
            <Toggle
              label="Reasons on recommendations"
              help="A one-line “why this” for the top recommendations."
              checked={!!d.reasons}
              onChange={(v) => s.update({ reasons: v })}
            />
          </div>
        </Card>

        <Card title="Learning">
          <Toggle
            label="Learn from skips and ratings"
            help="Tracks skipped in the first 30 seconds, films abandoned early and low ratings count against similar picks in radios, mixes, Muse and recommendations; full plays and high ratings count for them. Skips fade over 30 days. Works without AI."
            checked={!!d.learnFromBehaviour}
            onChange={(v) => s.update({ learnFromBehaviour: v })}
          />
        </Card>

        {st && <StatusCard st={st} />}
      </div>
      <SaveBar dirty={s.dirty} saving={s.saving} error={s.error} savedAt={s.savedAt} onSave={s.save} onReset={s.reset} />
    </>
  );
}

function TestResult({ result }: { result: AiStatus }) {
  if (!result.reachable) return <Alert tone="error">Couldn't reach Ollama{result.error ? `: ${result.error}` : ""}.</Alert>;
  return (
    <Alert tone={result.error ? "info" : "success"}>
      Connected to Ollama {result.version}. Chat model {result.chatModelInstalled ? "installed" : "not installed"}, embedding model{" "}
      {result.embedModelInstalled ? "installed" : "not installed"}.{result.error ? ` ${result.error}` : ""}
    </Alert>
  );
}

/** A dropdown of the installed models, or a text field for one that isn't listed. */
function ModelPicker({ label, help, value, models, onChange }: { label: string; help: string; value: string; models: string[]; onChange: (v: string) => void }) {
  const listed = models.includes(value) || models.includes(`${value}:latest`);
  const [typing, setTyping] = useState(false);
  const free = typing || models.length === 0 || (value !== "" && !listed);
  return (
    <Field label={label} help={help}>
      {(id) =>
        free ? (
          <div className="flex gap-2">
            <Input id={id} value={value} placeholder="e.g. qwen3:8b" onChange={(e) => onChange(e.target.value)} />
            {models.length > 0 && (
              <Button variant="ghost" onClick={() => (setTyping(false), onChange(models[0] ?? ""))}>
                List
              </Button>
            )}
          </div>
        ) : (
          <Select
            id={id}
            value={models.includes(value) ? value : `${value}:latest`}
            onChange={(e) => (e.target.value === OTHER ? setTyping(true) : onChange(e.target.value.replace(/:latest$/, "")))}
          >
            {models.map((m) => (
              <option key={m} value={m}>
                {m}
              </option>
            ))}
            <option value={OTHER}>Other…</option>
          </Select>
        )
      }
    </Field>
  );
}

function StatusCard({ st }: { st: AiStatus }) {
  return (
    <Card title="Status">
      <div className="space-y-2 rounded-lg bg-surface-2 p-4 text-sm">
        <div className="flex items-center gap-2">
          <Cpu className="size-4 text-muted" aria-hidden />
          {!st.reachable
            ? `Ollama isn't reachable${st.error ? ` (${st.error})` : ""}`
            : st.loaded.length === 0
              ? `Ollama ${st.version ?? ""} · no model loaded, the GPU is free`
              : `Loaded now: ${st.loaded.map((m) => [m.name, gb(m.vramBytes)].filter(Boolean).join(" ")).join(", ")}`}
        </div>
        {st.enabled && st.reachable && (!st.chatModelInstalled || !st.embedModelInstalled) && (
          <div className="text-danger">A chosen model isn't installed in Ollama. Install it with ollama pull, or pick another.</div>
        )}
        {st.videos && (
          <div className="text-muted">
            {st.videos.indexed.toLocaleString()} of {st.videos.total.toLocaleString()} movies and shows indexed
            {st.videos.model ? ` (${st.videos.model.replace(/^ollama\//, "")})` : ""}
          </div>
        )}
        {st.lastError && (
          <div className="text-muted">
            Last error{st.lastErrorAt ? ` (${new Date(st.lastErrorAt).toLocaleString()})` : ""}: {st.lastError}
            {st.paused ? " · paused briefly, Muse uses its usual ranking meanwhile" : ""}
          </div>
        )}
      </div>
    </Card>
  );
}
