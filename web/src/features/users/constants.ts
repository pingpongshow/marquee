export const ratingOptions = [
  { value: "", label: "No limit" },
  { value: "TV-Y", label: "TV-Y (all children)" },
  { value: "G", label: "G / TV-G" },
  { value: "PG", label: "PG / TV-PG" },
  { value: "PG-13", label: "PG-13 / TV-14" },
  { value: "R", label: "R / TV-MA" },
] as const;

export const remoteQualityOptions = [
  { value: 0, label: "Server default (automatic)" },
  { value: 12000, label: "1080p · 12 Mbps" },
  { value: 8000, label: "1080p · 8 Mbps" },
  { value: 4000, label: "720p · 4 Mbps" },
  { value: 2000, label: "480p · 2 Mbps" },
  { value: 1000, label: "360p · 1 Mbps" },
];

export const localQualityOptions = [{ value: 0, label: "Original (no transcoding)" }, ...remoteQualityOptions.slice(1)];

export const languageOptions = [
  ["", "Default"],
  ["eng", "English"],
  ["jpn", "Japanese"],
  ["spa", "Spanish"],
  ["fre", "French"],
  ["ger", "German"],
  ["ita", "Italian"],
  ["kor", "Korean"],
  ["chi", "Chinese"],
  ["por", "Portuguese"],
] as const;
