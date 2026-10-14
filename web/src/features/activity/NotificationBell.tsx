import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { clsx } from "clsx";
import { Bell, Check, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { api, unwrap } from "@/api/client";
import { timeAgo } from "@/lib/time";

const notificationsKey = ["notifications"];

/** Admin alerts (Settings → Notifications) under a bell in the top bar. */
export function NotificationBell() {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const list = useQuery({
    queryKey: notificationsKey,
    queryFn: () => unwrap(api.GET("/notifications")),
    refetchInterval: 60_000,
    refetchIntervalInBackground: false,
  });
  const refresh = () => qc.invalidateQueries({ queryKey: notificationsKey });
  const readOne = useMutation({
    mutationFn: (id: number) => unwrap(api.POST("/notifications/{notificationId}/read", { params: { path: { notificationId: id } } })),
    onSettled: refresh,
  });
  const readAll = useMutation({ mutationFn: () => unwrap(api.POST("/notifications/read-all")), onSettled: refresh });
  const dismiss = useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE("/notifications/{notificationId}", { params: { path: { notificationId: id } } })),
    onSettled: refresh,
  });

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false);
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const items = list.data?.items ?? [];
  const unread = list.data?.unread ?? 0;
  return (
    <div className="relative" ref={ref}>
      <button
        onClick={() => setOpen((v) => !v)}
        className={clsx("relative rounded p-1.5 hover:bg-surface-2", unread ? "text-text" : "text-muted hover:text-text")}
        aria-label={unread ? `Notifications: ${unread} unread` : "Notifications"}
        aria-expanded={open}
        title="Notifications"
      >
        <Bell className="size-5" />
        {unread > 0 && (
          <span className="absolute -top-0.5 -right-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-accent px-1 text-[10px] font-bold text-black">
            {unread > 99 ? "99+" : unread}
          </span>
        )}
      </button>
      {open && (
        <div className="absolute right-0 z-40 mt-2 w-[22rem] max-w-[calc(100vw-2rem)] overflow-hidden rounded-lg border border-border bg-surface shadow-2xl">
          <div className="flex items-center border-b border-border px-4 py-2.5">
            <span className="text-sm font-semibold">Notifications</span>
            {unread > 0 && (
              <button className="ml-auto text-xs text-muted hover:text-text" onClick={() => readAll.mutate()}>
                Mark all read
              </button>
            )}
          </div>
          {items.length === 0 && <p className="px-4 py-6 text-center text-sm text-muted">No alerts.</p>}
          <ul className="max-h-96 divide-y divide-border overflow-y-auto">
            {items.map((n) => (
              <li key={n.id} className={clsx("flex items-start gap-2 px-4 py-3", !n.read && "bg-accent/5")}>
                {!n.read && <span className="mt-1.5 size-2 shrink-0 rounded-full bg-accent" aria-label="Unread" />}
                <div className="min-w-0 flex-1">
                  <div className="text-sm font-medium">{n.title}</div>
                  {n.message && <div className="mt-0.5 text-xs break-words text-muted">{n.message}</div>}
                  <div className="mt-1 text-[11px] text-faint">{timeAgo(n.createdAt)}</div>
                </div>
                <div className="flex shrink-0 gap-0.5">
                  {!n.read && (
                    <button className="rounded p-1 text-muted hover:bg-surface-2 hover:text-text" aria-label={`Mark read: ${n.title}`} title="Mark read" onClick={() => readOne.mutate(n.id)}>
                      <Check className="size-3.5" />
                    </button>
                  )}
                  <button className="rounded p-1 text-muted hover:bg-surface-2 hover:text-text" aria-label={`Dismiss: ${n.title}`} title="Dismiss" onClick={() => dismiss.mutate(n.id)}>
                    <X className="size-3.5" />
                  </button>
                </div>
              </li>
            ))}
          </ul>
          <Link to="/settings/$section" params={{ section: "notifications" }} onClick={() => setOpen(false)} className="block border-t border-border px-4 py-2 text-xs text-muted hover:text-text">
            Choose alerts
          </Link>
        </div>
      )}
    </div>
  );
}
