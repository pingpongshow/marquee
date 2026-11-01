import type { ItemSummary } from "@/api/types";

export function formatDuration(ms?: number) {
  if (!ms) return "";
  const m = Math.round(ms / 60000);
  if (m < 60) return `${m} min`;
  return `${Math.floor(m / 60)} hr ${m % 60} min`;
}

export function formatTrackTime(ms?: number) {
  if (!ms) return "";
  const s = Math.round(ms / 1000);
  const mmss = `${Math.floor((s % 3600) / 60)}:${String(s % 60).padStart(2, "0")}`;
  return s >= 3600 ? `${Math.floor(s / 3600)}:${mmss.padStart(5, "0")}` : `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

export function formatBytes(n: number) {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n.toFixed(i >= 3 ? 2 : 0)} ${units[i]}`;
}

export function subtitleFor(item: ItemSummary) {
  switch (item.type) {
    case "show":
      return item.childCount === 1 ? "1 season" : `${item.childCount} seasons`;
    case "season":
      return `${item.leafCount} episodes`;
    case "artist":
      return item.childCount === 1 ? "1 album" : `${item.childCount} albums`;
    case "album":
      return item.year ? String(item.year) : `${item.childCount} tracks`;
    case "collection":
      return item.childCount === 1 ? "1 title" : `${item.childCount} titles`;
    default:
      return item.year ? String(item.year) : "";
  }
}

const langNames: Record<string, string> = {
  eng: "English", spa: "Spanish", fre: "French", fra: "French", ger: "German", deu: "German", ita: "Italian", jpn: "Japanese",
  kor: "Korean", chi: "Chinese", zho: "Chinese", por: "Portuguese", rus: "Russian", dut: "Dutch", nld: "Dutch", swe: "Swedish",
  dan: "Danish", nor: "Norwegian", nob: "Norwegian", fin: "Finnish", pol: "Polish", cze: "Czech", ces: "Czech", hun: "Hungarian",
  gre: "Greek", ell: "Greek", tur: "Turkish", ara: "Arabic", heb: "Hebrew", hin: "Hindi", tha: "Thai", vie: "Vietnamese",
  ind: "Indonesian", may: "Malay", msa: "Malay", ukr: "Ukrainian", rum: "Romanian", ron: "Romanian", ice: "Icelandic", isl: "Icelandic",
};
export const languageName = (code?: string) => (code && code !== "und" ? (langNames[code] ?? code.toUpperCase()) : "Unknown");

/** Names a version for pickers: its edition label, else resolution, HDR and codec (LIB-7). */
export function versionLabel(v: { label?: string; files: { height?: number; width?: number; hdrFormat?: string; videoCodec?: string }[] }) {
  const f = v.files[0];
  const res = resolutionLabel(f);
  const hdr = hdrLabel(f?.hdrFormat);
  const codec = codecLabel(f?.videoCodec);
  const tech = [res, hdr, codec].filter(Boolean).join(" · ");
  return v.label ? (tech ? `${v.label} (${tech})` : v.label) : tech || "Version";
}

/** 4K, 1080p, 720p or SD from a file's frame size ("" when unknown). */
export function resolutionLabel(f?: { width?: number; height?: number }) {
  if (!f?.height) return "";
  return (f.width ?? 0) >= 3200 || f.height >= 2000 ? "4K" : f.height >= 1000 || (f.width ?? 0) >= 1800 ? "1080p" : f.height >= 700 || (f.width ?? 0) >= 1200 ? "720p" : "SD";
}

export function hdrLabel(h?: string) {
  if (!h) return "";
  return ({ dolby_vision: "Dolby Vision", hdr10: "HDR10", hdr10plus: "HDR10+", hlg: "HLG" } as Record<string, string>)[h] ?? "HDR";
}

export function codecLabel(c?: string) {
  if (!c) return "";
  return ({ hevc: "HEVC", h264: "H.264", av1: "AV1", vp9: "VP9", mpeg2video: "MPEG-2", aac: "AAC", ac3: "Dolby Digital", eac3: "Dolby Digital Plus", truehd: "Dolby TrueHD", dts: "DTS", flac: "FLAC", opus: "Opus", mp3: "MP3" } as Record<string, string>)[c] ?? c.toUpperCase();
}

/** 2 → "2.0", 6 → "5.1", 8 → "7.1". */
export function channelsLabel(n?: number) {
  if (!n) return "";
  if (n === 1) return "Mono";
  return n >= 6 ? `${n - 1}.1` : `${n}.0`;
}

const audioCodecs: Record<string, string> = {
  truehd: "Dolby TrueHD", eac3: "Dolby Digital Plus", ac3: "Dolby Digital", aac: "AAC", flac: "FLAC", opus: "Opus",
  mp3: "MP3", mp2: "MP2", vorbis: "Vorbis", alac: "ALAC",
};

/** An audio codec's everyday name: "Dolby TrueHD Atmos", "DTS-HD MA", "PCM". */
export function audioCodecName(codec: string, profile?: string) {
  if (codec === "dts") return profile?.includes("MA") ? "DTS-HD MA" : profile?.includes("HD") ? "DTS-HD" : profile?.includes("X") ? "DTS:X" : "DTS";
  if (codec.startsWith("pcm_")) return "PCM";
  const name = audioCodecs[codec] ?? codec.toUpperCase();
  return profile?.includes("Atmos") ? `${name} Atmos` : name;
}

// Track titles worth showing: what the track is, not the release group or its format again.
const usefulTrackTitle = /commentary|descripti|narrat|isolated|score|brazil|latin|castil|european|canad|quebec|mandarin|cantonese/i;

/** An audio track on a title's page: "English · Dolby TrueHD 7.1 · Commentary". */
export function audioTrackLabel(s: { language?: string; codec: string; profile?: string; channels?: number; title?: string }) {
  const codec = [audioCodecName(s.codec, s.profile), s.channels === 2 ? "Stereo" : channelsLabel(s.channels)].filter(Boolean).join(" ");
  const title = s.title?.trim();
  return [languageName(s.language), codec, title && usefulTrackTitle.test(title) ? title : ""].filter(Boolean).join(" · ");
}

/** A title's subtitles by language, each once: "English", "English (SDH)", "French (Forced)". */
export function subtitleLanguages(streams: { kind: string; language?: string; forced: boolean; hearingImpaired: boolean }[]) {
  const out: string[] = [];
  for (const s of streams) {
    if (s.kind !== "subtitle") continue;
    const flags = [s.forced ? "Forced" : "", s.hearingImpaired ? "SDH" : ""].filter(Boolean).join(", ");
    const label = languageName(s.language) + (flags ? ` (${flags})` : "");
    if (!out.includes(label)) out.push(label);
  }
  return out;
}

/** Splits a server path into its file name and folder. */
export function splitPath(path: string) {
  const i = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\"));
  return i < 0 ? { name: path, folder: "" } : { name: path.slice(i + 1), folder: path.slice(0, i) };
}
