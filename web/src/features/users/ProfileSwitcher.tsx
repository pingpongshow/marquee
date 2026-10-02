import { useMutation, useQuery } from "@tanstack/react-query";
import { Lock } from "lucide-react";
import { useState } from "react";
import { api, deviceInfo, unwrap } from "@/api/client";
import { meQuery, profilesQuery } from "@/api/queries";
import type { Profile } from "@/api/types";
import { PinPad } from "@/components/PinPad";
import { Alert, Button, Dialog, Input, Spinner } from "@/components/ui";
import { useAuth } from "@/lib/auth";
import { Avatar } from "./Avatar";

/** Plex Home-style profile picker with an on-screen PIN pad. */
export function ProfileSwitcher({ onClose }: { onClose: () => void }) {
  const { signIn } = useAuth();
  const profiles = useQuery(profilesQuery);
  const me = useQuery(meQuery);
  const [target, setTarget] = useState<Profile | null>(null);
  const [password, setPassword] = useState("");
  const sw = useMutation({
    mutationFn: (p: { id: number; pin?: string; password?: string }) =>
      unwrap(api.POST("/profiles/{userId}/switch", { params: { path: { userId: p.id } }, body: { pin: p.pin, password: p.password, device: deviceInfo() } })),
    onSuccess: (res) => {
      signIn(res.token);
      onClose();
    },
  });

  // Admins open managed profiles directly.
  const needs = (p: Profile) => (me.data?.isAdmin && p.isManaged ? "none" : p.requires);
  const choose = (p: Profile) => {
    if (p.id === me.data?.id) return onClose();
    if (needs(p) === "none") return sw.mutate({ id: p.id });
    setTarget(p);
  };

  return (
    <Dialog open onClose={onClose} title={target ? `Enter ${target.displayName}'s ${needs(target) === "pin" ? "PIN" : "password"}` : "Switch profile"}>
      {sw.isError && (
        <div className="mb-4">
          <Alert tone="error">{sw.error.message}</Alert>
        </div>
      )}
      {!target ? (
        profiles.isPending ? (
          <Spinner />
        ) : (
          <ul className="grid grid-cols-3 gap-4">
            {profiles.data?.map((p) => (
              <li key={p.id}>
                <button onClick={() => choose(p)} className="group flex w-full flex-col items-center gap-2 rounded-lg p-2 hover:bg-surface-2" disabled={sw.isPending}>
                  <Avatar name={p.displayName} url={p.avatarUrl} className="size-16 text-2xl group-hover:ring-2 group-hover:ring-accent">
                    {needs(p) !== "none" && <Lock className="absolute -right-0.5 -bottom-0.5 size-5 rounded-full bg-surface p-1 text-muted" aria-label="Locked" />}
                  </Avatar>
                  <span className="max-w-full truncate text-sm">{p.displayName}</span>
                  {p.id === me.data?.id && <span className="text-[11px] text-accent">Current</span>}
                </button>
              </li>
            ))}
          </ul>
        )
      ) : needs(target) === "pin" ? (
        <div className="flex flex-col items-center gap-5">
          <PinPad disabled={sw.isPending} resetKey={sw.failureCount} onComplete={(pin) => sw.mutate({ id: target.id, pin })} />
          <Button variant="ghost" size="sm" onClick={() => setTarget(null)}>
            Back
          </Button>
        </div>
      ) : (
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            sw.mutate({ id: target.id, password });
          }}
        >
          <Input type="password" autoFocus autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} aria-label="Password" />
          <div className="flex justify-between">
            <Button type="button" variant="ghost" onClick={() => setTarget(null)}>
              Back
            </Button>
            <Button type="submit" variant="primary" loading={sw.isPending}>
              Switch
            </Button>
          </div>
        </form>
      )}
    </Dialog>
  );
}
