import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { api, imageUrl, session as authSession, unwrap } from "@/api/client";
import type { ItemSummary, RadioRequest } from "@/api/types";
import { deviceProfile } from "./deviceProfile";
import { MiniPlayer, NowPlaying } from "./NowPlaying";
import * as Q from "./queue";

export type Levelling = "off" | "track" | "album" | "auto";
export type Source = { title: string; radio?: RadioRequest };

type Ctx = {
  source?: Source;
  /** Plays a generated station; radios top themselves up as they play. */
  playStation: (station: { title: string; items: ItemSummary[] }, radio?: RadioRequest) => void;
  levelling: Levelling;
  setLevelling: (l: Levelling) => void;
  /** Sleep timer: an epoch time to pause at, "track" (end of this track), or null. */
  sleep: number | "track" | null;
  setSleep: (s: number | "track" | null) => void;
  queue: Q.Queue;
  current?: Q.Entry;
  playing: boolean;
  time: number;
  duration: number;
  volume: number;
  expanded: boolean;
  play: (tracks: ItemSummary[], start?: number, opts?: { shuffle?: boolean }) => void;
  playNext: (tracks: ItemSummary[]) => void;
  addToQueue: (tracks: ItemSummary[]) => void;
  remove: (key: number) => void;
  move: (key: number, to: number) => void;
  jump: (key: number) => void;
  next: () => void;
  prev: () => void;
  toggle: () => void;
  seek: (seconds: number) => void;
  setVolume: (v: number) => void;
  toggleShuffle: () => void;
  cycleRepeat: () => void;
  setExpanded: (v: boolean) => void;
  close: () => void;
};

const MusicContext = createContext<Ctx | null>(null);

export function useMusic() {
  const ctx = useContext(MusicContext);
  if (!ctx) throw new Error("useMusic outside MusicProvider");
  return ctx;
}

/** A playback session bound to one of the two audio elements, with its levelling gains. */
type Loaded = { key: number; sessionId: string; ready: boolean; trackGain?: number; albumGain?: number; peak?: number };

function storedLevelling(): Levelling {
  try {
    const v = localStorage.getItem("marquee.levelling");
    return v === "off" || v === "track" || v === "album" ? v : "auto";
  } catch {
    return "auto";
  }
}

/** Linear gain for a track: ReplayGain dB, limited so the peak doesn't clip. */
function levelGain(l: Loaded | null, mode: Levelling, albumRun: boolean): number {
  if (!l || mode === "off") return 1;
  const db = mode === "album" || (mode === "auto" && albumRun) ? (l.albumGain ?? l.trackGain) : l.trackGain;
  if (db === undefined) return 1;
  let g = Math.pow(10, db / 20);
  if (l.peak && l.peak > 0) g = Math.min(g, 1 / l.peak);
  return Math.min(g, 4);
}

const PRELOAD_SECONDS = 15;

function storedVolume() {
  try {
    const v = Number(localStorage.getItem("marquee.volume"));
    return Number.isFinite(v) && v > 0 && v <= 1 ? v : 1;
  } catch {
    return 1;
  }
}

function stopSession(id: string | undefined) {
  if (id) fetch(`/api/v1/playback/sessions/${id}`, { method: "DELETE", keepalive: true, headers: { Authorization: `Bearer ${authSession.token}` } }).catch(() => {});
}

async function openSession(item: ItemSummary, preload: boolean) {
  return unwrap(api.POST("/playback/sessions", { body: { itemId: item.id, profile: deviceProfile(), startMs: 0, preload } }));
}

/** Records the final position (so the play counts) and then ends the session. */
function finishSession(id: string, positionMs: number) {
  fetch(`/api/v1/playback/sessions/${id}`, {
    method: "PATCH",
    keepalive: true,
    headers: { Authorization: `Bearer ${authSession.token}`, "Content-Type": "application/json" },
    body: JSON.stringify({ positionMs, state: "paused" }),
  })
    .catch(() => {})
    .finally(() => stopSession(id));
}

/**
 * Plays music app-wide with a queue (MUSIC-13). Two audio elements alternate: the next
 * track is loaded into the idle one shortly before the current track ends, so the switch
 * is near-gapless (MUSIC-9).
 */
export function MusicProvider({ children }: { children: ReactNode }) {
  const audios = useRef<(HTMLAudioElement | null)[]>([null, null]);
  const active = useRef(0);
  const loaded = useRef<[Loaded | null, Loaded | null]>([null, null]);
  const [queue, setQueue] = useState<Q.Queue>(Q.emptyQueue);
  const queueRef = useRef(queue);
  const [playing, setPlaying] = useState(false);
  const [time, setTime] = useState(0);
  const [duration, setDuration] = useState(0);
  const [volume, setVolumeState] = useState(storedVolume);
  const [expanded, setExpanded] = useState(false);
  const [source, setSource] = useState<Source | undefined>();
  const [levelling, setLevellingState] = useState<Levelling>(storedLevelling);
  const [sleep, setSleep] = useState<number | "track" | null>(null);
  const cur = Q.current(queue);

  // Web Audio graph for levelling: element → per-element gain → master (volume) → speakers.
  // Created on the first play (browsers require a user gesture).
  const graph = useRef<{ ctx: AudioContext; master: GainNode; gains: GainNode[] } | null>(null);
  const ensureGraph = useCallback(() => {
    if (graph.current || typeof AudioContext === "undefined") return graph.current;
    try {
      const ctx = new AudioContext();
      const master = ctx.createGain();
      master.connect(ctx.destination);
      const gains = audios.current.map((a) => {
        const g = ctx.createGain();
        if (a) {
          ctx.createMediaElementSource(a).connect(g);
          a.volume = 1;
        }
        g.connect(master);
        return g;
      });
      graph.current = { ctx, master, gains };
    } catch {
      graph.current = null;
    }
    return graph.current;
  }, []);
  const levellingRef = useRef(levelling);
  const volumeRef = useRef(volume);
  useEffect(() => {
    levellingRef.current = levelling;
    volumeRef.current = volume;
  });
  /** Applies volume and levelling to element i (album gain when the previous track was from the same album). */
  const applyGain = useCallback((i: number) => {
    const g = graph.current;
    const a = audios.current[i];
    const q = queueRef.current;
    const curEntry = q.entries[q.index];
    const prevEntry = q.entries[q.index - 1];
    const albumRun = !!curEntry && !!prevEntry && curEntry.item.parentId === prevEntry.item.parentId;
    const gain = levelGain(loaded.current[i] ?? null, levellingRef.current, albumRun);
    if (g) {
      g.master.gain.value = volumeRef.current;
      g.gains[i]!.gain.value = gain;
      if (g.ctx.state === "suspended") g.ctx.resume().catch(() => {});
    } else if (a) {
      a.volume = Math.min(1, volumeRef.current * gain);
    }
  }, []);

  useEffect(() => {
    queueRef.current = queue;
  }, [queue]);

  const el = (i: number) => audios.current[i]!;
  const activeEl = useCallback(() => audios.current[active.current] ?? null, []);

  const report = useCallback((state: "playing" | "paused") => {
    const l = loaded.current[active.current];
    const a = audios.current[active.current];
    if (l && a) api.PATCH("/playback/sessions/{sessionId}", { params: { path: { sessionId: l.sessionId } }, body: { positionMs: Math.round(a.currentTime * 1000), state } }).catch(() => {});
  }, []);

  const unload = useCallback((i: number) => {
    const l = loaded.current[i];
    loaded.current[i] = null;
    const a = audios.current[i];
    const position = a && a.src ? Math.round(a.currentTime * 1000) : 0;
    if (a) {
      a.pause();
      a.removeAttribute("src");
      a.load();
    }
    if (l?.sessionId) finishSession(l.sessionId, position);
  }, []);

  /** Loads an entry into element i. */
  const loadInto = useCallback(async (i: number, entry: Q.Entry, preload = false) => {
    const s = await openSession(entry.item, preload);
    if (loaded.current[i]) stopSession(loaded.current[i]!.sessionId);
    loaded.current[i] = { key: entry.key, sessionId: s.id, ready: false, trackGain: s.trackGainDb, albumGain: s.albumGainDb, peak: s.peak };
    const a = audios.current[i]!;
    a.src = s.url;
    a.preload = "auto";
    a.load();
    loaded.current[i]!.ready = true;
  }, []);

  // Start the current entry unless it's already loaded in the active element (after a
  // gapless switch) or waiting in the idle one (preloaded).
  const curKey = cur?.key;
  useEffect(() => {
    if (curKey === undefined) return;
    const entry = Q.current(queueRef.current)!;
    if (loaded.current[active.current]?.key === curKey) return;
    let cancelled = false;
    const idle = (1 - active.current) as 0 | 1;
    (async () => {
      unload(active.current);
      const pre = loaded.current[idle];
      if (pre?.ready && pre.key === curKey) {
        active.current = idle;
      } else {
        try {
          await loadInto(active.current, entry);
        } catch {
          // Skip tracks that can't be played.
          if (!cancelled) setQueue((q) => (Q.skipIndex(q) >= 0 && Q.skipIndex(q) !== q.index ? { ...q, index: Q.skipIndex(q) } : q));
          return;
        }
      }
      if (cancelled) return;
      const a = activeEl()!;
      ensureGraph();
      applyGain(active.current);
      if (isFinite(a.duration)) setDuration(a.duration);
      a.play().catch(() => setPlaying(false));
    })();
    return () => {
      cancelled = true;
    };
    // Volume is applied separately; only a track change restarts playback.
  }, [curKey, loadInto, unload, activeEl, ensureGraph, applyGain]);

  // Stations keep going: fetch more when the end of the queue gets close.
  const refilling = useRef(false);
  useEffect(() => {
    const radio = source?.radio;
    if (!radio || refilling.current || queue.entries.length - queue.index > 5) return;
    refilling.current = true;
    const exclude = queue.entries.map((e) => e.item.id);
    unwrap(api.POST("/music/radio", { body: { ...radio, exclude, limit: 25 } }))
      .then((st) => setQueue((q) => Q.append(q, st.items.filter((t) => !exclude.includes(t.id)))))
      .catch(() => {})
      .finally(() => (refilling.current = false));
  }, [queue, source]);

  // Sleep timer.
  useEffect(() => {
    if (typeof sleep !== "number") return;
    const t = window.setTimeout(() => {
      activeEl()?.pause();
      setSleep(null);
    }, Math.max(0, sleep - Date.now()));
    return () => window.clearTimeout(t);
  }, [sleep, activeEl]);

  // Lock-screen / OS media metadata.
  useEffect(() => {
    if (!cur || !("mediaSession" in navigator)) return;
    const t = cur.item;
    navigator.mediaSession.metadata = new MediaMetadata({
      title: t.title,
      artist: t.artistCredit ?? t.grandparentTitle ?? "",
      album: t.parentTitle ?? "",
      artwork: t.images?.poster ? [{ src: imageUrl(t.images.poster, 256), sizes: "512x512" }] : [],
    });
  }, [cur]);

  useEffect(() => {
    const t = window.setInterval(() => !activeEl()?.paused && report("playing"), 15_000);
    const onHide = () =>
      loaded.current.forEach((l, i) => {
        const a = audios.current[i];
        if (l?.sessionId) finishSession(l.sessionId, a ? Math.round(a.currentTime * 1000) : 0);
      });
    window.addEventListener("pagehide", onHide);
    return () => {
      window.clearInterval(t);
      window.removeEventListener("pagehide", onHide);
    };
  }, [report, activeEl]);

  const goTo = useCallback((index: number) => setQueue((q) => (index < 0 ? q : { ...q, index })), []);
  const next = useCallback(() => goTo(Q.skipIndex(queueRef.current)), [goTo]);
  const prev = useCallback(() => {
    const a = activeEl();
    if (a && a.currentTime > 3) a.currentTime = 0;
    else goTo(Math.max(0, queueRef.current.index - 1));
  }, [goTo, activeEl]);
  const toggle = useCallback(() => {
    const a = activeEl();
    if (!a) return;
    if (a.paused) a.play().catch(() => {});
    else a.pause();
  }, [activeEl]);
  const close = useCallback(() => {
    report("paused");
    unload(0);
    unload(1);
    setQueue(Q.emptyQueue);
    setSource(undefined);
    setSleep(null);
    setExpanded(false);
    setPlaying(false);
  }, [report, unload]);

  useEffect(() => {
    if (!("mediaSession" in navigator)) return;
    navigator.mediaSession.setActionHandler("nexttrack", next);
    navigator.mediaSession.setActionHandler("previoustrack", prev);
    navigator.mediaSession.setActionHandler("play", () => activeEl()?.play());
    navigator.mediaSession.setActionHandler("pause", () => activeEl()?.pause());
  }, [next, prev, activeEl]);

  const onTimeUpdate = (i: number) => {
    if (i !== active.current) return;
    const a = el(i);
    setTime(a.currentTime);
    // Preload what follows into the idle element.
    const q = queueRef.current;
    const fi = Q.followingIndex(q);
    const idle = (1 - i) as 0 | 1;
    const following = q.entries[fi];
    if (following && fi !== q.index && isFinite(a.duration) && a.duration - a.currentTime < PRELOAD_SECONDS && !loaded.current[idle]) {
      loaded.current[idle] = { key: following.key, sessionId: "", ready: false }; // reserve
      loadInto(idle, following, true).catch(() => (loaded.current[idle] = null));
    }
  };

  const onEnded = (i: number) => {
    if (i !== active.current) return;
    report("paused");
    if (sleep === "track") {
      setSleep(null);
      setPlaying(false);
      return;
    }
    const q = queueRef.current;
    const fi = Q.followingIndex(q);
    if (fi < 0) {
      setPlaying(false);
      return;
    }
    if (fi === q.index) {
      const a = el(i);
      a.currentTime = 0;
      a.play().catch(() => {});
      return;
    }
    const idle = (1 - i) as 0 | 1;
    const pre = loaded.current[idle];
    if (pre?.ready && pre.key === q.entries[fi]?.key) {
      // Gapless switch: start the preloaded element now, then let state catch up.
      active.current = idle;
      const b = el(idle);
      b.play().catch(() => {});
      setDuration(b.duration);
      setTime(0);
      unload(i);
    }
    setQueue({ ...q, index: fi });
    // Levelling for the new track (album gain if it continues the album).
    queueRef.current = { ...q, index: fi };
    applyGain(active.current);
  };

  const setVolume = (v: number) => {
    setVolumeState(v);
    volumeRef.current = v;
    applyGain(active.current);
    try {
      localStorage.setItem("marquee.volume", String(v));
    } catch {
      /* storage unavailable */
    }
  };

  const ctx: Ctx = {
    source,
    playStation: (station, radio) => {
      unload(1 - active.current);
      setSource({ title: station.title, radio });
      setQueue((q) => Q.load(q, station.items, 0));
    },
    levelling,
    setLevelling: (l) => {
      setLevellingState(l);
      levellingRef.current = l;
      applyGain(active.current);
      try {
        localStorage.setItem("marquee.levelling", l);
      } catch {
        /* storage unavailable */
      }
    },
    sleep,
    setSleep,
    queue,
    current: cur,
    playing,
    time,
    duration,
    volume,
    expanded,
    play: (tracks, start = 0, opts) => {
      unload(1 - active.current);
      setSource(undefined);
      setQueue((q) => Q.load(q, tracks, start, opts?.shuffle));
    },
    playNext: (tracks) => {
      unload(1 - active.current); // the preloaded "next" track may have changed
      setQueue((q) => Q.playNext(q, tracks));
    },
    addToQueue: (tracks) => setQueue((q) => Q.append(q, tracks)),
    remove: (key) => {
      unload(1 - active.current);
      setQueue((q) => Q.remove(q, key));
    },
    move: (key, to) => {
      unload(1 - active.current);
      setQueue((q) => Q.move(q, key, to));
    },
    jump: (key) => setQueue((q) => Q.jump(q, key)),
    next,
    prev,
    toggle,
    seek: (s) => {
      const a = activeEl();
      if (a) a.currentTime = s;
    },
    setVolume,
    toggleShuffle: () => {
      unload(1 - active.current);
      setQueue((q) => Q.toggleShuffle(q));
    },
    cycleRepeat: () => {
      unload(1 - active.current);
      setQueue((q) => Q.cycleRepeat(q));
    },
    setExpanded,
    close,
  };

  return (
    <MusicContext.Provider value={ctx}>
      {children}
      {[0, 1].map((i) => (
        <audio
          key={i}
          crossOrigin="anonymous"
          ref={(a) => {
            audios.current[i] = a;
          }}
          onPlay={() => {
            if (i !== active.current) return;
            setPlaying(true);
            report("playing");
          }}
          onPause={() => {
            if (i !== active.current) return;
            setPlaying(false);
            report("paused");
          }}
          onTimeUpdate={() => onTimeUpdate(i)}
          onDurationChange={(e) => i === active.current && setDuration(e.currentTarget.duration)}
          onLoadedMetadata={(e) => i === active.current && setDuration(e.currentTarget.duration)}
          onEnded={() => onEnded(i)}
        />
      ))}
      {cur && (expanded ? <NowPlaying /> : <MiniPlayer />)}
    </MusicContext.Provider>
  );
}
