import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { api, imageUrl, session as authSession, unwrap } from "@/api/client";
import type { ItemSummary, RadioRequest } from "@/api/types";
import { deviceProfile } from "./deviceProfile";
import {
  createEqChain,
  routeEq,
  storedEq,
  storeEq,
  type EqChain,
  type EqSettings,
} from "./eqDsp";
import { MiniPlayer, NowPlaying } from "./NowPlaying";
import * as Q from "./queue";
import type { StreamInfo } from "./audioQuality";
import { albumRun, levelGain, type Levelling } from "./levelling";

export type { Levelling } from "./levelling";
export type DJMode = "wander" | "superfan" | "deep_cuts" | "same_era";
/** Older stored DJ mode names, carried over to the current ones. */
const legacyDJ: Record<string, DJMode> = { stretch: "wander", groupie: "superfan", contempo: "same_era" };
export type Source = { title: string; radio?: RadioRequest };

/** Player state that changes on track/queue/setting changes, not with the playhead. */
type State = {
  source?: Source;
  levelling: Levelling;
  /** Crossfade between tracks in seconds (0 = off); never within an album played in order. */
  crossfade: number;
  /** DJ (MUSIC-6): weaves a track in every few songs. */
  dj: DJMode | null;
  /** Sleep timer: an epoch time to pause at, "track" (end of this track), or null. */
  sleep: number | "track" | null;
  queue: Q.Queue;
  current?: Q.Entry;
  playing: boolean;
  volume: number;
  expanded: boolean;
  /** The equaliser, stored per browser. */
  eq: EqSettings;
  /** False when this browser has no Web Audio, so the equaliser can't work. */
  eqSupported: boolean;
  /** What is streamed for the current track (direct play or a transcode), for the quality badge. */
  stream?: StreamInfo;
};

/** The playhead, which updates several times a second while playing. */
type Time = { time: number; duration: number };

/** Stable functions: components that only start playback never re-render with the player. */
type Actions = {
  /** Plays a generated station; radios top themselves up as they play. */
  playStation: (
    station: { title: string; items: ItemSummary[] },
    radio?: RadioRequest,
  ) => void;
  setCrossfade: (s: number) => void;
  setDJ: (m: DJMode | null) => void;
  setSleep: (s: number | "track" | null) => void;
  setEq: (eq: EqSettings) => void;
  /** source names what's playing (an album or playlist) for the Now Playing header. */
  play: (
    tracks: ItemSummary[],
    start?: number,
    opts?: { shuffle?: boolean; source?: string },
  ) => void;
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

const ActionsContext = createContext<Actions | null>(null);
const StateContext = createContext<(Actions & State) | null>(null);
const TimeContext = createContext<Time>({ time: 0, duration: 0 });

/** Just the player's actions (play, queue…); never re-renders as music plays. */
export function useMusicActions() {
  const ctx = useContext(ActionsContext);
  if (!ctx) throw new Error("useMusicActions outside MusicProvider");
  return ctx;
}

/** Actions and player state, without the playhead (re-renders on track and setting changes). */
export function useMusicState() {
  const ctx = useContext(StateContext);
  if (!ctx) throw new Error("useMusicState outside MusicProvider");
  return ctx;
}

/** Everything, including the playhead: re-renders several times a second while playing. */
export function useMusic() {
  const state = useMusicState();
  const time = useContext(TimeContext);
  return useMemo(() => ({ ...state, ...time }), [state, time]);
}

/** A playback session bound to one of the two audio elements, with its levelling gains. */
type Loaded = {
  key: number;
  sessionId: string;
  ready: boolean;
  trackGain?: number;
  albumGain?: number;
  peak?: number;
};


/** Moves an audio parameter to `value` over a few milliseconds, without clicks. */
function glide(ctx: AudioContext, p: AudioParam, value: number) {
  const now = ctx.currentTime;
  p.cancelScheduledValues(now);
  p.setValueAtTime(p.value, now);
  p.setTargetAtTime(value, now, 0.015);
}

const PRELOAD_SECONDS = 15;
const DJ_EVERY = 3; // a DJ pick after this many of your own tracks

function stored<T extends string | number>(
  key: string,
  fallback: T,
  valid: (v: string) => boolean,
): T {
  try {
    const v = localStorage.getItem(key);
    if (v !== null && valid(v))
      return (typeof fallback === "number" ? Number(v) : v) as T;
  } catch {
    /* storage unavailable */
  }
  return fallback;
}

function store(key: string, v: string | number | null) {
  try {
    if (v === null) localStorage.removeItem(key);
    else localStorage.setItem(key, String(v));
  } catch {
    /* storage unavailable */
  }
}

function storedVolume() {
  try {
    const v = Number(localStorage.getItem("marquee.volume"));
    return Number.isFinite(v) && v > 0 && v <= 1 ? v : 1;
  } catch {
    return 1;
  }
}

function stopSession(id: string | undefined) {
  if (id)
    fetch(`/api/v1/playback/sessions/${id}`, {
      method: "DELETE",
      keepalive: true,
      headers: { Authorization: `Bearer ${authSession.token}` },
    }).catch(() => {});
}

async function openSession(item: ItemSummary, preload: boolean) {
  return unwrap(
    api.POST("/playback/sessions", {
      body: { itemId: item.id, profile: deviceProfile(), startMs: 0, preload },
    }),
  );
}

/** Records the final position (so the play counts) and then ends the session. */
function finishSession(id: string, positionMs: number) {
  fetch(`/api/v1/playback/sessions/${id}`, {
    method: "PATCH",
    keepalive: true,
    headers: {
      Authorization: `Bearer ${authSession.token}`,
      "Content-Type": "application/json",
    },
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
  // Bumped whenever an element is (re)loaded or unloaded, so a slow load that has been
  // overtaken (fast skipping) drops its own session instead of taking over the element.
  const loadGen = useRef([0, 0]);
  const [queue, setQueue] = useState<Q.Queue>(Q.emptyQueue);
  const queueRef = useRef(queue);
  const [playing, setPlaying] = useState(false);
  const [time, setTime] = useState(0);
  const [duration, setDuration] = useState(0);
  const [volume, setVolumeState] = useState(storedVolume);
  const [expanded, setExpanded] = useState(false);
  // Playback decisions by queue entry key (the last few), for the audio quality badge.
  const [streams, setStreams] = useState<Record<number, StreamInfo>>({});
  const [source, setSource] = useState<Source | undefined>();
  // Volume levelling is always on (no setting): album gain while an album plays in order,
  // else track gain.
  const levelling: Levelling = "auto";
  const [sleep, setSleep] = useState<number | "track" | null>(null);
  const [crossfade, setCrossfadeState] = useState<number>(() =>
    stored<number>(
      "marquee.crossfade",
      0,
      (v) => Number(v) >= 0 && Number(v) <= 12,
    ),
  );
  const [dj, setDJState] = useState<DJMode | null>(
    () =>
      (() => {
        const v = stored<string>("marquee.dj", "", (v) =>
          ["wander", "superfan", "deep_cuts", "same_era", ...Object.keys(legacyDJ)].includes(v),
        );
        return (legacyDJ[v] ?? v) as DJMode;
      })() || null,
  );
  const crossfadeRef = useRef(crossfade);
  // Queue entry keys whose stream was already reopened once, and whose preload failed.
  const retried = useRef(new Set<number>());
  const preloadFailed = useRef(new Set<number>());
  // The listener paused (not a stall or error), so a reopened stream stays paused.
  const userPaused = useRef(false);
  const fading = useRef(false);
  const cur = Q.current(queue);

  const [eq, setEqState] = useState<EqSettings>(storedEq);
  const eqRef = useRef(eq);

  // Web Audio graph for levelling: element → per-element gain → master (volume) →
  // equaliser (when on) → limiter → speakers. Created on the first play (browsers require a
  // user gesture).
  const graph = useRef<{
    ctx: AudioContext;
    master: GainNode;
    gains: GainNode[];
    eq: EqChain;
    limiter: DynamicsCompressorNode;
  } | null>(null);
  const ensureGraph = useCallback(() => {
    if (graph.current || typeof AudioContext === "undefined")
      return graph.current;
    try {
      // "playback" asks for a larger output buffer: with the default low-latency one, a busy
      // page (Now Playing redrawing) can starve the audio thread, which is heard as pops.
      const ctx = new AudioContext({ latencyHint: "playback" });
      const master = ctx.createGain();
      const chain = createEqChain(ctx);
      // A brick-wall limiter just under full scale: equaliser boosts can't hard-clip.
      const limiter = ctx.createDynamicsCompressor();
      limiter.threshold.value = -1;
      limiter.knee.value = 0;
      limiter.ratio.value = 20;
      limiter.attack.value = 0.002;
      limiter.release.value = 0.1;
      limiter.connect(ctx.destination);
      routeEq(chain, master, limiter, eqRef.current);
      const gains = audios.current.map((a) => {
        const g = ctx.createGain();
        if (a) {
          ctx.createMediaElementSource(a).connect(g);
          a.volume = 1;
        }
        g.connect(master);
        return g;
      });
      // The elements keep playing (and the progress bar moving) while a suspended or
      // interrupted context is silent, so bring it back whenever that happens mid-play.
      const wake = () => {
        const a = audios.current[active.current];
        if (ctx.state !== "running" && ctx.state !== "closed" && a && !a.paused)
          ctx.resume().catch(() => {});
      };
      ctx.addEventListener("statechange", wake);
      document.addEventListener("visibilitychange", wake);
      graph.current = { ctx, master, gains, eq: chain, limiter };
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
    crossfadeRef.current = crossfade;
  });
  /** Element i's levelling gain, for the queue entry it holds (album gain within an album). */
  const gainFor = useCallback((i: number) => {
    const l = loaded.current[i];
    const q = queueRef.current;
    const at = l ? q.entries.findIndex((e) => e.key === l.key) : -1;
    return levelGain(
      l,
      levellingRef.current,
      at >= 0 && albumRun(q.entries, at),
      // Boosts may lean on the limiter only when the Web Audio graph (with it) is there.
      !!graph.current,
    );
  }, []);
  /** A suspended or interrupted context (mobile Safari after a lock or a call) plays silence while the element's clock runs on. */
  const resumeCtx = useCallback(() => {
    const g = graph.current;
    if (g && g.ctx.state !== "running" && g.ctx.state !== "closed")
      g.ctx.resume().catch(() => {});
  }, []);
  /** Applies volume and levelling to element i. */
  /** `starting`: element i hasn't begun playing yet, so its gain is set at once, not glided. */
  const applyGain = useCallback((i: number, starting = false) => {
    const g = graph.current;
    const a = audios.current[i];
    const gain = gainFor(i);
    if (g) {
      // A short glide rather than a jump: stepping a gain mid-waveform clicks.
      glide(g.ctx, g.master.gain, volumeRef.current);
      // A crossfade schedules the elements' gains itself; leave them alone meanwhile.
      if (starting) {
        const p = g.gains[i]!.gain;
        p.cancelScheduledValues(g.ctx.currentTime);
        p.setValueAtTime(gain, g.ctx.currentTime);
      } else if (!fading.current) glide(g.ctx, g.gains[i]!.gain, gain);
      resumeCtx();
    } else if (a) {
      a.volume = Math.min(1, volumeRef.current * gain);
    }
  }, [gainFor, resumeCtx]);

  useEffect(() => {
    queueRef.current = queue;
  }, [queue]);

  const el = (i: number) => audios.current[i]!;
  const activeEl = useCallback(
    () => audios.current[active.current] ?? null,
    [],
  );

  const report = useCallback((state: "playing" | "paused") => {
    const l = loaded.current[active.current];
    const a = audios.current[active.current];
    if (l && a)
      api
        .PATCH("/playback/sessions/{sessionId}", {
          params: { path: { sessionId: l.sessionId } },
          body: { positionMs: Math.round(a.currentTime * 1000), state },
        })
        .catch(() => {});
  }, []);

  const unload = useCallback((i: number) => {
    loadGen.current[i]!++;
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

  /** Loads an entry into element i; false when a newer load or unload overtook it. */
  const loadInto = useCallback(
    async (i: number, entry: Q.Entry, preload = false): Promise<boolean> => {
      const gen = ++loadGen.current[i]!;
      let s;
      try {
        s = await openSession(entry.item, preload);
      } catch (e) {
        if (loadGen.current[i] !== gen) return false;
        throw e;
      }
      if (loadGen.current[i] !== gen) {
        stopSession(s.id);
        return false;
      }
      if (loaded.current[i]) stopSession(loaded.current[i]!.sessionId);
      loaded.current[i] = {
        key: entry.key,
        sessionId: s.id,
        ready: false,
        trackGain: s.trackGainDb,
        albumGain: s.albumGainDb,
        peak: s.peak,
      };
      const info: StreamInfo = { decision: s.decision, limitKbps: s.limitKbps };
      setStreams((m) => Object.fromEntries([...Object.entries(m).slice(-7), [entry.key, info]]));
      const a = audios.current[i]!;
      a.src = s.url;
      a.preload = "auto";
      a.load();
      loaded.current[i]!.ready = true;
      return true;
    },
    [],
  );

  /** Moves past a track that can't be played (unless it's the only one left). */
  const skipFailed = useCallback(
    () =>
      setQueue((q) =>
        Q.skipIndex(q) >= 0 && Q.skipIndex(q) !== q.index
          ? { ...q, index: Q.skipIndex(q) }
          : q,
      ),
    [],
  );

  // Start the current entry unless it's already loaded in the active element (after a
  // gapless switch) or waiting in the idle one (preloaded).
  const curKey = cur?.key;
  useEffect(() => {
    if (curKey === undefined) return;
    const entry = Q.current(queueRef.current)!;
    if (loaded.current[active.current]?.key === curKey) return;
    let cancelled = false;
    const idle = (1 - active.current) as 0 | 1;
    // The previous track's time must not stay on screen while this one loads.
    setTime(0);
    userPaused.current = false; // a new track starts playing
    (async () => {
      unload(active.current);
      const pre = loaded.current[idle];
      // Reuse a preloaded element only when it holds this track and isn't the tail of a
      // crossfade (that element is about to be unloaded and its gain is ramping to 0).
      if (pre?.ready && pre.key === curKey && !fading.current) {
        active.current = idle;
      } else {
        // Whatever the idle element holds (another track, a half-done preload of this one,
        // a fading tail) is stale now: free it so what follows this track can preload.
        if (pre) unload(idle);
        try {
          if (!(await loadInto(active.current, entry))) return; // overtaken
        } catch {
          // Skip tracks that can't be played.
          if (!cancelled) skipFailed();
          return;
        }
      }
      if (cancelled) return;
      const a = activeEl()!;
      ensureGraph();
      applyGain(active.current, true);
      if (isFinite(a.duration)) setDuration(a.duration);
      a.play().catch(() => setPlaying(false));
    })();
    return () => {
      cancelled = true;
    };
    // Volume is applied separately; only a track change restarts playback.
  }, [curKey, loadInto, unload, activeEl, ensureGraph, applyGain, skipFailed]);

  // Stations keep going: fetch more when the end of the queue gets close.
  const refilling = useRef(false);
  useEffect(() => {
    const radio = source?.radio;
    if (!radio || refilling.current || queue.entries.length - queue.index > 5)
      return;
    refilling.current = true;
    const exclude = queue.entries.map((e) => e.item.id);
    unwrap(api.POST("/music/radio", { body: { ...radio, exclude, limit: 25 } }))
      .then((st) =>
        setQueue((q) =>
          Q.append(
            q,
            st.items.filter((t) => !exclude.includes(t.id)),
          ),
        ),
      )
      .catch(() => {})
      .finally(() => (refilling.current = false));
  }, [queue, source]);

  // DJ: after every few of your tracks, weave one in from the DJ.
  const djCount = useRef(0);
  const djBusy = useRef(false);
  useEffect(() => {
    if (!dj || curKey === undefined || djBusy.current) return;
    const q = queueRef.current;
    const entry = q.entries[q.index];
    if (!entry || entry.dj) {
      djCount.current = 0;
      return;
    }
    djCount.current += 1;
    const following = q.entries[q.index + 1];
    if (djCount.current < DJ_EVERY || following?.dj) return;
    djBusy.current = true;
    const exclude = q.entries.map((e) => e.item.id);
    unwrap(
      api.POST("/music/dj", {
        body: { trackId: entry.item.id, mode: dj, exclude },
      }),
    )
      .then((pick) => {
        if (queueRef.current.entries[queueRef.current.index]?.key !== entry.key)
          return; // moved on
        unload(1 - active.current); // the preloaded "next" track changes
        setQueue((cq) => Q.playNext(cq, [pick], dj));
        djCount.current = 0;
      })
      .catch(() => {})
      .finally(() => (djBusy.current = false));
  }, [curKey, dj, unload]);

  // Sleep timer.
  useEffect(() => {
    if (typeof sleep !== "number") return;
    const t = window.setTimeout(
      () => {
        userPaused.current = true;
        activeEl()?.pause();
        setSleep(null);
      },
      Math.max(0, sleep - Date.now()),
    );
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
      artwork: t.images?.poster
        ? [{ src: imageUrl(t.images.poster, 256), sizes: "512x512" }]
        : [],
    });
  }, [cur]);

  useEffect(() => {
    const t = window.setInterval(
      () => !activeEl()?.paused && report("playing"),
      15_000,
    );
    const onHide = () =>
      loaded.current.forEach((l, i) => {
        const a = audios.current[i];
        if (l?.sessionId)
          finishSession(l.sessionId, a ? Math.round(a.currentTime * 1000) : 0);
      });
    window.addEventListener("pagehide", onHide);
    return () => {
      window.clearInterval(t);
      window.removeEventListener("pagehide", onHide);
    };
  }, [report, activeEl]);

  const goTo = useCallback(
    (index: number) => setQueue((q) => (index < 0 ? q : { ...q, index })),
    [],
  );
  const next = useCallback(() => goTo(Q.skipIndex(queueRef.current)), [goTo]);
  const prev = useCallback(() => {
    const a = activeEl();
    if (a && a.currentTime > 3) a.currentTime = 0;
    else goTo(Math.max(0, queueRef.current.index - 1));
  }, [goTo, activeEl]);
  const toggle = useCallback(() => {
    const a = activeEl();
    if (!a) return;
    if (a.paused) {
      userPaused.current = false;
      resumeCtx();
      a.play().catch(() => {});
    } else {
      userPaused.current = true;
      a.pause();
    }
  }, [activeEl, resumeCtx]);
  const close = useCallback(() => {
    report("paused");
    unload(0);
    unload(1);
    retried.current.clear();
    preloadFailed.current.clear();
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
    navigator.mediaSession.setActionHandler("play", () => {
      userPaused.current = false;
      resumeCtx();
      activeEl()?.play().catch(() => {});
    });
    navigator.mediaSession.setActionHandler("pause", () => {
      userPaused.current = true;
      activeEl()?.pause();
    });
  }, [next, prev, activeEl, resumeCtx]);

  const onTimeUpdate = (i: number) => {
    if (i !== active.current) return;
    const a = el(i);
    setTime(a.currentTime);
    // Preload what follows into the idle element.
    const q = queueRef.current;
    const fi = Q.followingIndex(q);
    const idle = (1 - i) as 0 | 1;
    const following = q.entries[fi];
    if (
      following &&
      fi !== q.index &&
      isFinite(a.duration) &&
      a.duration - a.currentTime < PRELOAD_SECONDS &&
      !loaded.current[idle] &&
      // A preload that already failed is loaded afresh when its turn comes instead.
      !preloadFailed.current.has(following.key)
    ) {
      loaded.current[idle] = {
        key: following.key,
        sessionId: "",
        ready: false,
      }; // reserve
      const gen = loadGen.current[idle]! + 1;
      loadInto(idle, following, true).catch(() => {
        if (loadGen.current[idle] !== gen) return;
        loaded.current[idle] = null;
        preloadFailed.current.add(following.key);
      });
    }
    // Crossfade into what follows, except within an album played in order (gapless albums
    // stay gapless) and when the sleep timer ends this track.
    const cf = crossfadeRef.current;
    const g = graph.current;
    const pre = loaded.current[idle];
    const sameAlbum =
      !!following &&
      following.item.parentId === q.entries[q.index]?.item.parentId &&
      (following.item.index ?? 0) ===
        (q.entries[q.index]?.item.index ?? -1) + 1;
    if (
      cf > 0 &&
      g &&
      following &&
      fi !== q.index &&
      !fading.current &&
      pre?.ready &&
      pre.key === following.key &&
      !sameAlbum &&
      sleep !== "track" &&
      isFinite(a.duration) &&
      a.duration - a.currentTime <= cf &&
      a.duration > cf * 2
    ) {
      fading.current = true;
      const now = g.ctx.currentTime;
      const left = Math.max(0.5, a.duration - a.currentTime);
      const out = g.gains[i]!;
      out.gain.setValueAtTime(out.gain.value, now);
      out.gain.linearRampToValueAtTime(0, now + left);
      // Hand over: the next track becomes the active one while this one fades out.
      active.current = idle;
      const nq = { ...q, index: fi };
      queueRef.current = nq;
      const target = gainFor(idle);
      const into = g.gains[idle]!;
      into.gain.setValueAtTime(0, now);
      into.gain.linearRampToValueAtTime(target, now + left);
      const b = el(idle);
      b.play().catch(() => {});
      setDuration(b.duration);
      setTime(0);
      setQueue(nq);
      const tail = loadGen.current[i];
      window.setTimeout(
        () => {
          // Only the faded-out track: a skip meanwhile may have reloaded this element or
          // made it the active one again, and unloading that would leave silence.
          if (loadGen.current[i] === tail && active.current !== i) unload(i);
          fading.current = false;
        },
        left * 1000 + 250,
      );
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
    // Levelling for the new track (album gain if it continues the album).
    queueRef.current = { ...q, index: fi };
    if (pre?.ready && pre.key === q.entries[fi]?.key) {
      // Gapless switch: set the preloaded element's level before it starts (a jump just
      // after the first samples is heard as a click), start it, then let state catch up.
      active.current = idle;
      applyGain(idle, true);
      const b = el(idle);
      b.play().catch(() => {});
      setDuration(b.duration);
      setTime(0);
      unload(i);
    }
    // Otherwise the current-track effect loads it fresh (and sets its level first).
    setQueue({ ...q, index: fi });
  };

  /**
   * An element's stream failed, typically because its session URL is gone (404 once the
   * server has ended it). The current track gets one fresh session, resuming where it
   * stopped, before it's skipped; a failed preload is dropped so the track is requested
   * afresh when its turn comes, never skipped silently.
   */
  const onError = (i: number) => {
    const l = loaded.current[i];
    const a = audios.current[i];
    if (!l?.ready || !a?.getAttribute("src")) return; // unloaded, or still being set up
    if (i !== active.current) {
      preloadFailed.current.add(l.key);
      unload(i);
      return;
    }
    const q = queueRef.current;
    const entry = q.entries.find((e) => e.key === l.key);
    if (!entry || retried.current.has(l.key)) {
      skipFailed();
      return;
    }
    retried.current.add(l.key);
    const position = a.currentTime;
    const wasPlaying = !userPaused.current;
    loadInto(i, entry)
      .then((ok) => {
        if (!ok || active.current !== i) return;
        const sid = loaded.current[i]?.sessionId;
        const start = () => {
          if (loaded.current[i]?.sessionId !== sid) return; // replaced since
          if (position > 0) a.currentTime = position;
          applyGain(i, true);
          if (wasPlaying) a.play().catch(() => setPlaying(false));
        };
        if (a.readyState >= HTMLMediaElement.HAVE_METADATA) start();
        else a.addEventListener("loadedmetadata", start, { once: true });
      })
      .catch(() => {
        if (active.current === i && loaded.current[i]?.key === l.key) skipFailed();
      });
  };

  const actions = useMemo<Actions>(
    () => ({
      playStation: (station, radio) => {
        unload(1 - active.current);
        setSource({ title: station.title, radio });
        setQueue((q) => Q.load(q, station.items, 0));
      },
      setCrossfade: (s) => {
        setCrossfadeState(s);
        crossfadeRef.current = s;
        store("marquee.crossfade", s);
      },
      setDJ: (m) => {
        setDJState(m);
        djCount.current = 0;
        store("marquee.dj", m);
      },
      setSleep,
      setEq: (next) => {
        setEqState(next);
        eqRef.current = next;
        storeEq(next);
        const g = graph.current;
        if (g) {
          routeEq(g.eq, g.master, g.limiter, next);
          // Changing it is a user gesture, the moment a suspended context may resume.
          if (g.ctx.state === "suspended") g.ctx.resume().catch(() => {});
        }
      },
      play: (tracks, start = 0, opts) => {
        unload(1 - active.current);
        setSource(opts?.source ? { title: opts.source } : undefined);
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
      setVolume: (v) => {
        setVolumeState(v);
        volumeRef.current = v;
        applyGain(active.current);
        try {
          localStorage.setItem("marquee.volume", String(v));
        } catch {
          /* storage unavailable */
        }
      },
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
    }),
    [unload, applyGain, next, prev, toggle, activeEl, close],
  );

  const state = useMemo(
    () => ({
      ...actions,
      source,
      levelling,
      crossfade,
      dj,
      sleep,
      queue,
      current: cur,
      playing,
      volume,
      expanded,
      eq,
      eqSupported: typeof AudioContext !== "undefined",
      stream: cur ? streams[cur.key] : undefined,
    }),
    [
      actions,
      source,
      levelling,
      crossfade,
      dj,
      sleep,
      queue,
      cur,
      playing,
      volume,
      expanded,
      eq,
      streams,
    ],
  );
  const timeValue = useMemo(() => ({ time, duration }), [time, duration]);

  return (
    <ActionsContext.Provider value={actions}>
      <StateContext.Provider value={state}>
        <TimeContext.Provider value={timeValue}>
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
                resumeCtx();
                setPlaying(true);
                report("playing");
              }}
              onPause={() => {
                if (i !== active.current) return;
                setPlaying(false);
                report("paused");
              }}
              onTimeUpdate={() => onTimeUpdate(i)}
              onDurationChange={(e) =>
                i === active.current && setDuration(e.currentTarget.duration)
              }
              onLoadedMetadata={(e) =>
                i === active.current && setDuration(e.currentTarget.duration)
              }
              onEnded={() => onEnded(i)}
              onError={() => onError(i)}
            />
          ))}
          {cur && (expanded ? <NowPlaying /> : <MiniPlayer />)}
        </TimeContext.Provider>
      </StateContext.Provider>
    </ActionsContext.Provider>
  );
}
