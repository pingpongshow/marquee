import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { clsx } from "clsx";
import Hls from "hls.js";
import { useWatchTogether } from "./useWatchTogether";
import {
  ArrowLeft,
  Captions,
  Info,
  Maximize,
  Minimize,
  Pause,
  Play,
  RotateCcw,
  RotateCw,
  Settings2,
  SkipForward,
  Volume2,
  VolumeX,
  UsersRound,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import {
  api,
  imageUrl,
  session as authSession,
  trickplaySheetUrl,
  unwrap,
} from "@/api/client";
import { itemQuery, meQuery, systemInfoQuery } from "@/api/queries";
import type { components } from "@/api/schema.gen";
import type {
  ItemDetail,
  ItemSummary,
  MediaStream,
  Trickplay,
} from "@/api/types";
import { Spinner } from "@/components/ui";
import { languageName, versionLabel } from "../browse/format";
import { deviceProfile } from "./deviceProfile";
import { SubtitleSearchDialog } from "./SubtitleSearch";

type PlaybackSession = components["schemas"]["PlaybackSession"];

export const qualityOptions = [
  { kbps: 0, label: "Original" },
  { kbps: 20000, label: "1080p · 20 Mbps" },
  { kbps: 12000, label: "1080p · 12 Mbps" },
  { kbps: 8000, label: "1080p · 8 Mbps" },
  { kbps: 4000, label: "720p · 4 Mbps" },
  { kbps: 2000, label: "480p · 2 Mbps" },
  { kbps: 1000, label: "360p · 1 Mbps" },
];

function storedQuality(remote: boolean) {
  try {
    return Number(
      localStorage.getItem(`marquee.quality.${remote ? "remote" : "local"}`) ??
        0,
    );
  } catch {
    return 0;
  }
}
function storeQuality(remote: boolean, kbps: number) {
  try {
    localStorage.setItem(
      `marquee.quality.${remote ? "remote" : "local"}`,
      String(kbps),
    );
  } catch {
    /* ignore */
  }
}

/** Measures download speed for automatic remote quality; cached for 10 minutes. */
let measured: { kbps: number; at: number } | null = null;
async function measureKbps(): Promise<number> {
  if (measured && Date.now() - measured.at < 600_000) return measured.kbps;
  const t0 = performance.now();
  const res = await fetch("/api/v1/playback/bandwidth-test?kb=2048", {
    headers: { Authorization: `Bearer ${authSession.token}` },
    cache: "no-store",
  });
  const bytes = (await res.arrayBuffer()).byteLength;
  const secs = (performance.now() - t0) / 1000;
  const kbps = Math.round((bytes * 8) / 1000 / Math.max(secs, 0.05));
  measured = { kbps, at: Date.now() };
  return kbps;
}

function fmt(sec: number) {
  if (!isFinite(sec) || sec < 0) sec = 0;
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = Math.floor(sec % 60);
  return h
    ? `${h}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`
    : `${m}:${String(s).padStart(2, "0")}`;
}

function trackLabel(s: MediaStream) {
  return [
    languageName(s.language),
    s.title,
    s.codec.toUpperCase(),
    s.channels ? `${s.channels > 2 ? `${s.channels - 1}.1` : "Stereo"}` : "",
    s.forced ? "Forced" : "",
    s.hearingImpaired ? "SDH" : "",
  ]
    .filter(Boolean)
    .join(" · ");
}

type Selection = {
  audio?: number;
  subtitle?: number;
  quality: number;
  file?: number;
};

/**
 * Fallback levels when the browser can't play what it claimed it could:
 * 0 = everything the browser reports, 1 = no direct play, 2 = H.264 video only (full transcode).
 */
function profileFor(level: number) {
  const base = deviceProfile();
  if (level === 0) return base;
  const noDirect = { ...base, containers: [] };
  if (level === 1) return noDirect;
  return {
    ...noDirect,
    videoCodecs: ["h264"],
    hlsVideoCodecs: ["h264"],
    tenBit: false,
    hdr: [],
  };
}

export function VideoPlayer({
  itemId,
  startMs,
  playlistId,
  fileId,
  groupId,
}: {
  itemId: number;
  startMs?: number;
  playlistId?: number;
  fileId?: number;
  groupId?: string;
}) {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const item = useQuery(itemQuery(itemId));
  const me = useQuery(meQuery);
  const trickplay = useQuery({
    queryKey: ["trickplay", itemId],
    queryFn: () =>
      unwrap(
        api.GET("/items/{itemId}/trickplay", { params: { path: { itemId } } }),
      ),
    retry: false,
    staleTime: Infinity,
  });
  const info = useQuery(systemInfoQuery);
  const remote = info.data?.networkClass === "remote";
  const videoRef = useRef<HTMLVideoElement>(null);
  const hlsRef = useRef<Hls | null>(null);
  const sessionRef = useRef<PlaybackSession | null>(null);
  const together = useWatchTogether(videoRef, itemId, me.data?.id, groupId);
  const [sess, setSess] = useState<PlaybackSession | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [sel, setSel] = useState<Selection>(() => ({
    quality: storedQuality(false),
    file: fileId,
  }));
  const [restartAt, setRestartAt] = useState<number | undefined>(startMs);
  const [playing, setPlaying] = useState(false);
  const [time, setTime] = useState(0);
  const [buffering, setBuffering] = useState(true);
  const [muted, setMuted] = useState(false);
  const [volume, setVolume] = useState(1);
  const [chrome, setChrome] = useState(true);
  const [menu, setMenu] = useState<
    "settings" | "subs" | "info" | "together" | null
  >(null);
  const [fullscreen, setFullscreen] = useState(false);
  const [next, setNext] = useState<ItemSummary | null>(null);
  const [nextDismissed, setNextDismissed] = useState(false);
  const hideTimer = useRef<number | undefined>(undefined);
  const [fallback, setFallback] = useState(0);
  const startedRef = useRef(false);
  const failRef = useRef<(m: string) => void>(() => {});
  const [bufferedEnd, setBufferedEnd] = useState(0);
  const menuRef = useRef(menu);
  useEffect(() => {
    menuRef.current = menu;
  }, [menu]);
  // Show the controls, then hide them after 3 s of no mouse/keyboard activity while playing.
  const poke = useCallback(() => {
    setChrome(true);
    window.clearTimeout(hideTimer.current);
    hideTimer.current = window.setTimeout(
      () =>
        !menuRef.current &&
        videoRef.current &&
        !videoRef.current.paused &&
        setChrome(false),
      3000,
    );
  }, []);
  const updateBuffered = (el: HTMLVideoElement) => {
    let end = 0;
    for (let i = 0; i < el.buffered.length; i++)
      if (el.buffered.start(i) <= el.currentTime + 1)
        end = Math.max(end, el.buffered.end(i));
    setBufferedEnd(end);
  };

  const file =
    item.data?.versions[0]?.files.find((f) => f.id === sess?.fileId) ??
    item.data?.versions[0]?.files[0];
  const audioTracks = file?.streams.filter((s) => s.kind === "audio") ?? [];
  const subTracks = file?.streams.filter((s) => s.kind === "subtitle") ?? [];
  const duration = (sess?.durationMs ?? item.data?.durationMs ?? 0) / 1000;

  const report = useCallback((state: "playing" | "paused" | "buffering") => {
    const s = sessionRef.current;
    const v = videoRef.current;
    if (!s || !v) return;
    api
      .PATCH("/playback/sessions/{sessionId}", {
        params: { path: { sessionId: s.id } },
        body: { positionMs: Math.round(v.currentTime * 1000), state },
      })
      .catch(() => {});
  }, []);

  const stopSession = useCallback(() => {
    const s = sessionRef.current;
    if (!s) return;
    sessionRef.current = null;
    // keepalive lets the request finish while the page unloads
    fetch(`/api/v1/playback/sessions/${s.id}`, {
      method: "DELETE",
      keepalive: true,
      headers: { Authorization: `Bearer ${authSession.token}` },
    }).catch(() => {});
  }, []);

  // Start (or restart, after a track/quality change) the session.
  useEffect(() => {
    if (!item.data || !info.data) return;
    let cancelled = false;
    (async () => {
      setError(null);
      setBuffering(true);
      try {
        const quality = sel.quality || storedQuality(remote);
        const measuredKbps =
          remote && !quality
            ? await measureKbps().catch(() => undefined)
            : undefined;
        const s = await unwrap(
          api.POST("/playback/sessions", {
            body: {
              itemId,
              fileId: sel.file,
              profile: profileFor(fallback),
              startMs: restartAt,
              audioStreamId: sel.audio,
              subtitleStreamId: sel.subtitle,
              maxBitrateKbps: quality || undefined,
              measuredKbps,
            },
          }),
        );
        if (cancelled) {
          fetch(`/api/v1/playback/sessions/${s.id}`, {
            method: "DELETE",
            keepalive: true,
            headers: { Authorization: `Bearer ${authSession.token}` },
          });
          return;
        }
        stopSession();
        sessionRef.current = s;
        setSess(s);
      } catch (e) {
        if (!cancelled) setError((e as Error).message);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [
    item.data,
    info.data,
    itemId,
    restartAt,
    sel,
    remote,
    stopSession,
    fallback,
  ]);

  // When playback fails, report it and retry one step safer (see profileFor).
  const fail = useCallback(
    (message: string) => {
      const s = sessionRef.current;
      const v = videoRef.current;
      if (s)
        api
          .PATCH("/playback/sessions/{sessionId}", {
            params: { path: { sessionId: s.id } },
            body: {
              positionMs: Math.round((v?.currentTime ?? 0) * 1000),
              state: "error",
              error: `${s.decision.method}: ${message}`,
            },
          })
          .catch(() => {});
      if (fallback < 2) {
        setRestartAt(
          v && v.currentTime > 1 ? Math.round(v.currentTime * 1000) : restartAt,
        );
        setFallback((f) => f + 1);
      } else {
        setError(`This video can't be played in this browser (${message}).`);
      }
    },
    [fallback, restartAt],
  );
  useEffect(() => {
    failRef.current = fail;
  }, [fail]);

  // A stream that hasn't started after 20 s counts as failed.
  useEffect(() => {
    if (!sess) return;
    startedRef.current = false;
    const t = window.setTimeout(
      () =>
        !startedRef.current &&
        failRef.current("didn't start within 20 seconds"),
      20_000,
    );
    return () => window.clearTimeout(t);
  }, [sess]);

  // Styled ASS subtitles: render with JASSUB, loading the fonts embedded in the file.
  useEffect(() => {
    const v = videoRef.current;
    if (!v || !sess || sess.subtitleFormat !== "ass" || !sess.subtitleUrl)
      return;
    let instance: { destroy: () => void } | null = null;
    let cancelled = false;
    (async () => {
      try {
        const [{ default: JASSUB }, fonts] = await Promise.all([
          import("jassub"),
          sess.fontsUrl
            ? fetch(sess.fontsUrl).then(
                (r) => r.json() as Promise<{ name: string; url: string }[]>,
              )
            : Promise.resolve([]),
        ]);
        if (cancelled) return;
        instance = new JASSUB({
          video: v,
          subUrl: new URL(sess.subtitleUrl!, location.href).href,
          fonts: fonts.map((f) => new URL(f.url, location.href).href),
          queryFonts: false, // local font access needs a secure context
        });
      } catch (e) {
        console.warn("styled subtitles unavailable", e);
      }
    })();
    return () => {
      cancelled = true;
      instance?.destroy();
    };
  }, [sess]);

  // Attach the stream to the <video>.
  useEffect(() => {
    const v = videoRef.current;
    if (!v || !sess) return;
    const start = sess.startMs / 1000;
    hlsRef.current?.destroy();
    hlsRef.current = null;
    if (sess.protocol === "hls" && Hls.isSupported()) {
      const hls = new Hls({
        startPosition: start,
        maxBufferLength: 30,
        backBufferLength: 60,
      });
      let mediaRecovered = false;
      hls.on(Hls.Events.ERROR, (_e, data) => {
        if (data.fatal) {
          if (data.type === Hls.ErrorTypes.MEDIA_ERROR && !mediaRecovered) {
            mediaRecovered = true;
            hls.recoverMediaError();
          } else failRef.current(`${data.type}: ${data.details}`);
        }
      });
      hls.loadSource(sess.url);
      hls.attachMedia(v);
      hlsRef.current = hls;
    } else {
      v.src = sess.url;
      const seek = () => {
        if (start > 0) v.currentTime = start;
        v.removeEventListener("loadedmetadata", seek);
      };
      v.addEventListener("loadedmetadata", seek);
    }
    v.play().catch(() => setPlaying(false));
    return () => {
      hlsRef.current?.destroy();
      hlsRef.current = null;
    };
  }, [sess]);

  // Periodic progress; stop the session when leaving.
  useEffect(() => {
    const t = window.setInterval(
      () => videoRef.current && !videoRef.current.paused && report("playing"),
      10_000,
    );
    const onUnload = () => stopSession();
    window.addEventListener("pagehide", onUnload);
    return () => {
      window.clearInterval(t);
      window.removeEventListener("pagehide", onUnload);
      report("paused");
      stopSession();
      qc.invalidateQueries({ queryKey: ["items"] });
    };
  }, [report, stopSession, qc]);

  // Up Next: the following playlist entry when playing a playlist, else the next episode.
  useEffect(() => {
    if (!playlistId) return;
    unwrap(
      api.GET("/playlists/{playlistId}/items", {
        params: { path: { playlistId }, query: { limit: 2000 } },
      }),
    )
      .then((p) => {
        const i = p.items.findIndex((e) => e.item.id === itemId);
        setNext(i >= 0 ? (p.items[i + 1]?.item ?? null) : null);
      })
      .catch(() => setNext(null));
  }, [playlistId, itemId]);
  useEffect(() => {
    if (playlistId || item.data?.type !== "episode") return;
    unwrap(api.GET("/items/{itemId}/next", { params: { path: { itemId } } }))
      .then((n) => setNext((n as ItemSummary) ?? null))
      .catch(() => setNext(null));
  }, [item.data?.type, itemId, playlistId]);

  const [finding, setFinding] = useState(false);
  const restart = (patch: Partial<Selection>) => {
    const v = videoRef.current;
    setRestartAt(v ? Math.round(v.currentTime * 1000) : 0);
    setSel((s) => ({ ...s, ...patch }));
    setMenu(null);
  };

  const toggle = useCallback(() => {
    const el = videoRef.current;
    if (!el) return;
    if (el.paused) el.play();
    else el.pause();
  }, []);
  const seekBy = (d: number) => {
    const el = videoRef.current;
    if (el)
      el.currentTime = Math.max(0, Math.min(el.currentTime + d, duration - 1));
  };
  const goFullscreen = () => {
    if (document.fullscreenElement) document.exitFullscreen();
    else document.getElementById("player-root")?.requestFullscreen();
  };
  const playNext = useCallback(() => {
    if (next)
      navigate({
        to: "/play/$itemId",
        params: { itemId: String(next.id) },
        search: { t: 0, pl: playlistId },
        replace: true,
      });
  }, [next, navigate, playlistId]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.target as HTMLElement)?.tagName === "INPUT") return;
      const el = videoRef.current;
      if (!el) return;
      switch (e.key) {
        case " ":
        case "k":
          e.preventDefault();
          toggle();
          break;
        case "ArrowLeft":
          el.currentTime = Math.max(0, el.currentTime - 10);
          break;
        case "ArrowRight":
          el.currentTime += 10;
          break;
        case "f":
          goFullscreen();
          break;
        case "m":
          el.muted = !el.muted;
          break;
        case "Escape":
          if (!document.fullscreenElement)
            navigate({
              to: "/item/$itemId",
              params: { itemId: String(itemId) },
            });
      }
      poke();
    };
    window.addEventListener("keydown", onKey);
    const onFs = () => setFullscreen(!!document.fullscreenElement);
    document.addEventListener("fullscreenchange", onFs);
    return () => {
      window.removeEventListener("keydown", onKey);
      document.removeEventListener("fullscreenchange", onFs);
    };
  });

  const d = item.data;
  const marker = sess?.markers.find(
    (m) => time * 1000 >= m.startMs && time * 1000 < m.endMs - 1000,
  );
  const nearEnd =
    duration > 0 &&
    (duration - time < 30 || (marker?.kind === "credits" && !!next));
  const showUpNext = !!next && nearEnd && !nextDismissed;

  return (
    <div
      id="player-root"
      className={clsx(
        "fixed inset-0 z-50 bg-black select-none",
        !chrome && "cursor-none",
      )}
      onMouseMove={poke}
      onClick={(e) => e.target === e.currentTarget && setMenu(null)}
    >
      <video
        ref={videoRef}
        className="size-full"
        playsInline
        onClick={() => (menu ? setMenu(null) : toggle())}
        onDoubleClick={goFullscreen}
        onPlay={() => {
          setPlaying(true);
          report("playing");
          poke();
        }}
        onPause={() => {
          setPlaying(false);
          setChrome(true);
          report("paused");
        }}
        onSeeked={() => report(videoRef.current?.paused ? "paused" : "playing")}
        onTimeUpdate={(e) => {
          setTime(e.currentTarget.currentTime);
          updateBuffered(e.currentTarget);
        }}
        onProgress={(e) => updateBuffered(e.currentTarget)}
        onWaiting={() => setBuffering(true)}
        onPlaying={() => {
          startedRef.current = true;
          setBuffering(false);
        }}
        onError={(e) => {
          const err = e.currentTarget.error;
          // hls.js reports its own errors; this covers direct play and native HLS.
          if (err && sess && !hlsRef.current)
            fail(
              `media error ${err.code}${err.message ? `: ${err.message}` : ""}`,
            );
        }}
        onCanPlay={() => setBuffering(false)}
        onVolumeChange={(e) => {
          setMuted(e.currentTarget.muted);
          setVolume(e.currentTarget.volume);
        }}
        onEnded={() => {
          report("paused");
          if (next && !nextDismissed) playNext();
        }}
      >
        {sess?.subtitleUrl && sess.subtitleFormat !== "ass" && (
          <track
            key={sess.subtitleUrl}
            kind="subtitles"
            src={sess.subtitleUrl}
            default
          />
        )}
      </video>

      {(buffering || !sess) && !error && (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
          <Spinner label={sess ? "Buffering" : "Starting"} />
        </div>
      )}
      {error && (
        <div className="absolute inset-0 flex flex-col items-center justify-center gap-4 text-center">
          <p className="max-w-md text-white">{error}</p>
          <button
            className="rounded-md bg-accent px-4 py-2 font-medium text-black"
            onClick={() => restart({})}
          >
            Try again
          </button>
        </div>
      )}

      {/* Top bar */}
      <div
        className={clsx(
          "absolute inset-x-0 top-0 flex items-center gap-4 bg-gradient-to-b from-black/80 to-transparent p-4 transition-opacity",
          chrome ? "opacity-100" : "opacity-0",
        )}
      >
        <button
          onClick={() =>
            navigate({
              to: "/item/$itemId",
              params: { itemId: String(itemId) },
            })
          }
          className="rounded-full p-2 text-white hover:bg-white/10"
          aria-label="Back"
        >
          <ArrowLeft className="size-6" />
        </button>
        <div className="min-w-0 text-white">
          <div className="truncate text-lg font-semibold">
            {d?.type === "episode" ? d.grandparentTitle : d?.title}
          </div>
          {d?.type === "episode" && (
            <div className="truncate text-sm text-white/70">
              {d.parentTitle} · Episode {d.index} · {d.title}
            </div>
          )}
        </div>
      </div>

      {/* Skip intro / credits */}
      {marker && !showUpNext && (
        <button
          onClick={() =>
            videoRef.current &&
            (videoRef.current.currentTime = marker.endMs / 1000)
          }
          className="absolute right-8 bottom-28 rounded-md border border-white/40 bg-black/60 px-5 py-2.5 font-semibold text-white backdrop-blur hover:bg-white hover:text-black"
        >
          Skip {marker.kind === "intro" ? "Intro" : "Credits"}
        </button>
      )}

      {/* Up Next */}
      {showUpNext && next && (
        <UpNext
          next={next}
          onPlay={playNext}
          onDismiss={() => setNextDismissed(true)}
          secondsLeft={Math.max(0, Math.ceil(duration - time))}
        />
      )}
      {finding && (
        <SubtitleSearchDialog
          itemId={itemId}
          preferred={me.data?.preferences?.subtitleLanguage}
          onClose={() => setFinding(false)}
          onAdded={(id) => {
            setFinding(false);
            restart({ subtitle: id });
          }}
        />
      )}

      {/* Bottom controls */}
      <div
        className={clsx(
          "absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/90 to-transparent px-6 pt-16 pb-5 transition-opacity",
          chrome ? "opacity-100" : "pointer-events-none opacity-0",
        )}
      >
        <SeekBar
          time={time}
          duration={duration}
          markers={sess?.markers ?? []}
          bufferedEnd={bufferedEnd}
          preview={trickplay.data ? { ...trickplay.data, itemId } : undefined}
          onSeek={(t) => videoRef.current && (videoRef.current.currentTime = t)}
        />
        <div className="mt-3 flex items-center gap-3 text-white">
          <button
            onClick={toggle}
            className="rounded-full p-2 hover:bg-white/10"
            aria-label={playing ? "Pause" : "Play"}
          >
            {playing ? (
              <Pause className="size-7 fill-current" />
            ) : (
              <Play className="size-7 fill-current" />
            )}
          </button>
          <button
            onClick={() => seekBy(-10)}
            className="rounded-full p-2 hover:bg-white/10"
            aria-label="Back 10 seconds"
          >
            <RotateCcw className="size-5" />
          </button>
          <button
            onClick={() => seekBy(30)}
            className="rounded-full p-2 hover:bg-white/10"
            aria-label="Forward 30 seconds"
          >
            <RotateCw className="size-5" />
          </button>
          {next && (
            <button
              onClick={playNext}
              className="rounded-full p-2 hover:bg-white/10"
              aria-label="Next episode"
            >
              <SkipForward className="size-5" />
            </button>
          )}
          <span className="ml-2 text-sm tabular-nums">
            {fmt(time)} / {fmt(duration)}
          </span>
          <div className="ml-auto flex items-center gap-1">
            <button
              onClick={() =>
                videoRef.current && (videoRef.current.muted = !muted)
              }
              className="rounded-full p-2 hover:bg-white/10"
              aria-label={muted ? "Unmute" : "Mute"}
            >
              {muted || volume === 0 ? (
                <VolumeX className="size-5" />
              ) : (
                <Volume2 className="size-5" />
              )}
            </button>
            <input
              type="range"
              min={0}
              max={1}
              step={0.05}
              value={muted ? 0 : volume}
              aria-label="Volume"
              className="w-24 accent-[var(--color-accent)]"
              onChange={(e) => {
                const el = videoRef.current;
                if (el) {
                  el.volume = Number(e.target.value);
                  el.muted = el.volume === 0;
                }
              }}
            />
            <button
              onClick={() => setMenu(menu === "subs" ? null : "subs")}
              className="rounded-full p-2 hover:bg-white/10"
              aria-label="Audio and subtitles"
            >
              <Captions className="size-5" />
            </button>
            <button
              onClick={() => setMenu(menu === "settings" ? null : "settings")}
              className="rounded-full p-2 hover:bg-white/10"
              aria-label="Quality"
            >
              <Settings2 className="size-5" />
            </button>
            <button
              onClick={() =>
                together.group
                  ? setMenu(menu === "together" ? null : "together")
                  : void together.start().then(() => setMenu("together"))
              }
              className={clsx(
                "relative rounded-full p-2 hover:bg-white/10",
                together.group && "text-accent",
              )}
              aria-label={
                together.group ? "Watching together" : "Watch together"
              }
            >
              <UsersRound className="size-5" />
              {together.group && (
                <span className="absolute -top-0.5 -right-0.5 rounded-full bg-accent px-1 text-[10px] font-bold text-black">
                  {together.group.members.length}
                </span>
              )}
            </button>
            <button
              onClick={() => setMenu(menu === "info" ? null : "info")}
              className="rounded-full p-2 hover:bg-white/10"
              aria-label="Playback info"
            >
              <Info className="size-5" />
            </button>
            <button
              onClick={goFullscreen}
              className="rounded-full p-2 hover:bg-white/10"
              aria-label="Full screen"
            >
              {fullscreen ? (
                <Minimize className="size-5" />
              ) : (
                <Maximize className="size-5" />
              )}
            </button>
          </div>
        </div>
      </div>

      {menu && (
        <div className="absolute right-6 bottom-24 max-h-[60vh] w-80 overflow-y-auto rounded-lg border border-white/15 bg-black/85 p-2 text-sm text-white shadow-2xl backdrop-blur">
          {menu === "together" && (
            <div className="space-y-2 p-2">
              <MenuHeading>Watching together</MenuHeading>
              {together.group ? (
                <>
                  <p className="text-white/70">
                    Play, pause and seeking are shared. Others join from Home,
                    where they'll see you're watching.
                  </p>
                  <ul className="space-y-1">
                    {together.group.members.map((m) => (
                      <li key={m.userId} className="flex items-center gap-2">
                        <span className="size-2 rounded-full bg-green-500" />
                        {m.name}
                        {m.buffering && (
                          <span className="text-xs text-white/50">
                            loading…
                          </span>
                        )}
                      </li>
                    ))}
                  </ul>
                  {together.group.lastBy && together.group.lastAction && (
                    <p className="text-xs text-white/50">
                      {together.group.lastBy}: {together.group.lastAction}
                    </p>
                  )}
                  <button
                    className="w-full rounded-md bg-white/10 px-3 py-2 text-left hover:bg-white/20"
                    onClick={() => {
                      together.leave();
                      setMenu(null);
                    }}
                  >
                    Leave the group
                  </button>
                </>
              ) : (
                <p className="text-white/70">{together.error ?? "Starting…"}</p>
              )}
            </div>
          )}
          {menu === "subs" && (
            <>
              <MenuHeading>Audio</MenuHeading>
              {audioTracks.map((s) => (
                <MenuItem
                  key={s.id}
                  active={s.id === sess?.audioStreamId}
                  onClick={() => restart({ audio: s.id })}
                >
                  {trackLabel(s)}
                </MenuItem>
              ))}
              <MenuHeading>Subtitles</MenuHeading>
              <MenuItem
                active={!sess?.subtitleStreamId}
                onClick={() => restart({ subtitle: -1 })}
              >
                Off
              </MenuItem>
              {subTracks.map((s) => (
                <MenuItem
                  key={s.id}
                  active={s.id === sess?.subtitleStreamId}
                  onClick={() => restart({ subtitle: s.id })}
                >
                  {trackLabel(s)}
                  {s.external ? " · External" : ""}
                </MenuItem>
              ))}
              {(item.data?.type === "movie" ||
                item.data?.type === "episode") && (
                <MenuItem
                  onClick={() => {
                    setMenu(null);
                    setFinding(true);
                  }}
                >
                  Find subtitles…
                </MenuItem>
              )}
            </>
          )}
          {menu === "settings" && (
            <>
              {(item.data?.versions.length ?? 0) > 1 && (
                <>
                  <MenuHeading>Version</MenuHeading>
                  {item.data!.versions.map((v) => {
                    const id = v.files[0]?.id;
                    return (
                      <MenuItem
                        key={v.id}
                        active={id === sess?.fileId}
                        onClick={() =>
                          id &&
                          restart({
                            file: id,
                            audio: undefined,
                            subtitle: undefined,
                          })
                        }
                      >
                        {versionLabel(v)}
                      </MenuItem>
                    );
                  })}
                </>
              )}
              <MenuHeading>
                Quality {remote ? "(away from home)" : "(home network)"}
              </MenuHeading>
              {remote && (
                <MenuItem
                  active={sel.quality === 0}
                  onClick={() => (
                    storeQuality(true, 0),
                    restart({ quality: 0 })
                  )}
                >
                  Automatic
                </MenuItem>
              )}
              {qualityOptions
                .filter((q) => !(remote && q.kbps === 0))
                .map((q) => (
                  <MenuItem
                    key={q.kbps}
                    active={sel.quality === q.kbps}
                    onClick={() => (
                      storeQuality(remote, q.kbps),
                      restart({ quality: q.kbps })
                    )}
                  >
                    {q.label}
                  </MenuItem>
                ))}
            </>
          )}
          {menu === "info" && sess && (
            <div className="space-y-2 p-2">
              <div className="font-semibold">{sess.decision.summary}</div>
              {sess.decision.reasons.length > 0 && (
                <ul className="list-disc space-y-0.5 pl-4 text-white/70">
                  {sess.decision.reasons.map((r) => (
                    <li key={r}>{r}</li>
                  ))}
                </ul>
              )}
              <div className="text-white/70">
                {sess.networkClass === "remote"
                  ? "Away from home"
                  : "Home network"}
                {sess.limitKbps
                  ? ` · limited to ${(sess.limitKbps / 1000).toFixed(1)} Mbps by ${sess.limitReason}`
                  : ""}
              </div>
              {file && (
                <div className="text-white/50">
                  Source: {file.container?.toUpperCase()} ·{" "}
                  {file.videoCodec?.toUpperCase()} {file.height}p ·{" "}
                  {file.audioCodec?.toUpperCase()} ·{" "}
                  {((file.bitrateKbps ?? 0) / 1000).toFixed(1)} Mbps
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function MenuHeading({ children }: { children: React.ReactNode }) {
  return (
    <div className="px-3 pt-2 pb-1 text-[11px] font-semibold tracking-wider text-white/50 uppercase">
      {children}
    </div>
  );
}

function MenuItem({
  active,
  onClick,
  children,
}: {
  active?: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      className={clsx(
        "flex w-full items-center rounded px-3 py-2 text-left hover:bg-white/10",
        active && "text-accent",
      )}
    >
      <span
        className={clsx(
          "mr-2 size-1.5 shrink-0 rounded-full",
          active ? "bg-accent" : "bg-transparent",
        )}
      />
      <span className="min-w-0 flex-1 truncate">{children}</span>
    </button>
  );
}

function SeekBar({
  time,
  duration,
  markers,
  bufferedEnd: bufEnd,
  preview,
  onSeek,
}: {
  time: number;
  duration: number;
  markers: PlaybackSession["markers"];
  bufferedEnd: number;
  preview?: Trickplay & { itemId: number };
  onSeek: (t: number) => void;
}) {
  const [hover, setHover] = useState<number | null>(null);
  const bar = useRef<HTMLDivElement>(null);
  const posFor = (clientX: number) => {
    const r = bar.current!.getBoundingClientRect();
    return Math.max(0, Math.min(1, (clientX - r.left) / r.width)) * duration;
  };
  const pct = (t: number) => `${duration ? (t / duration) * 100 : 0}%`;
  return (
    <div
      ref={bar}
      className="group relative h-5 cursor-pointer"
      onMouseMove={(e) => setHover(posFor(e.clientX))}
      onMouseLeave={() => setHover(null)}
      onClick={(e) => onSeek(posFor(e.clientX))}
      role="slider"
      aria-label="Seek"
      aria-valuemin={0}
      aria-valuemax={Math.round(duration)}
      aria-valuenow={Math.round(time)}
    >
      <div className="absolute inset-x-0 top-1/2 h-1 -translate-y-1/2 rounded-full bg-white/20 transition-all group-hover:h-1.5">
        <div
          className="absolute inset-y-0 left-0 rounded-full bg-white/30"
          style={{ width: pct(bufEnd) }}
        />
        {markers.map((m) => (
          <div
            key={m.startMs}
            className="absolute inset-y-0 bg-white/40"
            style={{
              left: pct(m.startMs / 1000),
              width: pct((m.endMs - m.startMs) / 1000),
            }}
          />
        ))}
        <div
          className="absolute inset-y-0 left-0 rounded-full bg-accent"
          style={{ width: pct(time) }}
        />
      </div>
      <div
        className="absolute top-1/2 size-3.5 -translate-x-1/2 -translate-y-1/2 rounded-full bg-accent opacity-0 group-hover:opacity-100"
        style={{ left: pct(time) }}
      />
      {hover !== null && (
        <div
          className="pointer-events-none absolute bottom-6 flex -translate-x-1/2 flex-col items-center gap-1"
          // Keep the preview on screen near the ends of the bar.
          style={{
            left: `clamp(${PREVIEW_W / 2}px, ${pct(hover)}, calc(100% - ${PREVIEW_W / 2}px))`,
          }}
        >
          {preview && <PreviewThumb t={hover} p={preview} />}
          <div className="rounded bg-black/80 px-2 py-0.5 text-xs text-white tabular-nums">
            {fmt(hover)}
          </div>
        </div>
      )}
    </div>
  );
}

const PREVIEW_W = 224;

/** One trickplay tile, cut from its sprite sheet with background positioning. */
function PreviewThumb({
  t,
  p,
}: {
  t: number;
  p: Trickplay & { itemId: number };
}) {
  const n = Math.min(
    p.count - 1,
    Math.max(0, Math.floor((t * 1000) / p.intervalMs)),
  );
  const per = p.columns * p.rows;
  const sheet = Math.floor(n / per);
  const tile = n % per;
  const scale = PREVIEW_W / p.width;
  const h = Math.round(p.height * scale);
  return (
    <div
      className="overflow-hidden rounded border border-white/30 bg-black shadow-xl"
      style={{
        width: PREVIEW_W,
        height: h,
        backgroundImage: `url(${trickplaySheetUrl(p.itemId, sheet)})`,
        backgroundSize: `${p.columns * PREVIEW_W}px ${p.rows * h}px`,
        backgroundPosition: `-${(tile % p.columns) * PREVIEW_W}px -${Math.floor(tile / p.columns) * h}px`,
      }}
    />
  );
}

function UpNext({
  next,
  onPlay,
  onDismiss,
  secondsLeft,
}: {
  next: ItemSummary;
  onPlay: () => void;
  onDismiss: () => void;
  secondsLeft: number;
}) {
  const art = next.images?.thumb ?? next.images?.backdrop;
  return (
    <div className="absolute right-8 bottom-28 w-80 overflow-hidden rounded-lg border border-white/15 bg-black/85 text-white shadow-2xl backdrop-blur">
      {art && (
        <img
          src={imageUrl(art, 320)}
          alt=""
          className="aspect-video w-full object-cover"
        />
      )}
      <div className="space-y-2 p-3">
        <div className="text-xs text-white/60 uppercase">
          Up next{secondsLeft <= 30 ? ` in ${secondsLeft}s` : ""}
        </div>
        <div className="truncate font-semibold">
          {next.type === "episode"
            ? `${next.grandparentTitle ?? next.parentTitle} · ${next.parentTitle} · E${next.index} · ${next.title}`
            : next.title}
        </div>
        <div className="flex gap-2">
          <button
            onClick={onPlay}
            className="flex-1 rounded bg-accent px-3 py-1.5 font-semibold text-black"
          >
            Play now
          </button>
          <button
            onClick={onDismiss}
            className="rounded bg-white/10 px-3 py-1.5"
          >
            Hide
          </button>
        </div>
      </div>
    </div>
  );
}

export type { ItemDetail };
