import { useMutation, useQuery } from "@tanstack/react-query";
import { useNavigate, useParams } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { ApiError, api, deviceInfo, unwrap } from "@/api/client";
import { Alert, Button, Field, Input, Spinner } from "@/components/ui";
import { useAuth } from "@/lib/auth";
import { AuthLayout } from "./AuthLayout";

/** Joining with a friend's invite link (USER-13): public, outside the sign-in gate. */
export function JoinPage() {
  const { token } = useParams({ from: "/join/$token" });
  const navigate = useNavigate();
  const { signIn } = useAuth();
  const invite = useQuery({
    queryKey: ["invitation", token],
    queryFn: () => unwrap(api.GET("/invitations/{token}", { params: { path: { token } } })),
    retry: false,
  });
  const [username, setUsername] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [touched, setTouched] = useState(false);
  const accept = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/invitations/{token}/accept", {
          params: { path: { token } },
          body: { username: username.trim(), displayName: displayName.trim() || undefined, password, device: deviceInfo() },
        }),
      ),
    onSuccess: (res) => {
      signIn(res.token);
      navigate({ to: "/", replace: true });
    },
  });

  if (invite.isPending)
    return (
      <AuthLayout title="Join Marquee">
        <Spinner label="Checking your invite" />
      </AuthLayout>
    );
  if (invite.isError) {
    const gone = invite.error instanceof ApiError && invite.error.status === 404;
    return (
      <AuthLayout title="This invite can't be used">
        <Alert tone="error">
          {gone
            ? "This invite link isn't valid, or it has already been used or has expired. Ask whoever sent it for a new one."
            : invite.error instanceof ApiError && invite.error.status === 429
              ? "Too many attempts. Wait a minute and try again."
              : `Couldn't check this invite. ${invite.error.message}`}
        </Alert>
      </AuthLayout>
    );
  }

  const info = invite.data;
  const shortPassword = password.length > 0 && password.length < 8;
  const mismatch = confirm.length > 0 && confirm !== password;
  const valid = username.trim() && password.length >= 8 && confirm === password;
  const error =
    accept.error instanceof ApiError
      ? accept.error.status === 409
        ? "That username is taken. Choose another."
        : accept.error.status === 404
          ? "This invite link has just been used or has expired."
          : accept.error.message
      : accept.error?.message;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (valid) accept.mutate();
  };

  return (
    <AuthLayout
      title={info.invitedBy ? `${info.invitedBy} invited you to ${info.serverName}` : `You're invited to ${info.serverName}`}
      subtitle={
        <>
          {info.note && <span className="block">“{info.note}”</span>}
          Choose a username and password to join. The invite expires {new Date(info.expiresAt).toLocaleDateString()}.
        </>
      }
    >
      <form onSubmit={submit} className="space-y-4" noValidate>
        {error && <Alert tone="error">{error}</Alert>}
        <Field label="Username" error={touched && !username.trim() ? "Choose a username." : undefined}>
          {(id) => <Input id={id} autoComplete="username" autoCapitalize="none" autoFocus required maxLength={64} value={username} onChange={(e) => setUsername(e.target.value)} />}
        </Field>
        <Field label="Display name" help="Optional. How your name appears in Marquee.">
          {(id) => <Input id={id} autoComplete="name" maxLength={64} value={displayName} onChange={(e) => setDisplayName(e.target.value)} />}
        </Field>
        <Field label="Password" error={shortPassword || (touched && password.length < 8) ? "At least 8 characters." : undefined}>
          {(id) => <Input id={id} type="password" autoComplete="new-password" required value={password} onChange={(e) => setPassword(e.target.value)} />}
        </Field>
        <Field label="Confirm password" error={mismatch ? "The passwords don't match." : undefined}>
          {(id) => <Input id={id} type="password" autoComplete="new-password" required value={confirm} onChange={(e) => setConfirm(e.target.value)} />}
        </Field>
        <Button type="submit" variant="primary" className="w-full" loading={accept.isPending}>
          Join {info.serverName}
        </Button>
      </form>
    </AuthLayout>
  );
}
