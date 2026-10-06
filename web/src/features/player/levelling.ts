import type { Entry } from "./queue";

export type Levelling = "off" | "track" | "album" | "auto";

/** ReplayGain values for one track, from its playback session (relative to -18 LUFS). */
export type TrackGains = { trackGain?: number; albumGain?: number; peak?: number };

/** Quiet tracks are raised by at most this much. */
export const MAX_BOOST_DB = 6;
/**
 * How far over full scale a boosted peak may go when the brick-wall limiter follows the
 * gain: it absorbs a few dB of rare peaks without audible pumping. Without the limiter a
 * boost never lets the peak pass full scale.
 */
export const LIMITER_HEADROOM_DB = 3;

const linear = (db: number) => Math.pow(10, db / 20);

/**
 * Auto levelling's choice: album gain while consecutive tracks come from the same album
 * (the track before or after this one shares its album), so an album keeps its own
 * dynamics from its first track on; otherwise track gain.
 */
export function albumRun(entries: Entry[], index: number): boolean {
  const cur = entries[index];
  if (!cur?.item.parentId) return false;
  const same = (e: Entry | undefined) => !!e && e.item.parentId === cur.item.parentId;
  return same(entries[index - 1]) || same(entries[index + 1]);
}

/**
 * Linear gain for a track. Cuts apply in full; boosts (quiet tracks) are capped at
 * +MAX_BOOST_DB and so the peak stays under full scale (plus the limiter's headroom when
 * one follows). Without gain data the track plays as it is.
 */
export function levelGain(
  g: TrackGains | null | undefined,
  mode: Levelling,
  inAlbum: boolean,
  limiter = false,
): number {
  if (!g || mode === "off") return 1;
  const db =
    mode === "album" || (mode === "auto" && inAlbum)
      ? (g.albumGain ?? g.trackGain)
      : g.trackGain;
  if (db === undefined || !Number.isFinite(db)) return 1;
  let gain = linear(Math.min(db, MAX_BOOST_DB));
  if (g.peak && g.peak > 0) {
    // Only ever limits a boost: a cut is never made deeper because of the peak.
    const ceiling = linear(limiter ? LIMITER_HEADROOM_DB : 0) / g.peak;
    gain = Math.min(gain, Math.max(1, ceiling));
  }
  return gain;
}
