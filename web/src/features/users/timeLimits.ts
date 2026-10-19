import type { UserRestrictions } from "@/api/types";

/**
 * Reads a time allowance typed as minutes ("90") or hours and minutes ("1:30", "1h30",
 * "2h"). Empty is null (no limit); anything else unreadable or over a day is undefined.
 */
export function parseAllowance(text: string): number | null | undefined {
  const t = text.trim().toLowerCase();
  if (!t) return null;
  let m: RegExpMatchArray | null;
  let minutes: number;
  if ((m = t.match(/^(\d{1,4})\s*(m|min|mins|minutes)?$/))) minutes = Number(m[1]);
  else if ((m = t.match(/^(\d{1,2})\s*(?::|h|hr|hrs|hours?)\s*(\d{1,2})?\s*(m|min|mins)?$/))) {
    const mins = m[2] ? Number(m[2]) : 0;
    if (mins > 59) return undefined;
    minutes = Number(m[1]) * 60 + mins;
  } else return undefined;
  return minutes >= 1 && minutes <= 1440 ? minutes : undefined;
}

/** "1:30" for 90 minutes, "45 min" under an hour. */
export function formatAllowance(minutes: number): string {
  if (minutes < 60) return `${minutes} min`;
  return `${Math.floor(minutes / 60)}:${String(minutes % 60).padStart(2, "0")}`;
}

/** "2 h 15 min" / "45 min" / "2 h", for sentences. */
export function formatMinutes(minutes: number): string {
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  if (!h) return `${m} min`;
  return m ? `${h} h ${m} min` : `${h} h`;
}

/** A short summary of a person's time limits for the users list ("" when there are none). */
export function describeTimeLimits(r: UserRestrictions): string {
  const parts: string[] = [];
  const daily = r.dailyLimitMinutes || 0;
  const weekend = r.weekendLimitMinutes;
  if (daily) parts.push(`${formatMinutes(daily)} a day`);
  if (weekend != null && weekend !== daily && (daily || weekend))
    parts.push(weekend ? `${formatMinutes(weekend)} at weekends` : "no limit at weekends");
  if (r.allowedFrom && r.allowedUntil) parts.push(`${r.allowedFrom}–${r.allowedUntil}`);
  return parts.join(", ");
}
