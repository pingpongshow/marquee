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
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
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
    default:
      return item.year ? String(item.year) : "";
  }
}

const langNames: Record<string, string> = {
  eng: "English", spa: "Spanish", fre: "French", fra: "French", ger: "German", deu: "German", ita: "Italian", jpn: "Japanese",
  kor: "Korean", chi: "Chinese", zho: "Chinese", por: "Portuguese", rus: "Russian", dut: "Dutch", swe: "Swedish",
};
export const languageName = (code?: string) => (code ? (langNames[code] ?? code.toUpperCase()) : "Unknown");
