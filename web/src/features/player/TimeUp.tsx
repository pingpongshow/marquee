import { queryOptions, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Hourglass } from "lucide-react";
import { useEffect, useRef, useSyncExternalStore } from "react";
import { api, onTimeLimit, unwrap } from "@/api/client";
import { toast } from "@/components/Toast";
import { Button } from "@/components/ui";
import { formatMinutes } from "@/features/users/timeLimits";

/** The signed-in person's time limit today (kids' profiles). */
export const timeLimitQuery = queryOptions({
  queryKey: ["me", "time-limit"],
  queryFn: () => unwrap(api.GET("/me/time-limit")),
  staleTime: 30_000,
});

// The reason shown full screen when time's up (null = not shown).
let shown: string | null = null;
// When it was last closed: the player's last reports on its way out answer time_limit too,
// and mustn't bring it straight back.
let closedAt = 0;
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((l) => l());
function subscribe(fn: () => void) {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}

/** Shows the time's-up screen with the server's reason, and stops any video playing. */
export function showTimeUp(reason: string) {
  if (Date.now() - closedAt < 10_000) return;
  shown = reason || "Time's up for today";
  document.querySelectorAll("video").forEach((v) => v.pause());
  if (document.fullscreenElement) void document.exitFullscreen().catch(() => {});
  emit();
}

/** Checks whether a stream that just ended was ended by a time limit, and if so says so. */
export async function checkTimeUp() {
  const { data } = await api.GET("/me/time-limit").catch(() => ({ data: undefined }));
  if (data?.limited && !data.allowedNow) showTimeUp(data.reason ?? "");
}

const isWatching = () =>
  Array.from(document.querySelectorAll("video")).some((v) => !v.paused && !v.ended);

/** Mounted once in the app: the full-screen time's-up message, and a gentle warning when
 * five minutes of watching are left. */
export function TimeUpHost() {
  const reason = useSyncExternalStore(subscribe, () => shown);
  const qc = useQueryClient();
  const navigate = useNavigate();
  const warned = useRef<string | null>(null);

  useEffect(
    () =>
      onTimeLimit((r) => {
        showTimeUp(r);
        void qc.invalidateQueries({ queryKey: timeLimitQuery.queryKey });
      }),
    [qc],
  );

  // While a video plays, look at the time left once a minute; warn once a day at 5 minutes.
  useEffect(() => {
    const t = window.setInterval(async () => {
      if (!isWatching()) return;
      const st = await qc.fetchQuery({ ...timeLimitQuery, staleTime: 0 }).catch(() => undefined);
      if (!st?.limited || !st.allowedNow || st.minutesLeft == null) return;
      const day = new Date().toDateString();
      if (st.minutesLeft <= 5 && st.minutesLeft > 0 && warned.current !== day) {
        warned.current = day;
        toast(
          st.minutesLeft === 1
            ? "1 minute of watching left today"
            : `${st.minutesLeft} minutes of watching left today`,
          { ms: 8000 },
        );
      }
    }, 60_000);
    return () => window.clearInterval(t);
  }, [qc]);

  if (!reason) return null;
  const close = () => {
    shown = null;
    closedAt = Date.now();
    emit();
    if (location.pathname.startsWith("/play/") || location.pathname.endsWith("/play"))
      void navigate({ to: "/" });
  };
  return (
    <div
      role="alertdialog"
      aria-modal="true"
      aria-labelledby="time-up-title"
      className="fixed inset-0 z-[100] grid place-items-center bg-black/95 p-6 text-center"
    >
      <div className="max-w-md space-y-5">
        <Hourglass className="mx-auto size-14 text-accent" aria-hidden />
        <h2 id="time-up-title" className="text-2xl font-semibold text-white">
          That's all for now
        </h2>
        <p className="text-lg text-white/80">{reason}</p>
        <Button variant="primary" onClick={close} autoFocus>
          OK
        </Button>
      </div>
    </div>
  );
}

/** "45 min left today" for a person with a time limit (nothing for anyone else). */
export function TimeLeftNote({ className }: { className?: string }) {
  const st = useQuery({ ...timeLimitQuery, refetchInterval: 5 * 60_000 }).data;
  if (!st?.limited) return null;
  let text: string;
  if (!st.allowedNow) text = st.reason ?? "No more watching today";
  else if (st.minutesLeft != null) text = `${formatMinutes(st.minutesLeft)} left today`;
  else if (st.allowedFrom && st.allowedUntil) text = `Watching ${st.allowedFrom}–${st.allowedUntil}`;
  else return null;
  return (
    <p className={className}>
      <Hourglass className="mr-1.5 inline size-3.5 align-[-2px]" aria-hidden />
      {text}
    </p>
  );
}
