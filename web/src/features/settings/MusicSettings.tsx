import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Cpu, Play } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { useState } from "react";
import { settingsQuery, useUpdateSettings } from "@/api/queries";
import {
  Alert,
  Button,
  Card,
  Field,
  Input,
  Select,
  Spinner,
  Toggle,
} from "@/components/ui";
import type { components } from "@/api/schema.gen";

type SoundModelScore = components["schemas"]["SoundModelScore"];
const modelName = { general: "general", music: "music" } as const;
const percent = (v: number | undefined) => (v === undefined ? "—" : `${Math.round(v * 100)}%`);
import { SaveBar } from "./SaveBar";
import { useSectionDraft } from "./useSectionDraft";

/** Settings → Music: Soundprint analysis, lyrics and loudness (M6.5). */
export function MusicSettings() {
  const s = useSectionDraft("music");
  const qc = useQueryClient();
  const status = useQuery({
    queryKey: ["music", "status"],
    queryFn: () => unwrap(api.GET("/music/status")),
    refetchInterval: (q) => (q.state.data?.running ? 3000 : 30_000),
  });
  const run = useMutation({
    mutationFn: (taskId: string) =>
      unwrap(api.POST("/tasks/{taskId}/run", { params: { path: { taskId } } })),
    onSettled: () => qc.invalidateQueries({ queryKey: ["music", "status"] }),
  });
  // Switch to the sound model that was tried, or keep the one in use (MUSIC-1).
  const choose = useMutation({
    mutationFn: (action: "switch" | "keep") =>
      unwrap(api.POST("/music/sound-model", { body: { action } })),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: ["music", "status"] });
      qc.invalidateQueries({ queryKey: settingsQuery.queryKey });
      s.reset();
    },
  });
  if (!s.draft) return <Spinner />;
  const st = status.data;
  const pct = st && st.total ? Math.round((st.analyzed / st.total) * 100) : 0;
  return (
    <>
      <div className="space-y-6">
        <Card
          title="Soundprint analysis"
          description="Marquee listens to every track on this server's GPU to power radios, “sounds like” suggestions, Sound Journey, Muse and daily mixes. Audio never leaves the server."
        >
          {st && (
            <div className="space-y-2 rounded-lg bg-surface-2 p-4 text-sm">
              <div className="flex flex-wrap items-center gap-x-6 gap-y-1">
                <span className="flex items-center gap-2">
                  <Cpu className="size-4 text-muted" aria-hidden />
                  {st.available
                    ? `${st.model?.split("#")[0]} on ${st.device === "cuda" ? "the GPU" : "the CPU"}`
                    : "The analysis service isn't running"}
                </span>
                <span className="text-muted">
                  {st.analyzed.toLocaleString()} of {st.total.toLocaleString()}{" "}
                  tracks analysed{st.failed ? ` · ${st.failed} unreadable` : ""}
                </span>
              </div>
              <div className="h-2 overflow-hidden rounded-full bg-surface-3">
                <div
                  className="h-full bg-accent transition-all"
                  style={{ width: `${pct}%` }}
                />
              </div>
              {st.running && st.runTotal ? (
                <div className="text-xs text-muted">
                  {st.phase || "Analysing new tracks"}: {(st.progress ?? 0).toLocaleString()} of{" "}
                  {st.runTotal.toLocaleString()}
                </div>
              ) : null}
              {st.analyzed > 0 && (
                <div className="text-xs text-muted">
                  What tracks are like (happy, sad, danceable, sung…) measured for {(st.traits ?? 0).toLocaleString()};
                  language read from the lyrics of {(st.languages ?? 0).toLocaleString()}
                </div>
              )}
            </div>
          )}
          {!st?.available && st?.enabled && (
            <Alert tone="info">
              Start the <code>soundprint</code> container (it's in the compose file)
              to enable radios and Muse.
            </Alert>
          )}
          <Toggle
            label="Analyse music"
            help="New tracks are analysed within the hour, and right after a music scan."
            checked={!!s.draft.soundprintAnalysis}
            onChange={(v) => s.update({ soundprintAnalysis: v })}
          />
          <div>
            <Button
              size="sm"
              onClick={() => run.mutate("soundprint")}
              disabled={!st?.available || st?.running}
              loading={run.isPending && run.variables === "soundprint"}
            >
              <Play className="size-4" /> Analyse new tracks now
            </Button>
          </div>
        </Card>
        <Card
          title="Sound model"
          description="How Soundprint hears music. The general model knows all kinds of sound; the music model was trained on music alone. Choosing the other one analyses the library with it alongside the one in use (radios keep working), compares the two on your music, and switches only when you say so."
        >
          <Field label="Model">
            {(id) => (
              <Select
                id={id}
                value={s.draft?.soundModel ?? "general"}
                onChange={(e) => s.update({ soundModel: e.target.value as "general" | "music" })}
              >
                <option value="general">General (all kinds of sound)</option>
                <option value="music">Music (trained on music alone)</option>
              </Select>
            )}
          </Field>
          {st?.soundModel && (
            <p className="text-sm text-muted">In use: the {modelName[st.soundModel]} model.</p>
          )}
          {st?.trial && (
            <div className="space-y-3 rounded-lg bg-surface-2 p-4 text-sm">
              {!st.trial.tried ? (
                <>
                  <p>
                    Trying the {modelName[st.trial.model]} model: {st.trial.analyzed.toLocaleString()} of{" "}
                    {st.trial.total.toLocaleString()} tracks analysed with it. It's compared once it covers the library.
                  </p>
                  <div>
                    <Button
                      size="sm"
                      onClick={() => run.mutate("soundprint")}
                      disabled={!st.available || st.running}
                      loading={run.isPending && run.variables === "soundprint"}
                    >
                      <Play className="size-4" /> Continue now
                    </Button>
                  </div>
                </>
              ) : (
                <>
                  <p>
                    The {modelName[st.trial.model]} model has been tried on the whole library. How often each model's
                    closest-sounding tracks share a genre with the track (and were played in the same sitting, with
                    enough history), and how well it finds music from words, which Muse and the moods depend on.
                    Higher is better:
                  </p>
                  <ModelComparison
                    current={st.trial.current}
                    tried={st.trial.tried}
                    currentName={modelName[st.soundModel ?? "general"]}
                    triedName={modelName[st.trial.model]}
                  />
                  {(st.trial.current?.textMatch ?? 0) > 0 &&
                    (st.trial.tried?.textMatch ?? 0) < 0.6 * (st.trial.current?.textMatch ?? 0) && (
                      <Alert tone="error">
                        The {modelName[st.trial.model]} model finds music from words much less well, so Muse and the
                        mood stations would get worse with it.
                      </Alert>
                    )}
                  <div className="flex flex-wrap gap-2">
                    <Button variant="primary" size="sm" loading={choose.isPending && choose.variables === "switch"}
                      onClick={() => choose.mutate("switch")}>
                      Switch to the {modelName[st.trial.model]} model
                    </Button>
                    <Button size="sm" variant="ghost" loading={choose.isPending && choose.variables === "keep"}
                      onClick={() => choose.mutate("keep")}>
                      Keep the {modelName[st.soundModel ?? "general"]} model
                    </Button>
                  </div>
                </>
              )}
            </div>
          )}
          {choose.error && <Alert tone="error">{choose.error.message}</Alert>}
        </Card>
        <Card
          title="Similar artists and tags"
          description="What listeners elsewhere say about your music: which artists they consider alike, and how they tag artists and tracks (melancholic, chill, female vocals…). Artist radios and Discovery reach further, and moods and Muse understand more."
        >
          <Toggle
            label="Look up similar artists and tags"
            help="Asks ListenBrainz (free and open) about artists with a MusicBrainz id, a few requests a second, and Last.fm too once a Last.fm API key is set below. Only artist and track names and MusicBrainz ids are sent. The first run takes a while for a large library (an hour or more with Last.fm); later runs cover new music and refresh old lookups."
            checked={!!s.draft.communityData}
            onChange={(v) => s.update({ communityData: v })}
          />
          <div className="flex flex-wrap gap-2">
            <Button
              size="sm"
              onClick={() => run.mutate("music-community")}
              disabled={!s.draft.communityData}
              loading={run.isPending && run.variables === "music-community"}
            >
              <Play className="size-4" /> Look up now
            </Button>
            <Button
              size="sm"
              onClick={() => run.mutate("music-listening")}
              loading={run.isPending && run.variables === "music-listening"}
            >
              <Play className="size-4" /> Learn from listening now
            </Button>
          </div>
          <p className="text-xs text-muted">
            Learning from listening pairs the tracks people play in one sitting or keep in one playlist, so radios and
            mixes play them together. It runs nightly in the maintenance window.
          </p>
        </Card>
        <Card
          title="Artist and album details"
          description="Bios, styles and facts that help artist pages and Muse."
        >
          <Toggle
            label="Look up artists and albums online"
            help="Asks MusicBrainz (an open music database) about each artist and album, about one a second, and Wikipedia for artist bios: styles, solo artist or band, country, active years, album types and labels. Only names and MusicBrainz ids are sent. The first run takes a few hours for a large library; later runs cover new music."
            checked={!!s.draft.artistDetails}
            onChange={(v) => s.update({ artistDetails: v })}
          />
          <div>
            <Button
              size="sm"
              onClick={() => run.mutate("musicbrainz")}
              disabled={!s.draft.artistDetails}
              loading={run.isPending && run.variables === "musicbrainz"}
            >
              <Play className="size-4" /> Look up new artists and albums now
            </Button>
          </div>
        </Card>
        <Card
          title="Lyrics"
          description="Lyrics come from the files themselves and from .lrc files next to them."
        >
          <Toggle
            label="Look up lyrics online"
            help="When a track has none, ask LRCLIB (a free, open lyrics database). Only the artist, title, album and length are sent."
            checked={!!s.draft.onlineLyrics}
            onChange={(v) => s.update({ onlineLyrics: v })}
          />
        </Card>
        <Card
          title="Volume levelling"
          description="Every track plays at an even volume."
        >
          <p className="text-sm text-muted">
            Volume levelling is always on. Tracks without ReplayGain tags have their loudness measured once, in the
            maintenance window, a few at a time at low priority.
          </p>
          <div>
            <Button
              size="sm"
              onClick={() => run.mutate("loudness")}
              loading={run.isPending && run.variables === "loudness"}
            >
              <Play className="size-4" /> Measure now
            </Button>
          </div>
        </Card>
        {run.error && <Alert tone="error">{run.error.message}</Alert>}
        <LastFmSettings />
      </div>
      <SaveBar
        dirty={s.dirty}
        saving={s.saving}
        error={s.error}
        savedAt={s.savedAt}
        onSave={s.save}
        onReset={s.reset}
      />
    </>
  );
}

/** Last.fm scrobbling (MUSIC-12): the server's Last.fm API account. Saved on its own. */
function LastFmSettings() {
  const settings = useQuery(settingsQuery);
  const save = useUpdateSettings();
  const [key, setKey] = useState("");
  const [secret, setSecret] = useState("");
  const on = !!settings.data?.integrations?.lastFmConfigured;
  return (
    <Card
      title="Last.fm"
      description={
        <>
          Lets people send what they play to their Last.fm accounts (each
          connects under Account), and adds Last.fm's similar artists and tags
          to the ones from ListenBrainz. Create an API account at{" "}
          <a
            className="underline"
            href="https://www.last.fm/api/account/create"
            target="_blank"
            rel="noreferrer"
          >
            last.fm/api/account/create
          </a>{" "}
          — the callback URL can be left empty — then enter its key and shared
          secret here.
        </>
      }
    >
      {on && (
        <p className="text-sm">
          Last.fm is set up. Enter new values to replace them.
        </p>
      )}
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="API key">
          {(id) => (
            <Input
              id={id}
              value={key}
              autoComplete="off"
              placeholder={on ? "••••••••" : ""}
              onChange={(e) => setKey(e.target.value)}
            />
          )}
        </Field>
        <Field label="Shared secret">
          {(id) => (
            <Input
              id={id}
              type="password"
              autoComplete="new-password"
              value={secret}
              placeholder={on ? "••••••••" : ""}
              onChange={(e) => setSecret(e.target.value)}
            />
          )}
        </Field>
      </div>
      <div className="flex gap-2">
        <Button
          variant="primary"
          size="sm"
          disabled={!key.trim() || !secret.trim()}
          loading={save.isPending}
          onClick={() =>
            save.mutate(
              {
                integrations: {
                  lastFmApiKey: key.trim(),
                  lastFmSecret: secret.trim(),
                },
              },
              { onSuccess: () => (setKey(""), setSecret("")) },
            )
          }
        >
          Save
        </Button>
        {on && (
          <Button
            size="sm"
            variant="ghost"
            onClick={() =>
              save.mutate({
                integrations: { lastFmApiKey: "", lastFmSecret: "" },
              })
            }
          >
            Turn off
          </Button>
        )}
      </div>
      {save.error && <Alert tone="error">{save.error.message}</Alert>}
    </Card>
  );
}

/** The two models' scores side by side. */
function ModelComparison({
  current,
  tried,
  currentName,
  triedName,
}: {
  current?: SoundModelScore;
  tried?: SoundModelScore;
  currentName: string;
  triedName: string;
}) {
  const played = !!(current?.playedMatch || tried?.playedMatch);
  const words = !!(current?.textMatch || tried?.textMatch);
  return (
    <table className="w-full max-w-md text-left text-sm">
      <thead className="text-xs text-muted">
        <tr>
          <th className="py-1 font-medium">Model</th>
          <th className="py-1 font-medium">Same genre</th>
          {played && <th className="py-1 font-medium">Played together</th>}
          {words && <th className="py-1 font-medium">Found by description</th>}
        </tr>
      </thead>
      <tbody>
        <tr>
          <td className="py-1">{currentName} (in use)</td>
          <td className="py-1 tabular-nums">{percent(current?.genreMatch)}</td>
          {played && <td className="py-1 tabular-nums">{percent(current?.playedMatch)}</td>}
          {words && <td className="py-1 tabular-nums">{percent(current?.textMatch)}</td>}
        </tr>
        <tr>
          <td className="py-1">{triedName}</td>
          <td className="py-1 tabular-nums">{percent(tried?.genreMatch)}</td>
          {played && <td className="py-1 tabular-nums">{percent(tried?.playedMatch)}</td>}
          {words && <td className="py-1 tabular-nums">{percent(tried?.textMatch)}</td>}
        </tr>
      </tbody>
    </table>
  );
}
