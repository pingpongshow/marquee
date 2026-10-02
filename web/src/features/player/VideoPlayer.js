import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { clsx } from "clsx";
import Hls from "hls.js";
import { ArrowLeft, Captions, Info, Maximize, Minimize, Pause, Play, RotateCcw, RotateCw, Settings2, SkipForward, Volume2, VolumeX } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { api, imageUrl, session as authSession, unwrap } from "@/api/client";
import { itemQuery, systemInfoQuery } from "@/api/queries";
import { Spinner } from "@/components/ui";
import { languageName } from "../browse/format";
import { deviceProfile } from "./deviceProfile";
export const qualityOptions = [
    { kbps: 0, label: "Original" },
    { kbps: 20000, label: "1080p · 20 Mbps" },
    { kbps: 12000, label: "1080p · 12 Mbps" },
    { kbps: 8000, label: "1080p · 8 Mbps" },
    { kbps: 4000, label: "720p · 4 Mbps" },
    { kbps: 2000, label: "480p · 2 Mbps" },
    { kbps: 1000, label: "360p · 1 Mbps" },
];
function storedQuality(remote) {
    try {
        return Number(localStorage.getItem(`marquee.quality.${remote ? "remote" : "local"}`) ?? 0);
    }
    catch {
        return 0;
    }
}
function storeQuality(remote, kbps) {
    try {
        localStorage.setItem(`marquee.quality.${remote ? "remote" : "local"}`, String(kbps));
    }
    catch {
        /* ignore */
    }
}
/** Measures download speed for automatic remote quality; cached for 10 minutes. */
let measured = null;
async function measureKbps() {
    if (measured && Date.now() - measured.at < 600_000)
        return measured.kbps;
    const t0 = performance.now();
    const res = await fetch("/api/v1/playback/bandwidth-test?kb=2048", { headers: { Authorization: `Bearer ${authSession.token}` }, cache: "no-store" });
    const bytes = (await res.arrayBuffer()).byteLength;
    const secs = (performance.now() - t0) / 1000;
    const kbps = Math.round((bytes * 8) / 1000 / Math.max(secs, 0.05));
    measured = { kbps, at: Date.now() };
    return kbps;
}
function fmt(sec) {
    if (!isFinite(sec) || sec < 0)
        sec = 0;
    const h = Math.floor(sec / 3600);
    const m = Math.floor((sec % 3600) / 60);
    const s = Math.floor(sec % 60);
    return h ? `${h}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}` : `${m}:${String(s).padStart(2, "0")}`;
}
function trackLabel(s) {
    return [languageName(s.language), s.title, s.codec.toUpperCase(), s.channels ? `${s.channels > 2 ? `${s.channels - 1}.1` : "Stereo"}` : "", s.forced ? "Forced" : "", s.hearingImpaired ? "SDH" : ""]
        .filter(Boolean)
        .join(" · ");
}
/**
 * Fallback levels when the browser can't play what it claimed it could:
 * 0 = everything the browser reports, 1 = no direct play, 2 = H.264 video only (full transcode).
 */
function profileFor(level) {
    const base = deviceProfile();
    if (level === 0)
        return base;
    const noDirect = { ...base, containers: [] };
    if (level === 1)
        return noDirect;
    return { ...noDirect, videoCodecs: ["h264"], hlsVideoCodecs: ["h264"], tenBit: false, hdr: [] };
}
export function VideoPlayer({ itemId, startMs }) {
    const navigate = useNavigate();
    const qc = useQueryClient();
    const item = useQuery(itemQuery(itemId));
    const info = useQuery(systemInfoQuery);
    const remote = info.data?.networkClass === "remote";
    const videoRef = useRef(null);
    const hlsRef = useRef(null);
    const sessionRef = useRef(null);
    const [sess, setSess] = useState(null);
    const [error, setError] = useState(null);
    const [sel, setSel] = useState(() => ({ quality: storedQuality(false) }));
    const [restartAt, setRestartAt] = useState(startMs);
    const [playing, setPlaying] = useState(false);
    const [time, setTime] = useState(0);
    const [buffering, setBuffering] = useState(true);
    const [muted, setMuted] = useState(false);
    const [volume, setVolume] = useState(1);
    const [chrome, setChrome] = useState(true);
    const [menu, setMenu] = useState(null);
    const [fullscreen, setFullscreen] = useState(false);
    const [next, setNext] = useState(null);
    const [nextDismissed, setNextDismissed] = useState(false);
    const hideTimer = useRef(undefined);
    const [fallback, setFallback] = useState(0);
    const startedRef = useRef(false);
    const failRef = useRef(() => { });
    const [bufferedEnd, setBufferedEnd] = useState(0);
    const menuRef = useRef(menu);
    useEffect(() => {
        menuRef.current = menu;
    }, [menu]);
    // Show the controls, then hide them after 3 s of no mouse/keyboard activity while playing.
    const poke = useCallback(() => {
        setChrome(true);
        window.clearTimeout(hideTimer.current);
        hideTimer.current = window.setTimeout(() => !menuRef.current && videoRef.current && !videoRef.current.paused && setChrome(false), 3000);
    }, []);
    const updateBuffered = (el) => {
        let end = 0;
        for (let i = 0; i < el.buffered.length; i++)
            if (el.buffered.start(i) <= el.currentTime + 1)
                end = Math.max(end, el.buffered.end(i));
        setBufferedEnd(end);
    };
    const file = item.data?.versions[0]?.files.find((f) => f.id === sess?.fileId) ?? item.data?.versions[0]?.files[0];
    const audioTracks = file?.streams.filter((s) => s.kind === "audio") ?? [];
    const subTracks = file?.streams.filter((s) => s.kind === "subtitle") ?? [];
    const duration = (sess?.durationMs ?? item.data?.durationMs ?? 0) / 1000;
    const report = useCallback((state) => {
        const s = sessionRef.current;
        const v = videoRef.current;
        if (!s || !v)
            return;
        api.PATCH("/playback/sessions/{sessionId}", { params: { path: { sessionId: s.id } }, body: { positionMs: Math.round(v.currentTime * 1000), state } }).catch(() => { });
    }, []);
    const stopSession = useCallback(() => {
        const s = sessionRef.current;
        if (!s)
            return;
        sessionRef.current = null;
        // keepalive lets the request finish while the page unloads
        fetch(`/api/v1/playback/sessions/${s.id}`, { method: "DELETE", keepalive: true, headers: { Authorization: `Bearer ${authSession.token}` } }).catch(() => { });
    }, []);
    // Start (or restart, after a track/quality change) the session.
    useEffect(() => {
        if (!item.data || !info.data)
            return;
        let cancelled = false;
        (async () => {
            setError(null);
            setBuffering(true);
            try {
                const quality = sel.quality || storedQuality(remote);
                const measuredKbps = remote && !quality ? await measureKbps().catch(() => undefined) : undefined;
                const s = await unwrap(api.POST("/playback/sessions", {
                    body: {
                        itemId,
                        profile: profileFor(fallback),
                        startMs: restartAt,
                        audioStreamId: sel.audio,
                        subtitleStreamId: sel.subtitle,
                        maxBitrateKbps: quality || undefined,
                        measuredKbps,
                    },
                }));
                if (cancelled) {
                    fetch(`/api/v1/playback/sessions/${s.id}`, { method: "DELETE", keepalive: true, headers: { Authorization: `Bearer ${authSession.token}` } });
                    return;
                }
                stopSession();
                sessionRef.current = s;
                setSess(s);
            }
            catch (e) {
                if (!cancelled)
                    setError(e.message);
            }
        })();
        return () => {
            cancelled = true;
        };
    }, [item.data, info.data, itemId, restartAt, sel, remote, stopSession, fallback]);
    // When playback fails, report it and retry one step safer (see profileFor).
    const fail = useCallback((message) => {
        const s = sessionRef.current;
        const v = videoRef.current;
        if (s)
            api.PATCH("/playback/sessions/{sessionId}", {
                params: { path: { sessionId: s.id } },
                body: { positionMs: Math.round((v?.currentTime ?? 0) * 1000), state: "error", error: `${s.decision.method}: ${message}` },
            }).catch(() => { });
        if (fallback < 2) {
            setRestartAt(v && v.currentTime > 1 ? Math.round(v.currentTime * 1000) : restartAt);
            setFallback((f) => f + 1);
        }
        else {
            setError(`This video can't be played in this browser (${message}).`);
        }
    }, [fallback, restartAt]);
    useEffect(() => {
        failRef.current = fail;
    }, [fail]);
    // A stream that hasn't started after 20 s counts as failed.
    useEffect(() => {
        if (!sess)
            return;
        startedRef.current = false;
        const t = window.setTimeout(() => !startedRef.current && failRef.current("didn't start within 20 seconds"), 20_000);
        return () => window.clearTimeout(t);
    }, [sess]);
    // Styled ASS subtitles: render with JASSUB, loading the fonts embedded in the file.
    useEffect(() => {
        const v = videoRef.current;
        if (!v || !sess || sess.subtitleFormat !== "ass" || !sess.subtitleUrl)
            return;
        let instance = null;
        let cancelled = false;
        (async () => {
            try {
                const [{ default: JASSUB }, fonts] = await Promise.all([
                    import("jassub"),
                    sess.fontsUrl ? fetch(sess.fontsUrl).then((r) => r.json()) : Promise.resolve([]),
                ]);
                if (cancelled)
                    return;
                instance = new JASSUB({
                    video: v,
                    subUrl: new URL(sess.subtitleUrl, location.href).href,
                    fonts: fonts.map((f) => new URL(f.url, location.href).href),
                    queryFonts: false, // local font access needs a secure context
                });
            }
            catch (e) {
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
        if (!v || !sess)
            return;
        const start = sess.startMs / 1000;
        hlsRef.current?.destroy();
        hlsRef.current = null;
        if (sess.protocol === "hls" && Hls.isSupported()) {
            const hls = new Hls({ startPosition: start, maxBufferLength: 30, backBufferLength: 60 });
            let mediaRecovered = false;
            hls.on(Hls.Events.ERROR, (_e, data) => {
                if (data.fatal) {
                    if (data.type === Hls.ErrorTypes.MEDIA_ERROR && !mediaRecovered) {
                        mediaRecovered = true;
                        hls.recoverMediaError();
                    }
                    else
                        failRef.current(`${data.type}: ${data.details}`);
                }
            });
            hls.loadSource(sess.url);
            hls.attachMedia(v);
            hlsRef.current = hls;
        }
        else {
            v.src = sess.url;
            const seek = () => {
                if (start > 0)
                    v.currentTime = start;
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
        const t = window.setInterval(() => videoRef.current && !videoRef.current.paused && report("playing"), 10_000);
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
    // Up Next for episodes.
    useEffect(() => {
        if (item.data?.type !== "episode")
            return;
        unwrap(api.GET("/items/{itemId}/next", { params: { path: { itemId } } }))
            .then((n) => setNext(n ?? null))
            .catch(() => setNext(null));
    }, [item.data?.type, itemId]);
    const restart = (patch) => {
        const v = videoRef.current;
        setRestartAt(v ? Math.round(v.currentTime * 1000) : 0);
        setSel((s) => ({ ...s, ...patch }));
        setMenu(null);
    };
    const toggle = useCallback(() => {
        const el = videoRef.current;
        if (!el)
            return;
        if (el.paused)
            el.play();
        else
            el.pause();
    }, []);
    const seekBy = (d) => {
        const el = videoRef.current;
        if (el)
            el.currentTime = Math.max(0, Math.min(el.currentTime + d, duration - 1));
    };
    const goFullscreen = () => {
        if (document.fullscreenElement)
            document.exitFullscreen();
        else
            document.getElementById("player-root")?.requestFullscreen();
    };
    const playNext = useCallback(() => {
        if (next)
            navigate({ to: "/play/$itemId", params: { itemId: String(next.id) }, search: { t: 0 }, replace: true });
    }, [next, navigate]);
    useEffect(() => {
        const onKey = (e) => {
            if (e.target?.tagName === "INPUT")
                return;
            const el = videoRef.current;
            if (!el)
                return;
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
                        navigate({ to: "/item/$itemId", params: { itemId: String(itemId) } });
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
    const marker = sess?.markers.find((m) => time * 1000 >= m.startMs && time * 1000 < m.endMs - 1000);
    const nearEnd = duration > 0 && (duration - time < 30 || (marker?.kind === "credits" && !!next));
    const showUpNext = !!next && nearEnd && !nextDismissed;
    return (_jsxs("div", { id: "player-root", className: clsx("fixed inset-0 z-50 bg-black select-none", !chrome && "cursor-none"), onMouseMove: poke, onClick: (e) => e.target === e.currentTarget && setMenu(null), children: [_jsx("video", { ref: videoRef, className: "size-full", playsInline: true, onClick: () => (menu ? setMenu(null) : toggle()), onDoubleClick: goFullscreen, onPlay: () => {
                    setPlaying(true);
                    report("playing");
                    poke();
                }, onPause: () => {
                    setPlaying(false);
                    setChrome(true);
                    report("paused");
                }, onSeeked: () => report(videoRef.current?.paused ? "paused" : "playing"), onTimeUpdate: (e) => {
                    setTime(e.currentTarget.currentTime);
                    updateBuffered(e.currentTarget);
                }, onProgress: (e) => updateBuffered(e.currentTarget), onWaiting: () => setBuffering(true), onPlaying: () => {
                    startedRef.current = true;
                    setBuffering(false);
                }, onError: (e) => {
                    const err = e.currentTarget.error;
                    // hls.js reports its own errors; this covers direct play and native HLS.
                    if (err && sess && !hlsRef.current)
                        fail(`media error ${err.code}${err.message ? `: ${err.message}` : ""}`);
                }, onCanPlay: () => setBuffering(false), onVolumeChange: (e) => {
                    setMuted(e.currentTarget.muted);
                    setVolume(e.currentTarget.volume);
                }, onEnded: () => {
                    report("paused");
                    if (next && !nextDismissed)
                        playNext();
                }, children: sess?.subtitleUrl && sess.subtitleFormat !== "ass" && _jsx("track", { kind: "subtitles", src: sess.subtitleUrl, default: true }, sess.subtitleUrl) }), (buffering || !sess) && !error && (_jsx("div", { className: "pointer-events-none absolute inset-0 flex items-center justify-center", children: _jsx(Spinner, { label: sess ? "Buffering" : "Starting" }) })), error && (_jsxs("div", { className: "absolute inset-0 flex flex-col items-center justify-center gap-4 text-center", children: [_jsx("p", { className: "max-w-md text-white", children: error }), _jsx("button", { className: "rounded-md bg-accent px-4 py-2 font-medium text-black", onClick: () => restart({}), children: "Try again" })] })), _jsxs("div", { className: clsx("absolute inset-x-0 top-0 flex items-center gap-4 bg-gradient-to-b from-black/80 to-transparent p-4 transition-opacity", chrome ? "opacity-100" : "opacity-0"), children: [_jsx("button", { onClick: () => navigate({ to: "/item/$itemId", params: { itemId: String(itemId) } }), className: "rounded-full p-2 text-white hover:bg-white/10", "aria-label": "Back", children: _jsx(ArrowLeft, { className: "size-6" }) }), _jsxs("div", { className: "min-w-0 text-white", children: [_jsx("div", { className: "truncate text-lg font-semibold", children: d?.type === "episode" ? d.grandparentTitle : d?.title }), d?.type === "episode" && (_jsxs("div", { className: "truncate text-sm text-white/70", children: [d.parentTitle, " \u00B7 Episode ", d.index, " \u00B7 ", d.title] }))] })] }), marker && !showUpNext && (_jsxs("button", { onClick: () => videoRef.current && (videoRef.current.currentTime = marker.endMs / 1000), className: "absolute right-8 bottom-28 rounded-md border border-white/40 bg-black/60 px-5 py-2.5 font-semibold text-white backdrop-blur hover:bg-white hover:text-black", children: ["Skip ", marker.kind === "intro" ? "Intro" : "Credits"] })), showUpNext && next && _jsx(UpNext, { next: next, onPlay: playNext, onDismiss: () => setNextDismissed(true), secondsLeft: Math.max(0, Math.ceil(duration - time)) }), _jsxs("div", { className: clsx("absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/90 to-transparent px-6 pt-16 pb-5 transition-opacity", chrome ? "opacity-100" : "pointer-events-none opacity-0"), children: [_jsx(SeekBar, { time: time, duration: duration, markers: sess?.markers ?? [], bufferedEnd: bufferedEnd, onSeek: (t) => videoRef.current && (videoRef.current.currentTime = t) }), _jsxs("div", { className: "mt-3 flex items-center gap-3 text-white", children: [_jsx("button", { onClick: toggle, className: "rounded-full p-2 hover:bg-white/10", "aria-label": playing ? "Pause" : "Play", children: playing ? _jsx(Pause, { className: "size-7 fill-current" }) : _jsx(Play, { className: "size-7 fill-current" }) }), _jsx("button", { onClick: () => seekBy(-10), className: "rounded-full p-2 hover:bg-white/10", "aria-label": "Back 10 seconds", children: _jsx(RotateCcw, { className: "size-5" }) }), _jsx("button", { onClick: () => seekBy(30), className: "rounded-full p-2 hover:bg-white/10", "aria-label": "Forward 30 seconds", children: _jsx(RotateCw, { className: "size-5" }) }), next && (_jsx("button", { onClick: playNext, className: "rounded-full p-2 hover:bg-white/10", "aria-label": "Next episode", children: _jsx(SkipForward, { className: "size-5" }) })), _jsxs("span", { className: "ml-2 text-sm tabular-nums", children: [fmt(time), " / ", fmt(duration)] }), _jsxs("div", { className: "ml-auto flex items-center gap-1", children: [_jsx("button", { onClick: () => videoRef.current && (videoRef.current.muted = !muted), className: "rounded-full p-2 hover:bg-white/10", "aria-label": muted ? "Unmute" : "Mute", children: muted || volume === 0 ? _jsx(VolumeX, { className: "size-5" }) : _jsx(Volume2, { className: "size-5" }) }), _jsx("input", { type: "range", min: 0, max: 1, step: 0.05, value: muted ? 0 : volume, "aria-label": "Volume", className: "w-24 accent-[var(--color-accent)]", onChange: (e) => {
                                            const el = videoRef.current;
                                            if (el) {
                                                el.volume = Number(e.target.value);
                                                el.muted = el.volume === 0;
                                            }
                                        } }), _jsx("button", { onClick: () => setMenu(menu === "subs" ? null : "subs"), className: "rounded-full p-2 hover:bg-white/10", "aria-label": "Audio and subtitles", children: _jsx(Captions, { className: "size-5" }) }), _jsx("button", { onClick: () => setMenu(menu === "settings" ? null : "settings"), className: "rounded-full p-2 hover:bg-white/10", "aria-label": "Quality", children: _jsx(Settings2, { className: "size-5" }) }), _jsx("button", { onClick: () => setMenu(menu === "info" ? null : "info"), className: "rounded-full p-2 hover:bg-white/10", "aria-label": "Playback info", children: _jsx(Info, { className: "size-5" }) }), _jsx("button", { onClick: goFullscreen, className: "rounded-full p-2 hover:bg-white/10", "aria-label": "Full screen", children: fullscreen ? _jsx(Minimize, { className: "size-5" }) : _jsx(Maximize, { className: "size-5" }) })] })] })] }), menu && (_jsxs("div", { className: "absolute right-6 bottom-24 max-h-[60vh] w-80 overflow-y-auto rounded-lg border border-white/15 bg-black/85 p-2 text-sm text-white shadow-2xl backdrop-blur", children: [menu === "subs" && (_jsxs(_Fragment, { children: [_jsx(MenuHeading, { children: "Audio" }), audioTracks.map((s) => (_jsx(MenuItem, { active: s.id === sess?.audioStreamId, onClick: () => restart({ audio: s.id }), children: trackLabel(s) }, s.id))), _jsx(MenuHeading, { children: "Subtitles" }), _jsx(MenuItem, { active: !sess?.subtitleStreamId, onClick: () => restart({ subtitle: -1 }), children: "Off" }), subTracks.map((s) => (_jsxs(MenuItem, { active: s.id === sess?.subtitleStreamId, onClick: () => restart({ subtitle: s.id }), children: [trackLabel(s), s.external ? " · External" : ""] }, s.id)))] })), menu === "settings" && (_jsxs(_Fragment, { children: [_jsxs(MenuHeading, { children: ["Quality ", remote ? "(away from home)" : "(home network)"] }), remote && (_jsx(MenuItem, { active: sel.quality === 0, onClick: () => (storeQuality(true, 0), restart({ quality: 0 })), children: "Automatic" })), qualityOptions
                                .filter((q) => !(remote && q.kbps === 0))
                                .map((q) => (_jsx(MenuItem, { active: sel.quality === q.kbps, onClick: () => (storeQuality(remote, q.kbps), restart({ quality: q.kbps })), children: q.label }, q.kbps)))] })), menu === "info" && sess && (_jsxs("div", { className: "space-y-2 p-2", children: [_jsx("div", { className: "font-semibold", children: sess.decision.summary }), sess.decision.reasons.length > 0 && (_jsx("ul", { className: "list-disc space-y-0.5 pl-4 text-white/70", children: sess.decision.reasons.map((r) => (_jsx("li", { children: r }, r))) })), _jsxs("div", { className: "text-white/70", children: [sess.networkClass === "remote" ? "Away from home" : "Home network", sess.limitKbps ? ` · limited to ${(sess.limitKbps / 1000).toFixed(1)} Mbps by ${sess.limitReason}` : ""] }), file && (_jsxs("div", { className: "text-white/50", children: ["Source: ", file.container?.toUpperCase(), " \u00B7 ", file.videoCodec?.toUpperCase(), " ", file.height, "p \u00B7 ", file.audioCodec?.toUpperCase(), " \u00B7 ", ((file.bitrateKbps ?? 0) / 1000).toFixed(1), " Mbps"] }))] }))] }))] }));
}
function MenuHeading({ children }) {
    return _jsx("div", { className: "px-3 pt-2 pb-1 text-[11px] font-semibold tracking-wider text-white/50 uppercase", children: children });
}
function MenuItem({ active, onClick, children }) {
    return (_jsxs("button", { onClick: onClick, className: clsx("flex w-full items-center rounded px-3 py-2 text-left hover:bg-white/10", active && "text-accent"), children: [_jsx("span", { className: clsx("mr-2 size-1.5 shrink-0 rounded-full", active ? "bg-accent" : "bg-transparent") }), _jsx("span", { className: "min-w-0 flex-1 truncate", children: children })] }));
}
function SeekBar({ time, duration, markers, bufferedEnd: bufEnd, onSeek, }) {
    const [hover, setHover] = useState(null);
    const bar = useRef(null);
    const posFor = (clientX) => {
        const r = bar.current.getBoundingClientRect();
        return Math.max(0, Math.min(1, (clientX - r.left) / r.width)) * duration;
    };
    const pct = (t) => `${duration ? (t / duration) * 100 : 0}%`;
    return (_jsxs("div", { ref: bar, className: "group relative h-5 cursor-pointer", onMouseMove: (e) => setHover(posFor(e.clientX)), onMouseLeave: () => setHover(null), onClick: (e) => onSeek(posFor(e.clientX)), role: "slider", "aria-label": "Seek", "aria-valuemin": 0, "aria-valuemax": Math.round(duration), "aria-valuenow": Math.round(time), children: [_jsxs("div", { className: "absolute inset-x-0 top-1/2 h-1 -translate-y-1/2 rounded-full bg-white/20 transition-all group-hover:h-1.5", children: [_jsx("div", { className: "absolute inset-y-0 left-0 rounded-full bg-white/30", style: { width: pct(bufEnd) } }), markers.map((m) => (_jsx("div", { className: "absolute inset-y-0 bg-white/40", style: { left: pct(m.startMs / 1000), width: pct((m.endMs - m.startMs) / 1000) } }, m.startMs))), _jsx("div", { className: "absolute inset-y-0 left-0 rounded-full bg-accent", style: { width: pct(time) } })] }), _jsx("div", { className: "absolute top-1/2 size-3.5 -translate-x-1/2 -translate-y-1/2 rounded-full bg-accent opacity-0 group-hover:opacity-100", style: { left: pct(time) } }), hover !== null && (_jsx("div", { className: "absolute bottom-6 -translate-x-1/2 rounded bg-black/80 px-2 py-0.5 text-xs text-white tabular-nums", style: { left: pct(hover) }, children: fmt(hover) }))] }));
}
function UpNext({ next, onPlay, onDismiss, secondsLeft }) {
    const art = next.images?.thumb ?? next.images?.backdrop;
    return (_jsxs("div", { className: "absolute right-8 bottom-28 w-80 overflow-hidden rounded-lg border border-white/15 bg-black/85 text-white shadow-2xl backdrop-blur", children: [art && _jsx("img", { src: imageUrl(art, 320), alt: "", className: "aspect-video w-full object-cover" }), _jsxs("div", { className: "space-y-2 p-3", children: [_jsxs("div", { className: "text-xs text-white/60 uppercase", children: ["Up next", secondsLeft <= 30 ? ` in ${secondsLeft}s` : ""] }), _jsxs("div", { className: "truncate font-semibold", children: [next.parentTitle, " \u00B7 E", next.index, " \u00B7 ", next.title] }), _jsxs("div", { className: "flex gap-2", children: [_jsx("button", { onClick: onPlay, className: "flex-1 rounded bg-accent px-3 py-1.5 font-semibold text-black", children: "Play now" }), _jsx("button", { onClick: onDismiss, className: "rounded bg-white/10 px-3 py-1.5", children: "Hide" })] })] })] }));
}
