import { useNavigate } from "@tanstack/react-router";
import { useEffect, useRef } from "react";
import { api, unwrap } from "@/api/client";
import { fetchLeaves } from "@/api/queries";
import type { ItemSummary, RemoteCommand, RemotePlayerState } from "@/api/types";
import { toast } from "@/components/Toast";
import { useMusic } from "../player/MusicPlayer";
import { onRemoteStateChanged, videoTarget, type RemoteTarget } from "./bus";

const RETRY_MS = 5_000;
const REPORT_EVERY_MS = 10_000;
/** A new "Controlled from" toast after this long without commands. */
const SESSION_GAP_MS = 30 * 60_000;

function sleep(ms: number, signal: AbortSignal) {
  return new Promise<void>((resolve) => {
    const t = window.setTimeout(resolve, ms);
    signal.addEventListener("abort", () => {
      window.clearTimeout(t);
      resolve();
    });
  });
}

async function fetchItems(ids: number[]) {
  const out: ItemSummary[] = [];
  for (let i = 0; i < ids.length; i += 20) {
    const chunk = await Promise.all(ids.slice(i, i + 20).map((id) => unwrap(api.GET("/items/{itemId}", { params: { path: { itemId: id } } })).catch(() => null)));
    out.push(...chunk.filter((x): x is NonNullable<typeof x> => !!x));
  }
  return out;
}

/**
 * Makes this tab a controllable player (USER-14) while it's visible, or while it's playing
 * music in the background: long-polls the inbox, runs the commands it gets, and reports what's
 * playing on every change and every 10 s while playing. Mounted only while signed in.
 */
export function RemoteHost() {
  const music = useMusic();
  const navigate = useNavigate();
  const musicRef = useRef(music);
  const navigateRef = useRef(navigate);
  const wakeRef = useRef<() => void>(() => {});
  const scheduleRef = useRef<() => void>(() => {});
  // A start position for music the remote asked for, applied once that track is playing.
  const pendingSeek = useRef<{ itemId: number; ms: number } | null>(null);
  const lastTime = useRef(0);

  useEffect(() => {
    musicRef.current = music;
    navigateRef.current = navigate;
  });

  // The inbox loop and state reports.
  useEffect(() => {
    let disposed = false;
    let running = false;
    let ctrl = new AbortController();
    let cursor = 0;
    let reportTimer: number | undefined;
    let lastCommandAt = 0;
    let lastFrom = "";

    const musicTarget = (): RemoteTarget | null => {
      const m = musicRef.current;
      const cur = m.current;
      if (!cur) return null;
      const t = cur.item;
      return {
        state: () => {
          const mm = musicRef.current;
          const dur = isFinite(mm.duration) && mm.duration > 0 ? mm.duration * 1000 : (t.durationMs ?? 0);
          return {
            state: mm.playing ? "playing" : "paused",
            itemId: t.id,
            itemType: t.type,
            title: t.title,
            subtitle: [t.artistCredit ?? t.grandparentTitle, t.parentTitle].filter(Boolean).join(" · "),
            artItemId: t.parentId ?? t.id,
            positionMs: Math.round(mm.time * 1000),
            durationMs: Math.round(dur),
            queueIndex: mm.queue.index,
            queueLength: mm.queue.entries.length,
            volume: mm.volume,
            shuffle: mm.queue.shuffled,
          };
        },
        pause: () => musicRef.current.playing && musicRef.current.toggle(),
        resume: () => !musicRef.current.playing && musicRef.current.toggle(),
        seek: (ms) => musicRef.current.seek(ms / 1000),
        stop: () => musicRef.current.close(),
        next: () => musicRef.current.next(),
        previous: () => musicRef.current.prev(),
        setVolume: (v) => musicRef.current.setVolume(v),
      };
    };
    const target = () => videoTarget() ?? musicTarget();
    const currentState = (): RemotePlayerState => target()?.state() ?? { state: "idle", positionMs: 0 };
    const active = () => document.visibilityState === "visible" || musicRef.current.playing;

    const report = () => {
      if (disposed || !active()) return;
      api.PUT("/remote/state", { body: currentState() }).catch(() => {});
    };
    const schedule = () => {
      window.clearTimeout(reportTimer);
      reportTimer = window.setTimeout(report, 150);
    };
    scheduleRef.current = schedule;

    const playVideo = (id: number, startMs?: number) => {
      const m = musicRef.current;
      if (m.playing) m.toggle();
      navigateRef.current({ to: "/play/$itemId", params: { itemId: String(id) }, search: { t: startMs } });
    };
    const playMusic = (tracks: ItemSummary[], index: number, c: RemoteCommand, source?: string) => {
      if (!tracks.length) return;
      videoTarget()?.stop();
      const start = Math.min(Math.max(0, index), tracks.length - 1);
      pendingSeek.current = c.startMs ? { itemId: tracks[start]!.id, ms: c.startMs } : null;
      musicRef.current.play(tracks, start, { shuffle: c.shuffle, source });
    };
    // One item plays the way its own Play button would; several are a track queue.
    const play = async (c: RemoteCommand) => {
      const ids = c.itemIds ?? [];
      if (!ids.length) return;
      if (ids.length > 1) {
        const items = await fetchItems(ids);
        if (items.every((i) => i.type === "track")) playMusic(items, c.index ?? 0, c);
        else {
          const first = items[c.index ?? 0] ?? items[0];
          if (first) playVideo(first.id, c.startMs);
        }
        return;
      }
      const item = await unwrap(api.GET("/items/{itemId}", { params: { path: { itemId: ids[0]! } } }));
      switch (item.type) {
        case "movie":
        case "episode":
        case "video":
          playVideo(item.id, c.startMs);
          break;
        case "track":
          playMusic([item], 0, c);
          break;
        case "show":
        case "season": {
          const [ep] = await fetchLeaves(item.id, { unwatched: true });
          const first = ep ?? (await fetchLeaves(item.id))[0];
          if (first) playVideo(first.id);
          break;
        }
        default: {
          // Albums, artists and collections: their tracks, or a collection's first title.
          const leaves = await fetchLeaves(item.id, c.shuffle ? { shuffle: true } : undefined);
          if (leaves[0]?.type === "track") playMusic(leaves, 0, c, item.title);
          else if (leaves[0]) playVideo(leaves[0].id);
        }
      }
    };
    const execute = async (c: RemoteCommand) => {
      const now = Date.now();
      if (c.from && (c.from !== lastFrom || now - lastCommandAt > SESSION_GAP_MS)) toast(`Controlled from ${c.from}`);
      lastFrom = c.from ?? lastFrom;
      lastCommandAt = now;
      const t = target();
      switch (c.type) {
        case "play":
          await play(c).catch((e) => toast(`Couldn't play that: ${(e as Error).message}`, { tone: "error" }));
          break;
        case "pause":
          t?.pause();
          break;
        case "resume":
          t?.resume();
          break;
        case "seek":
          t?.seek(c.positionMs ?? 0);
          break;
        case "stop":
          t?.stop();
          break;
        case "next":
          t?.next();
          break;
        case "previous":
          t?.previous();
          break;
        case "setAudio":
          if (c.streamId !== undefined) t?.setAudio?.(c.streamId);
          break;
        case "setSubtitle":
          if (c.streamId !== undefined) t?.setSubtitle?.(c.streamId);
          break;
        case "setVolume":
          if (c.volume !== undefined) t?.setVolume?.(c.volume);
          break;
      }
      schedule();
    };

    const loop = async () => {
      if (running) return;
      running = true;
      while (!disposed && active()) {
        ctrl = new AbortController();
        const signal = ctrl.signal;
        try {
          const { data, response } = await api.POST("/remote/inbox", {
            body: { cursor, capabilities: ["video", "music"], state: currentState() },
            signal,
          });
          if (!response.ok || !data) throw new Error(`inbox ${response.status}`);
          cursor = data.cursor;
          for (const c of data.commands) await execute(c);
        } catch {
          if (disposed || signal.aborted) break;
          await sleep(RETRY_MS, signal);
        }
      }
      running = false;
    };
    wakeRef.current = () => void loop();

    const onVisibility = () => {
      if (active()) {
        void loop();
        schedule();
      } else ctrl.abort(); // hidden and silent: stop being a player
    };
    document.addEventListener("visibilitychange", onVisibility);
    const offChanged = onRemoteStateChanged(schedule);
    const tick = window.setInterval(() => {
      if (currentState().state === "playing") report();
    }, REPORT_EVERY_MS);
    void loop();
    return () => {
      disposed = true;
      ctrl.abort();
      window.clearTimeout(reportTimer);
      window.clearInterval(tick);
      offChanged();
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, []);

  // Music changes: play/pause (which may also start the loop in a background tab), track, volume.
  const curId = music.current?.item.id;
  useEffect(() => {
    if (music.playing) wakeRef.current();
    scheduleRef.current();
  }, [music.playing, curId, music.volume, music.queue.index, music.queue.entries.length]);

  // A seek shows up as a jump in the playhead; a remote's start position is applied once playing.
  useEffect(() => {
    const jumped = Math.abs(music.time - lastTime.current) > 3;
    lastTime.current = music.time;
    if (jumped && !videoTarget()) scheduleRef.current();
    const p = pendingSeek.current;
    if (p && music.playing && curId === p.itemId && music.duration > 0) {
      pendingSeek.current = null;
      music.seek(p.ms / 1000);
    }
  }, [music.time, music.playing, music.duration, curId, music]);

  return null;
}
