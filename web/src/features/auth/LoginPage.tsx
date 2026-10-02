import { useMutation, useQuery } from "@tanstack/react-query";
import { Lock } from "lucide-react";
import { useState, type FormEvent } from "react";
import { api, deviceInfo, unwrap } from "@/api/client";
import type { Profile } from "@/api/types";
import { PinPad } from "@/components/PinPad";
import { Alert, Button, Field, Input, Spinner } from "@/components/ui";
import { useAuth } from "@/lib/auth";
import { AuthLayout } from "./AuthLayout";

function PasswordForm({ onBack }: { onBack?: () => void }) {
  const { signIn } = useAuth();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const login = useMutation({
    mutationFn: () => unwrap(api.POST("/auth/login", { body: { username, password, device: deviceInfo() } })),
    onSuccess: (res) => signIn(res.token),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    login.mutate();
  };
  return (
    <form onSubmit={submit} className="space-y-4">
      {login.isError && <Alert tone="error">{login.error.message}</Alert>}
      <Field label="Username">{(id) => <Input id={id} autoComplete="username" autoFocus required value={username} onChange={(e) => setUsername(e.target.value)} />}</Field>
      <Field label="Password">
        {(id) => <Input id={id} type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />}
      </Field>
      <Button variant="primary" type="submit" className="w-full" loading={login.isPending}>
        Sign in
      </Button>
      {onBack && (
        <button type="button" onClick={onBack} className="w-full text-center text-sm text-muted hover:text-text">
          Back to profiles
        </button>
      )}
    </form>
  );
}

/** Plex-style "Who's watching?": choose a profile, then its PIN (or password). */
function ProfilePicker({ profiles, onUsePassword }: { profiles: Profile[]; onUsePassword: () => void }) {
  const { signIn } = useAuth();
  const [chosen, setChosen] = useState<Profile | null>(null);
  const [password, setPassword] = useState("");
  const [attempt, setAttempt] = useState(0);
  const login = useMutation({
    mutationFn: (p: { userId: number; pin?: string; password?: string }) => unwrap(api.POST("/auth/pin", { body: { ...p, device: deviceInfo() } })),
    onSuccess: (res) => signIn(res.token),
    onError: () => setAttempt((n) => n + 1),
  });
  const choose = (p: Profile) => {
    login.reset();
    if (p.requires === "none") login.mutate({ userId: p.id });
    else setChosen(p);
  };

  if (!chosen)
    return (
      <div className="space-y-6">
        {login.isError && <Alert tone="error">{login.error.message}</Alert>}
        <ul className="grid grid-cols-3 gap-4">
          {profiles.map((p) => (
            <li key={p.id}>
              <button onClick={() => choose(p)} disabled={login.isPending} className="group flex w-full flex-col items-center gap-2 rounded-lg p-2 hover:bg-surface-2">
                <span className="relative flex size-16 items-center justify-center rounded-full bg-surface-3 text-2xl font-bold group-hover:ring-2 group-hover:ring-accent">
                  {p.displayName.slice(0, 1).toUpperCase()}
                  {p.requires !== "none" && <Lock className="absolute -right-0.5 -bottom-0.5 size-5 rounded-full bg-surface p-1 text-muted" aria-label="Locked" />}
                </span>
                <span className="max-w-full truncate text-sm">{p.displayName}</span>
              </button>
            </li>
          ))}
        </ul>
        <button onClick={onUsePassword} className="w-full text-center text-sm text-muted hover:text-text">
          Sign in with username and password
        </button>
      </div>
    );

  return (
    <div className="space-y-5">
      <div className="text-center">
        <div className="mx-auto mb-2 flex size-16 items-center justify-center rounded-full bg-surface-3 text-2xl font-bold">{chosen.displayName.slice(0, 1).toUpperCase()}</div>
        <div className="font-medium">{chosen.displayName}</div>
      </div>
      {login.isError && <Alert tone="error">{login.error.message}</Alert>}
      {chosen.requires === "pin" ? (
        <PinPad disabled={login.isPending} resetKey={attempt} onComplete={(pin) => login.mutate({ userId: chosen.id, pin })} />
      ) : (
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            login.mutate({ userId: chosen.id, password });
          }}
        >
          <Input type="password" aria-label="Password" placeholder="Password" autoFocus autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
          <Button variant="primary" type="submit" className="w-full" loading={login.isPending}>
            Sign in
          </Button>
        </form>
      )}
      {login.isPending && <Spinner label="Signing in" />}
      <button onClick={() => (setChosen(null), setPassword(""), login.reset())} className="w-full text-center text-sm text-muted hover:text-text">
        Choose a different profile
      </button>
    </div>
  );
}

export function LoginPage({ serverName, pinSignIn }: { serverName: string; pinSignIn: boolean }) {
  const [usePassword, setUsePassword] = useState(false);
  const profiles = useQuery({
    queryKey: ["auth", "profiles"],
    queryFn: () => unwrap(api.GET("/auth/profiles")),
    enabled: pinSignIn,
  });
  const showPicker = pinSignIn && !usePassword && (profiles.data?.length ?? 0) > 0;
  return (
    <AuthLayout title={showPicker ? "Who's watching?" : `Sign in to ${serverName}`} subtitle={showPicker ? serverName : undefined}>
      {pinSignIn && profiles.isPending ? (
        <Spinner />
      ) : showPicker ? (
        <ProfilePicker profiles={profiles.data!} onUsePassword={() => setUsePassword(true)} />
      ) : (
        <PasswordForm onBack={pinSignIn && (profiles.data?.length ?? 0) > 0 ? () => setUsePassword(false) : undefined} />
      )}
    </AuthLayout>
  );
}
