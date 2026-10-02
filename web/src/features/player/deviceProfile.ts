import type { components } from "@/api/schema.gen";

type DeviceProfile = components["schemas"]["DeviceProfile"];

/** What this browser can play, from MediaSource/canPlayType probes. Computed once. */
let cached: DeviceProfile | null = null;

export function deviceProfile(): DeviceProfile {
  if (cached) return cached;
  const v = document.createElement("video");
  const a = document.createElement("audio");
  const MSE =
    (window as unknown as { ManagedMediaSource?: typeof MediaSource })
      .ManagedMediaSource ?? window.MediaSource;
  const mse = (type: string) => !!MSE && MSE.isTypeSupported(type);
  const can = (el: HTMLMediaElement, type: string) =>
    el.canPlayType(type) !== "";
  const nativeHLS = can(v, "application/vnd.apple.mpegurl");

  const videoCodecs: string[] = [];
  const hlsVideo: string[] = [];
  const checks: [string, string][] = [
    ["h264", 'video/mp4; codecs="avc1.640029"'],
    ["hevc", 'video/mp4; codecs="hvc1.1.6.L150.90"'],
    ["av1", 'video/mp4; codecs="av01.0.08M.08"'],
    ["vp9", 'video/webm; codecs="vp9"'],
  ];
  for (const [codec, type] of checks) {
    if (can(v, type) || mse(type)) videoCodecs.push(codec);
    if (codec !== "vp9" && (mse(type) || (nativeHLS && can(v, type))))
      hlsVideo.push(codec);
  }
  const audioCodecs: string[] = [];
  const hlsAudio: string[] = [];
  const audioChecks: [string, string][] = [
    ["aac", 'audio/mp4; codecs="mp4a.40.2"'],
    ["mp3", "audio/mpeg"],
    ["opus", 'audio/webm; codecs="opus"'],
    ["flac", "audio/flac"],
    ["ac3", 'audio/mp4; codecs="ac-3"'],
    ["eac3", 'audio/mp4; codecs="ec-3"'],
    ["alac", 'audio/mp4; codecs="alac"'],
    ["vorbis", 'audio/ogg; codecs="vorbis"'],
  ];
  for (const [codec, type] of audioChecks) {
    if (can(a, type) || mse(type)) audioCodecs.push(codec);
    if (
      codec !== "vorbis" &&
      codec !== "mp3" &&
      (mse(type) || (nativeHLS && can(a, type)))
    )
      hlsAudio.push(codec);
  }
  const containers = ["mp4"];
  if (can(v, "video/webm")) containers.push("webm");
  for (const c of ["mp3", "flac", "ogg"])
    if (can(a, c === "mp3" ? "audio/mpeg" : `audio/${c}`)) containers.push(c);
  if (can(a, "audio/mp4")) containers.push("m4a");

  const hdr: ("hdr10" | "hlg")[] = window.matchMedia?.("(dynamic-range: high)")
    .matches
    ? ["hdr10", "hlg"]
    : [];
  cached = {
    containers,
    videoCodecs,
    audioCodecs,
    maxAudioChannels: 2,
    tenBit: mse('video/mp4; codecs="hvc1.2.4.L150.90"'),
    hdr,
    hls: nativeHLS || !!MSE,
    hlsVideoCodecs: hlsVideo,
    hlsAudioCodecs: hlsAudio,
    textSubtitles: true,
    // Styled anime subtitles are drawn by JASSUB (libass in WebAssembly).
    assSubtitles:
      typeof WebAssembly === "object" &&
      typeof Worker === "function" &&
      typeof OffscreenCanvas === "function",
  };
  return cached;
}
