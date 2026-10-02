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
  kor: "Korean", chi: "Chinese", zho: "Chinese", por: "Portuguese", rus: "Russian", dut: "Dutch", swe: "Swedish",
};
export const languageName = (code?: string) => (code ? (langNames[code] ?? code.toUpperCase()) : "Unknown");

/** Names a version for pickers: its edition label, else resolution, HDR and codec (LIB-7). */
export function versionLabel(v: { label?: string; files: { height?: number; width?: number; hdrFormat?: string; videoCodec?: string }[] }) {
  const f = v.files[0];
  const res = !f?.height ? "" : (f.width ?? 0) >= 3200 || f.height >= 2000 ? "4K" : f.height >= 1000 ? "1080p" : f.height >= 700 ? "720p" : "SD";
  const hdr = f?.hdrFormat ? { dolby_vision: "Dolby Vision", hdr10: "HDR10", hdr10plus: "HDR10+", hlg: "HLG" }[f.hdrFormat] ?? "HDR" : "";
  const codec = f?.videoCodec ? ({ hevc: "HEVC", h264: "H.264", av1: "AV1", vp9: "VP9", mpeg2video: "MPEG-2" } as Record<string, string>)[f.videoCodec] ?? f.videoCodec.toUpperCase() : "";
  const tech = [res, hdr, codec].filter(Boolean).join(" · ");
  return v.label ? (tech ? `${v.label} (${tech})` : v.label) : tech || "Version";
}
