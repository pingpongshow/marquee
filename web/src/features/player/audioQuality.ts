import { useSyncExternalStore } from "react";
import type { components } from "@/api/schema.gen";

type AudioFormat = components["schemas"]["AudioFormat"];
type Decision = components["schemas"]["PlaybackDecision"];

/** What the player is actually streaming for a track (MUSIC-23). */
export type StreamInfo = { decision: Decision; limitKbps?: number };

const KEY = "marquee.showAudioQuality";
const listeners = new Set<() => void>();

function read() {
  try {
    return localStorage.getItem(KEY) === "1";
  } catch {
    return false;
  }
}

/** "Show audio quality": stored per browser, off by default. */
export function useShowAudioQuality() {
  return useSyncExternalStore(
    (fn) => {
      listeners.add(fn);
      return () => listeners.delete(fn);
    },
    read,
    () => false,
  );
}

export function setShowAudioQuality(on: boolean) {
  try {
    if (on) localStorage.setItem(KEY, "1");
    else localStorage.removeItem(KEY);
  } catch {
    /* storage unavailable */
  }
  listeners.forEach((fn) => fn());
}

/** ffprobe codec name → a display label. */
export function audioCodecLabel(codec: string) {
  const c = codec.toLowerCase();
  if (c.startsWith("pcm_")) return "PCM";
  if (c.startsWith("dsd_")) return "DSD";
  return (
    ({ flac: "FLAC", alac: "ALAC", mp3: "MP3", aac: "AAC", opus: "Opus", vorbis: "Ogg Vorbis", wavpack: "WavPack", ape: "APE" } as Record<string, string>)[c] ??
    codec.toUpperCase()
  );
}

/** 96000 → "96", 44100 → "44.1". */
const khz = (hz: number) => String(Math.round(hz / 100) / 10);

export const isHiRes = (f: AudioFormat) => f.lossless && ((f.bitDepth ?? 0) >= 24 || (f.sampleRate ?? 0) > 48000);

/** "FLAC · 24-bit/96 kHz", "FLAC · 44.1 kHz", "MP3 · 320 kbps". */
export function audioQualityLabel(f: AudioFormat) {
  const codec = audioCodecLabel(f.codec);
  let detail = "";
  if (f.lossless) {
    if (f.bitDepth && f.sampleRate) detail = `${f.bitDepth}-bit/${khz(f.sampleRate)} kHz`;
    else if (f.sampleRate) detail = `${khz(f.sampleRate)} kHz`;
  } else if (f.bitrateKbps) detail = `${f.bitrateKbps} kbps`;
  return detail ? `${codec} · ${detail}` : codec;
}

/** Compact, for track lists: "FLAC 24/96", "FLAC 44.1", "MP3 320". */
export function audioQualityShort(f: AudioFormat) {
  const codec = audioCodecLabel(f.codec);
  if (f.lossless) {
    if (f.bitDepth && f.sampleRate) return `${codec} ${f.bitDepth}/${khz(f.sampleRate)}`;
    return f.sampleRate ? `${codec} ${khz(f.sampleRate)}` : codec;
  }
  return f.bitrateKbps ? `${codec} ${f.bitrateKbps}` : codec;
}

/** What's streamed instead of the original ("AAC 256 kbps"), or "" when it's the original audio. */
export function streamedLabel(s?: StreamInfo) {
  if (!s || s.decision.method === "direct_play" || s.decision.audioCopy) return "";
  const codec = audioCodecLabel(s.decision.audioCodec ?? "aac");
  // The server says what bitrate it converts at.
  const kbps = s.decision.audioKbps;
  return kbps ? `${codec} ${kbps} kbps` : codec;
}
