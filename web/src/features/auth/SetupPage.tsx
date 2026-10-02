import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { api, deviceInfo, unwrap } from "@/api/client";
import { systemInfoQuery } from "@/api/queries";
import { Alert, Button, Field, Input } from "@/components/ui";
import { useAuth } from "@/lib/auth";
import { AuthLayout } from "./AuthLayout";

/** First-run setup (ADM-1): name the server and create the admin account. */
export function SetupPage() {
  const { signIn } = useAuth();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [serverName, setServerName] = useState("Marquee");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const mismatch = confirm.length > 0 && confirm !== password;

  const setup = useMutation({
    mutationFn: () => unwrap(api.POST("/setup", { body: { serverName, username, password, device: deviceInfo() } })),
    onSuccess: async (res) => {
      signIn(res.token);
      await qc.invalidateQueries({ queryKey: systemInfoQuery.queryKey });
      navigate({ to: "/settings/$section", params: { section: "libraries" } });
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!mismatch) setup.mutate();
  };

  return (
    <AuthLayout title="Welcome to Marquee" subtitle="Let’s set up your server. You’ll add libraries next.">
      <form onSubmit={submit} className="space-y-4">
        {setup.isError && <Alert tone="error">{setup.error.message}</Alert>}
        <Field label="Server name" help="Shown to every app that connects.">
          {(id) => <Input id={id} required maxLength={64} value={serverName} onChange={(e) => setServerName(e.target.value)} />}
        </Field>
        <Field label="Admin username">
          {(id) => <Input id={id} autoComplete="username" required maxLength={64} value={username} onChange={(e) => setUsername(e.target.value)} />}
        </Field>
        <Field label="Password" help="At least 8 characters.">
          {(id) => <Input id={id} type="password" autoComplete="new-password" required minLength={8} value={password} onChange={(e) => setPassword(e.target.value)} />}
        </Field>
        <Field label="Confirm password" error={mismatch ? "Passwords don’t match" : undefined}>
          {(id) => <Input id={id} type="password" autoComplete="new-password" required value={confirm} onChange={(e) => setConfirm(e.target.value)} />}
        </Field>
        <Button variant="primary" type="submit" className="w-full" loading={setup.isPending} disabled={mismatch}>
          Create server
        </Button>
      </form>
    </AuthLayout>
  );
}
