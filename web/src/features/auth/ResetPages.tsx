import { useMutation, useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { api, unwrap } from "@/api/client";
import { PinPad } from "@/components/PinPad";
import { Alert, Button, Field, Input, Spinner } from "@/components/ui";
import { AuthLayout } from "./AuthLayout";

export type ResetKind = "password" | "pin";

/** "Forgot password?" / "Forgot PIN?": asks for the username or email and requests a link. */
export function ForgotForm({ kind, onBack }: { kind: ResetKind; onBack: () => void }) {
  const [login, setLogin] = useState("");
  const forgot = useMutation({ mutationFn: () => unwrap(api.POST("/auth/forgot", { body: { login: login.trim(), kind } })) });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    forgot.mutate();
  };
  const what = kind === "pin" ? "PIN" : "password";
  if (forgot.isSuccess)
    return (
      <div className="space-y-4" data-testid="forgot-sent">
        <Alert tone="success">If that account has an email address, a link is on its way. It works for 30 minutes.</Alert>
        <Button className="w-full" onClick={onBack}>
          Back to sign in
        </Button>
      </div>
    );
  return (
    <form onSubmit={submit} className="space-y-4">
      <p className="text-sm text-muted">Enter your username or email address. If your account has an email address, we'll send a link to choose a new {what}.</p>
      {forgot.isError && <Alert tone="error">{forgot.error.message}</Alert>}
      <Field label="Username or email">
        {(id) => <Input id={id} autoComplete="username" autoFocus required maxLength={254} value={login} onChange={(e) => setLogin(e.target.value)} />}
      </Field>
      <Button variant="primary" type="submit" className="w-full" loading={forgot.isPending}>
        Send link
      </Button>
      <button type="button" onClick={onBack} className="w-full text-center text-sm text-muted hover:text-text">
        Back to sign in
      </button>
    </form>
  );
}

/** /reset?token=…: choose a new password or PIN with a mailed link. */
export function ResetPage({ token }: { token: string }) {
  const navigate = useNavigate();
  const info = useQuery({
    queryKey: ["reset-info", token],
    queryFn: () => unwrap(api.POST("/auth/reset/info", { body: { token } })),
    enabled: !!token,
    retry: false,
    staleTime: Infinity,
  });
  const [password, setPassword] = useState("");
  const [again, setAgain] = useState("");
  const [mismatch, setMismatch] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const reset = useMutation({
    mutationFn: (body: { password?: string; pin?: string }) => unwrap(api.POST("/auth/reset", { body: { token, ...body } })),
    onError: () => setAttempt((n) => n + 1),
  });
  const toSignIn = () => void navigate({ to: "/", replace: true });

  const kind = info.data?.kind;
  const title = kind === "pin" ? "Choose a new PIN" : "Choose a new password";
  let body;
  if (!token || info.isError)
    body = (
      <div className="space-y-4">
        <Alert tone="error">{info.error?.message ?? "This link is incomplete."} Ask for a new link from the sign-in page.</Alert>
        <Button className="w-full" onClick={toSignIn}>
          Go to sign in
        </Button>
      </div>
    );
  else if (info.isPending) body = <Spinner />;
  else if (reset.isSuccess)
    body = (
      <div className="space-y-4" data-testid="reset-done">
        <Alert tone="success">Your {kind === "pin" ? "PIN" : "password"} was changed, and every device was signed out. Sign in with the new one.</Alert>
        <Button variant="primary" className="w-full" onClick={toSignIn}>
          Sign in
        </Button>
      </div>
    );
  else if (kind === "pin")
    body = (
      <div className="space-y-4">
        <p className="text-center text-sm text-muted">Enter a new 4-digit PIN for {info.data.displayName}.</p>
        {reset.isError && <Alert tone="error">{reset.error.message}</Alert>}
        <PinPad disabled={reset.isPending} resetKey={attempt} onComplete={(pin) => reset.mutate({ pin })} />
      </div>
    );
  else
    body = (
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          if (password !== again) return setMismatch(true);
          setMismatch(false);
          reset.mutate({ password });
        }}
      >
        <p className="text-sm text-muted">For {info.data.displayName}. At least 8 characters.</p>
        {mismatch && <Alert tone="error">The passwords don't match.</Alert>}
        {reset.isError && <Alert tone="error">{reset.error.message}</Alert>}
        <Field label="New password">
          {(id) => <Input id={id} type="password" autoComplete="new-password" autoFocus required minLength={8} maxLength={256} value={password} onChange={(e) => setPassword(e.target.value)} />}
        </Field>
        <Field label="New password again">
          {(id) => <Input id={id} type="password" autoComplete="new-password" required minLength={8} maxLength={256} value={again} onChange={(e) => setAgain(e.target.value)} />}
        </Field>
        <Button variant="primary" type="submit" className="w-full" loading={reset.isPending}>
          Change password
        </Button>
      </form>
    );
  return <AuthLayout title={title}>{body}</AuthLayout>;
}
