import { useMutation, useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { MonitorSpeaker, Pause, Play, RotateCcw, RotateCw, SkipBack, SkipForward, Square, Unplug, Volume2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { api, imageUrl, unwrap } from "@/api/client";
import { itemQuery } from "@/api/queries";
import type { MediaStream, RemoteCommand, RemotePlayer } from "@/api/types";
import { Alert, Button, Select, Spinner } from "@/components/ui";
import { languageName } from "../browse/format";
import { fmtTime } from "../player/NowPlaying";
import { PlatformIcon, PlayerList, playerActivity } from "./PlayOn";

/** Remote (USER-14): pick a Marquee player, then control it. */
export function RemotePage() {
  const { d } = useSearch({ from: "/remote" });
  const navigate = useNavigate();
  return (
    <div className="mx-auto max-w-xl p-6 lg:p-8">
      {d ? (
        <RemoteControl key={d} deviceId={d} onDisconnect={() => navigate({ to: "/remote", search: { d: undefined } })} />
      ) : (
        <>
          <h1 className="mb-1 flex items-center gap-2 text-2xl font-bold">
            <MonitorSpeaker className="size-6 text-accent" aria-hidden /> Remote
          </h1>
          <p className="mb-6 text-muted">Control Marquee on your TV, phone, tablet or another browser.</p>
          <PlayerList onPick={(p) => navigate({ to: "/remote", search: { d: p.deviceId } })} />
        </>
      )}
    </div>
  );
}

/** Follows a player's state with long polls (?since=version). */
function usePlayer(deviceId: number) {
  const [player, setPlayer] = useState<RemotePlayer | null>(null);
  const [receivedAt, setReceivedAt] = useState(0);
  const [gone, setGone] = useState(false);
  useEffect(() => {
    const ctrl = new AbortController();
    let since: number | undefined;
    (async () => {
      while (!ctrl.signal.aborted) {
        try {
          const { data, response } = await api.GET("/remote/players/{deviceId}", {
            params: { path: { deviceId }, query: since === undefined ? {} : { since } },
            signal: ctrl.signal,
          });
          if (response.status === 404) {
            setGone(true);
            return;
          }
          if (!data) throw new Error(String(response.status));
          since = data.version;
          setGone(false);
          setPlayer(data);
          setReceivedAt(Date.now());
        } catch {
          if (ctrl.signal.aborted) return;
          await new Promise((r) => window.setTimeout(r, 3000));
        }
      }
    })();
    return () => ctrl.abort();
  }, [deviceId]);
  return { player, receivedAt, gone };
}

function streamLabel(s: MediaStream) {
  return [languageName(s.language), s.title, s.codec.toUpperCase(), s.forced ? "Forced" : "", s.hearingImpaired ? "SDH" : ""].filter(Boolean).join(" · ");
}

function RemoteControl({ deviceId, onDisconnect }: { deviceId: number; onDisconnect: () => void }) {
  const { player, receivedAt, gone } = usePlayer(deviceId);
  const s = player?.state;
  const [now, setNow] = useState(() => Date.now());
  const [scrub, setScrub] = useState<number | null>(null);
  const [vol, setVol] = useState<number | null>(null);
  const timers = useRef<{ seek?: number; vol?: number }>({});
  const send = useMutation({
    mutationFn: (body: RemoteCommand) => unwrap(api.POST("/remote/players/{deviceId}/commands", { params: { path: { deviceId } }, body })),
  });
  const playing = s?.state === "playing";
  useEffect(() => {
    if (!playing) return;
    const t = window.setInterval(() => setNow(Date.now()), 500);
    return () => window.clearInterval(t);
  }, [playing]);
  useEffect(() => () => window.clearTimeout(timers.current.seek), []);
  const art = useQuery({ ...itemQuery(s?.artItemId ?? 0), enabled: !!s?.artItemId });
  const item = useQuery({ ...itemQuery(s?.itemId ?? 0), enabled: !!s?.itemId && s.itemType !== "track" });

  if (gone)
    return (
      <div className="space-y-4">
        <Alert tone="info">That player isn't available any more. It may have been closed or gone to sleep.</Alert>
        <Button onClick={onDisconnect}>Choose another player</Button>
      </div>
    );
  if (!player) return <Spinner label="Connecting" />;

  const duration = s?.durationMs ?? 0;
  const live = (s?.positionMs ?? 0) + (playing ? Math.max(0, now - receivedAt) : 0);
  const position = scrub ?? Math.min(live, duration || live);
  const poster = art.data?.images?.poster ?? art.data?.images?.thumb;
  const files = item.data?.versions.flatMap((v) => v.files) ?? [];
  const file = files.find((f) => f.streams.some((x) => x.id === s?.audioStreamId || x.id === s?.subtitleStreamId)) ?? files[0];
  const audio = file?.streams.filter((x) => x.kind === "audio") ?? [];
  const subs = file?.streams.filter((x) => x.kind === "subtitle") ?? [];
  const idle = !s || s.state === "idle" || s.state === "stopped" || !s.itemId;
  const cmd = (c: RemoteCommand) => send.mutate(c);
  const btn = "rounded-full p-3 text-text hover:bg-surface-2 disabled:opacity-40";

  return (
    <div className="space-y-6" data-testid="remote-control">
      <header className="flex items-center gap-3">
        <PlatformIcon platform={player.platform} className="size-7 text-accent" />
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-xl font-bold">{player.name}</h1>
          <p className="truncate text-sm text-muted">
            {player.userName} · {playerActivity(player)}
          </p>
        </div>
      </header>
      {send.isError && <Alert tone="error">{send.error.message}</Alert>}
      <div className="flex flex-col items-center gap-4 text-center">
        <div className="aspect-square w-full max-w-72 overflow-hidden rounded-xl bg-surface-2 shadow-2xl">
          {poster ? <img src={imageUrl(poster, 400)} alt="" className="size-full object-cover" /> : <div className="flex size-full items-center justify-center text-faint">{idle ? "Nothing playing" : ""}</div>}
        </div>
        <div className="w-full min-w-0">
          <div className="truncate text-xl font-semibold" data-testid="remote-title">
            {idle ? "Nothing playing" : s.title}
          </div>
          {!idle && s.subtitle && <div className="truncate text-muted">{s.subtitle}</div>}
          {!idle && s.queueLength !== undefined && s.queueLength > 1 && (
            <div className="text-xs text-faint">
              {(s.queueIndex ?? 0) + 1} of {s.queueLength}
            </div>
          )}
        </div>
      </div>
      <div>
        <input
          type="range"
          min={0}
          max={duration || 1}
          step={1000}
          value={Math.min(position, duration || 1)}
          disabled={idle || !duration}
          aria-label="Seek"
          className="w-full accent-[var(--color-accent)]"
          onChange={(e) => {
            const ms = Number(e.target.value);
            setScrub(ms);
            window.clearTimeout(timers.current.seek);
            timers.current.seek = window.setTimeout(() => {
              cmd({ type: "seek", positionMs: ms });
              setScrub(null);
            }, 350);
          }}
        />
        <div className="flex justify-between text-xs text-muted tabular-nums" data-testid="remote-time">
          <span>{fmtTime(position / 1000)}</span>
          <span>{fmtTime(duration / 1000)}</span>
        </div>
      </div>
      <div className="flex items-center justify-center gap-2">
        <button className={btn} disabled={idle} onClick={() => cmd({ type: "previous" })} aria-label="Previous">
          <SkipBack className="size-6 fill-current" />
        </button>
        <button className={btn} disabled={idle} onClick={() => cmd({ type: "seek", positionMs: Math.max(0, live - 10_000) })} aria-label="Back 10 seconds">
          <RotateCcw className="size-6" />
        </button>
        <button
          className="rounded-full bg-accent p-4 text-black disabled:opacity-40"
          disabled={idle}
          onClick={() => cmd({ type: playing ? "pause" : "resume" })}
          aria-label={playing ? "Pause" : "Play"}
        >
          {playing ? <Pause className="size-7 fill-current" /> : <Play className="size-7 fill-current" />}
        </button>
        <button className={btn} disabled={idle} onClick={() => cmd({ type: "seek", positionMs: duration ? Math.min(duration - 1000, live + 30_000) : live + 30_000 })} aria-label="Forward 30 seconds">
          <RotateCw className="size-6" />
        </button>
        <button className={btn} disabled={idle} onClick={() => cmd({ type: "next" })} aria-label="Next">
          <SkipForward className="size-6 fill-current" />
        </button>
      </div>
      {(audio.length > 1 || subs.length > 0) && !idle && (
        <div className="grid gap-3 sm:grid-cols-2">
          {audio.length > 1 && (
            <Select aria-label="Audio track" value={s.audioStreamId ?? ""} onChange={(e) => cmd({ type: "setAudio", streamId: Number(e.target.value) })}>
              {audio.map((a) => (
                <option key={a.id} value={a.id}>
                  {streamLabel(a)}
                </option>
              ))}
            </Select>
          )}
          {subs.length > 0 && (
            <Select aria-label="Subtitles" value={s.subtitleStreamId ?? -1} onChange={(e) => cmd({ type: "setSubtitle", streamId: Number(e.target.value) })}>
              <option value={-1}>Subtitles off</option>
              {subs.map((x) => (
                <option key={x.id} value={x.id}>
                  {streamLabel(x)}
                </option>
              ))}
            </Select>
          )}
        </div>
      )}
      {s?.volume !== undefined && (
        <div className="flex items-center gap-3">
          <Volume2 className="size-5 text-muted" aria-hidden />
          <input
            type="range"
            min={0}
            max={1}
            step={0.05}
            value={vol ?? s.volume}
            aria-label="Volume"
            className="flex-1 accent-[var(--color-accent)]"
            onChange={(e) => {
              const v = Number(e.target.value);
              setVol(v);
              window.clearTimeout(timers.current.vol);
              timers.current.vol = window.setTimeout(() => {
                cmd({ type: "setVolume", volume: v });
                setVol(null);
              }, 250);
            }}
          />
        </div>
      )}
      <div className="flex justify-center gap-3">
        <Button disabled={idle} onClick={() => cmd({ type: "stop" })}>
          <Square className="size-4 fill-current" /> Stop
        </Button>
        <Button variant="ghost" onClick={onDisconnect}>
          <Unplug className="size-4" /> Disconnect
        </Button>
      </div>
    </div>
  );
}
