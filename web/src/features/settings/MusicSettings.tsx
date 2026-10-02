import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Cpu, Play } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Alert, Button, Card, Spinner, Toggle } from "@/components/ui";
import { SaveBar } from "./SaveBar";
import { useSectionDraft } from "./useSectionDraft";

/** Settings → Music: sonic analysis, lyrics and loudness (M6.5). */
export function MusicSettings() {
  const s = useSectionDraft("music");
  const qc = useQueryClient();
  const status = useQuery({
    queryKey: ["music", "status"],
    queryFn: () => unwrap(api.GET("/music/status")),
    refetchInterval: (q) => (q.state.data?.running ? 3000 : 30_000),
  });
  const run = useMutation({
    mutationFn: (taskId: string) => unwrap(api.POST("/tasks/{taskId}/run", { params: { path: { taskId } } })),
    onSettled: () => qc.invalidateQueries({ queryKey: ["music", "status"] }),
  });
  if (!s.draft) return <Spinner />;
  const st = status.data;
  const pct = st && st.total ? Math.round((st.analyzed / st.total) * 100) : 0;
  return (
    <>
      <div className="space-y-6">
        <Card title="Sonic analysis" description="Marquee listens to every track on this server's GPU to power radios, “sounds like” suggestions, Sonic Adventure, Sonic Sage and daily mixes. Audio never leaves the server.">
          {st && (
            <div className="space-y-2 rounded-lg bg-surface-2 p-4 text-sm">
              <div className="flex flex-wrap items-center gap-x-6 gap-y-1">
                <span className="flex items-center gap-2">
                  <Cpu className="size-4 text-muted" aria-hidden />
                  {st.available ? `${st.model} on ${st.device === "cuda" ? "the GPU" : "the CPU"}` : "The analysis service isn't running"}
                </span>
                <span className="text-muted">
                  {st.analyzed.toLocaleString()} of {st.total.toLocaleString()} tracks analysed{st.failed ? ` · ${st.failed} unreadable` : ""}
                </span>
              </div>
              <div className="h-2 overflow-hidden rounded-full bg-surface-3">
                <div className="h-full bg-accent transition-all" style={{ width: `${pct}%` }} />
              </div>
              {st.running && st.runTotal ? <div className="text-xs text-muted">Analysing now: {st.progress ?? 0} of {st.runTotal} new tracks</div> : null}
            </div>
          )}
          {!st?.available && st?.enabled && (
            <Alert tone="info">Start the <code>sonic</code> container (it's in the compose file) to enable radios and Sonic Sage.</Alert>
          )}
          <Toggle label="Analyse music" help="New tracks are analysed within the hour, and right after a music scan." checked={!!s.draft.sonicAnalysis} onChange={(v) => s.update({ sonicAnalysis: v })} />
          <div>
            <Button size="sm" onClick={() => run.mutate("sonic")} disabled={!st?.available || st?.running} loading={run.isPending}>
              <Play className="size-4" /> Analyse new tracks now
            </Button>
          </div>
        </Card>
        <Card title="Lyrics" description="Lyrics come from the files themselves and from .lrc files next to them.">
          <Toggle
            label="Look up lyrics online"
            help="When a track has none, ask LRCLIB (a free, open lyrics database). Only the artist, title, album and length are sent."
            checked={!!s.draft.onlineLyrics}
            onChange={(v) => s.update({ onlineLyrics: v })}
          />
        </Card>
        <Card title="Volume levelling" description="Tracks with ReplayGain tags are levelled straight away. Others need their loudness measured once.">
          <Toggle label="Measure loudness" help="Runs in the maintenance window, a few tracks at a time at low priority." checked={!!s.draft.loudnessAnalysis} onChange={(v) => s.update({ loudnessAnalysis: v })} />
          <div>
            <Button size="sm" onClick={() => run.mutate("loudness")} loading={run.isPending}>
              <Play className="size-4" /> Measure now
            </Button>
          </div>
        </Card>
      </div>
      <SaveBar dirty={s.dirty} saving={s.saving} error={s.error} savedAt={s.savedAt} onSave={s.save} onReset={s.reset} />
    </>
  );
}
