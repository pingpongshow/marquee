import { useQuery } from "@tanstack/react-query";
import { Globe, LogOut, Monitor, Smartphone, Tablet, Tv } from "lucide-react";
import { devicesQuery, useRevokeDevice } from "@/api/queries";
import type { Device } from "@/api/types";
import { Alert, Button, Card, Spinner } from "@/components/ui";

const icons: Record<string, typeof Monitor> = { web: Globe, ios: Smartphone, android: Smartphone, ipados: Tablet, tvos: Tv, androidtv: Tv };

function ago(iso: string) {
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (s < 120) return "Active now";
  if (s < 3600) return `${Math.round(s / 60)} min ago`;
  if (s < 86400) return `${Math.round(s / 3600)} hr ago`;
  return new Date(iso).toLocaleDateString();
}

export function DevicesSettings() {
  const devices = useQuery(devicesQuery);
  const revoke = useRevokeDevice();
  return (
    <Card title="Signed-in devices" description="Signing a device out requires it to log in again.">
      {devices.isPending && <Spinner />}
      {revoke.isError && <Alert tone="error">{revoke.error.message}</Alert>}
      <ul className="-my-2 divide-y divide-border">
        {devices.data?.map((d: Device) => {
          const Icon = icons[d.platform] ?? Monitor;
          return (
            <li key={d.id} className="flex items-center gap-4 py-3">
              <Icon className="size-6 shrink-0 text-muted" aria-hidden />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="truncate font-medium">{d.name}</span>
                  {d.current && <span className="rounded bg-accent/15 px-1.5 py-0.5 text-[11px] font-medium text-accent">This device</span>}
                </div>
                <div className="truncate text-xs text-muted">
                  {d.userName} · {d.product ?? d.platform}
                  {d.version && d.version !== "dev" ? ` ${d.version}` : ""} · {ago(d.lastSeenAt)}
                  {d.lastIp ? ` · ${d.lastIp}` : ""}
                </div>
              </div>
              {!d.current && (
                <Button size="sm" variant="ghost" onClick={() => revoke.mutate(d.id)} aria-label={`Sign out ${d.name}`}>
                  <LogOut className="size-4" /> Sign out
                </Button>
              )}
            </li>
          );
        })}
      </ul>
    </Card>
  );
}
