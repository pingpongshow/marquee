/**
 * How many days the daily chart covers: the period chosen, or for "all time" from the first day
 * anything played until today (at least 30, at most the last 365). The server sends only days
 * with plays, so their count isn't the span: 60 such days over two years are two years.
 */
export function chartSpan(period: number, dates: readonly string[], today = new Date()): number {
  if (period) return period;
  const first = dates.reduce<string | null>((a, d) => (a === null || d < a ? d : a), null);
  if (!first) return 30;
  const [y = 0, m = 1, d = 1] = first.split("-").map(Number);
  const from = Date.UTC(y, m - 1, d);
  const to = Date.UTC(today.getFullYear(), today.getMonth(), today.getDate());
  const days = Math.round((to - from) / 86_400_000) + 1;
  return Math.min(365, Math.max(30, days));
}
