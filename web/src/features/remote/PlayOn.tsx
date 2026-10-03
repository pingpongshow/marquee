import { useMutation, useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { clsx } from "clsx";
import { Globe, Loader2, Monitor, MonitorSpeaker, Smartphone, Tablet, Tv } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import type { RemotePlayer } from "@/api/types";
import { Alert, Button, Dialog, Spinner } from "@/components/ui";

export const remotePlayersQuery = {
  queryKey: ["remote", "players"],
  queryFn: () => unwrap(api.GET("/remote/players")),
};

export function PlatformIcon({ platform, className }: { platform: string; className?: string }) {
  const Icon = platform === "web" ? Globe : platform === "ios" || platform === "android" ? Smartphone : platform === "ipados" ? Tablet : platform === "tvos" || platform === "androidtv" ? Tv : Monitor;
  return <Icon className={className} aria-hidden />;
}

export function playerActivity(p: RemotePlayer) {
  const s = p.state;
  if (!s || s.state === "idle" || s.state === "stopped" || !s.title) return "Idle";
  return `${s.state === "paused" ? "Paused" : "Playing"}: ${s.title}${s.subtitle ? ` · ${s.subtitle}` : ""}`;
}

/** Marquee players the person can control (USER-14), as a list to pick from. */
export function PlayerList({ onPick, busyId }: { onPick: (p: RemotePlayer) => void; busyId?: number }) {
  const players = useQuery({ ...remotePlayersQuery, refetchInterval: 4000 });
  if (players.isPending) return <Spinner label="Looking for players" />;
  if (players.isError) return <Alert tone="error">{players.error.message}</Alert>;
  if (!players.data.length)
    return <p className="text-sm text-muted">No other Marquee apps are open. Open Marquee on your TV, phone, tablet or another browser and it appears here.</p>;
  return (
    <ul className="space-y-1" aria-label="Players">
      {players.data.map((p) => (
        <li key={p.deviceId}>
          <button
            onClick={() => onPick(p)}
            disabled={busyId !== undefined}
            className="flex w-full items-center gap-3 rounded-md p-2 text-left hover:bg-surface-2 disabled:opacity-60"
          >
            <PlatformIcon platform={p.platform} className="size-6 shrink-0 text-muted" />
            <span className="min-w-0 flex-1">
              <span className="block truncate font-medium">{p.name}</span>
              <span className="block truncate text-xs text-muted">
                {p.userName} · {playerActivity(p)}
              </span>
            </span>
            {busyId === p.deviceId && <Loader2 className="size-4 animate-spin text-muted" aria-label="Sending" />}
          </button>
        </li>
      ))}
    </ul>
  );
}

/**
 * "Play on…" (USER-14): send something to another Marquee app. When handing off what's
 * playing here, the position goes along and this one pauses. Marquee players only; Chromecast is separate.
 */
export function PlayOnButton({
  itemIds,
  index,
  startMs,
  shuffle,
  onHandoff,
  className,
  iconClassName = "size-5",
}: {
  itemIds: number[];
  index?: number;
  /** The current position when handing off something already playing here. */
  startMs?: () => number;
  shuffle?: boolean;
  onHandoff?: () => void;
  className?: string;
  iconClassName?: string;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button type="button" onClick={() => setOpen(true)} className={className} aria-label="Play on…" title="Play on another device">
        <MonitorSpeaker className={iconClassName} />
      </button>
      {open && <PlayOnDialog itemIds={itemIds} index={index} startMs={startMs} shuffle={shuffle} onHandoff={onHandoff} onClose={() => setOpen(false)} />}
    </>
  );
}

function PlayOnDialog({
  itemIds,
  index,
  startMs,
  shuffle,
  onHandoff,
  onClose,
}: {
  itemIds: number[];
  index?: number;
  startMs?: () => number;
  shuffle?: boolean;
  onHandoff?: () => void;
  onClose: () => void;
}) {
  const navigate = useNavigate();
  const send = useMutation({
    mutationFn: (p: RemotePlayer) =>
      unwrap(
        api.POST("/remote/players/{deviceId}/commands", {
          params: { path: { deviceId: p.deviceId } },
          body: { type: "play", itemIds: itemIds.slice(0, 1000), index, startMs: startMs?.(), shuffle },
        }),
      ),
    onSuccess: (_d, p) => {
      onHandoff?.();
      onClose();
      navigate({ to: "/remote", search: { d: p.deviceId } });
    },
  });
  return (
    <Dialog open onClose={onClose} title="Play on…">
      <div className="space-y-4">
        {send.isError && <Alert tone="error">{send.error.message}</Alert>}
        <PlayerList onPick={(p) => send.mutate(p)} busyId={send.isPending ? send.variables?.deviceId : undefined} />
        <div className="flex justify-between">
          <Link to="/remote" search={{ d: undefined }} onClick={onClose} className={clsx("text-sm text-muted hover:text-text hover:underline")}>
            Open Remote
          </Link>
          <Button variant="ghost" size="sm" onClick={onClose}>
            Cancel
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
