import type { SubtitleStyle } from "@/api/types";

/** How text subtitles look (PLAY-20); absent fields use these. */
export type ResolvedSubtitleStyle = Required<SubtitleStyle>;

export const defaultSubtitleStyle: ResolvedSubtitleStyle = {
  size: "medium",
  color: "#FFFFFF",
  background: "outline",
  position: "bottom",
};

export function resolveSubtitleStyle(s?: SubtitleStyle | null): ResolvedSubtitleStyle {
  const color = s?.color && /^#[0-9a-f]{6}$/i.test(s.color) ? s.color.toUpperCase() : defaultSubtitleStyle.color;
  return {
    size: s?.size ?? defaultSubtitleStyle.size,
    color,
    background: s?.background ?? defaultSubtitleStyle.background,
    position: s?.position ?? defaultSubtitleStyle.position,
  };
}

/** Text size relative to the browser's own subtitle size (which follows the video's height). */
export const subtitleScale: Record<ResolvedSubtitleStyle["size"], number> = {
  small: 0.75,
  medium: 1,
  large: 1.3,
  huge: 1.65,
};

const outline = "-1px -1px 0 #000, 1px -1px 0 #000, -1px 1px 0 #000, 1px 1px 0 #000, 0 0 4px #000";

/** The CSS for the text (shared by the player's ::cue rule and the settings preview). */
export function subtitleTextStyle(s: ResolvedSubtitleStyle) {
  return {
    color: s.color,
    fontSize: `${subtitleScale[s.size]}em`,
    background: s.background === "translucent" ? "rgba(0, 0, 0, 0.6)" : s.background === "opaque" ? "#000000" : "transparent",
    textShadow: s.background === "outline" ? outline : s.background === "none" ? "2px 2px 3px rgba(0, 0, 0, 0.9)" : "none",
  };
}

/** A ::cue rule for native WebVTT rendering under the given selector. */
export function cueCss(selector: string, s: ResolvedSubtitleStyle) {
  const t = subtitleTextStyle(s);
  return `${selector}::cue{color:${t.color};font-size:${t.fontSize};background:${t.background};text-shadow:${t.textShadow};}`;
}

/** Raised subtitles sit four lines up, above the player's controls and lower-third captions. */
const raisedCues = new WeakSet<VTTCue>();

/**
 * Raised subtitles sit four lines up, above the player's controls and lower-third captions.
 * Only cues placed automatically move; a cue the file positions itself keeps its place.
 */
export function placeCues(track: TextTrack, position: ResolvedSubtitleStyle["position"]) {
  const cues = track.cues;
  if (!cues) return;
  for (let i = 0; i < cues.length; i++) {
    const c = cues[i] as VTTCue;
    if (!("line" in c)) continue;
    if (position === "raised" && c.line === "auto") {
      c.snapToLines = true;
      c.line = -4;
      raisedCues.add(c);
    } else if (position === "bottom" && raisedCues.has(c)) {
      c.line = "auto";
      raisedCues.delete(c);
    }
  }
}
