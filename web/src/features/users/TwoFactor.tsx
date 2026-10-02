import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import qrcode from "qrcode-generator";
import { useMemo, useState } from "react";
import { api, unwrap } from "@/api/client";
import { Alert, Button, Card, Field, Input } from "@/components/ui";

/** Two-factor sign-in (TOTP) for password sign-ins: set up with an authenticator app. */
export function TwoFactorCard() {
  const qc = useQueryClient();
  const status = useQuery({
    queryKey: ["totp"],
    queryFn: () => unwrap(api.GET("/auth/totp")),
  });
  const [setup, setSetup] = useState<{
    secret: string;
    otpauthUrl: string;
  } | null>(null);
  const [code, setCode] = useState("");
  const [recovery, setRecovery] = useState<string[] | null>(null);
  const start = useMutation({
    mutationFn: () => unwrap(api.POST("/auth/totp/setup")),
    onSuccess: (s) => (setSetup(s), setCode("")),
  });
  const enable = useMutation({
    mutationFn: () => unwrap(api.POST("/auth/totp/enable", { body: { code } })),
    onSuccess: (r) => {
      setRecovery(r.recoveryCodes);
      setSetup(null);
      setCode("");
      qc.invalidateQueries({ queryKey: ["totp"] });
    },
  });
  const disable = useMutation({
    mutationFn: () =>
      unwrap(api.POST("/auth/totp/disable", { body: { code } })),
    onSuccess: () => (
      setCode(""),
      qc.invalidateQueries({ queryKey: ["totp"] })
    ),
  });
  const qr = useMemo(() => {
    if (!setup) return null;
    const q = qrcode(0, "M");
    q.addData(setup.otpauthUrl);
    q.make();
    return q.createSvgTag({ cellSize: 4, margin: 2, scalable: true });
  }, [setup]);
  const on = status.data?.enabled;

  return (
    <Card
      title="Two-factor sign-in"
      description="Ask for a code from an authenticator app (1Password, Google Authenticator, Authy…) whenever this account signs in with its password. PIN sign-in on the home network isn't affected."
    >
      <div className="space-y-4">
        {recovery && (
          <Alert tone="success">
            <p className="mb-2 font-medium">
              Two-factor sign-in is on. Keep these recovery codes somewhere
              safe; each works once if you lose your phone:
            </p>
            <pre className="grid grid-cols-2 gap-1 font-mono text-sm">
              {recovery.join("\n")}
            </pre>
          </Alert>
        )}
        {on && !recovery && (
          <p className="text-sm text-muted">
            On · {status.data?.recoveryCodesLeft ?? 0} recovery codes left.
          </p>
        )}
        {on ? (
          <div className="flex flex-wrap items-end gap-3">
            <div className="w-48">
              <Field label="Code to turn it off">
                {(id) => (
                  <Input
                    id={id}
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    value={code}
                    onChange={(e) => setCode(e.target.value)}
                  />
                )}
              </Field>
            </div>
            <Button
              variant="danger"
              onClick={() => disable.mutate()}
              disabled={!code || disable.isPending}
            >
              Turn off
            </Button>
          </div>
        ) : setup ? (
          <div className="flex flex-wrap gap-6">
            {qr && (
              <div
                className="w-44 shrink-0 rounded-lg bg-white p-2"
                aria-label="QR code for your authenticator app"
                dangerouslySetInnerHTML={{ __html: qr }}
              />
            )}
            <div className="min-w-60 flex-1 space-y-3">
              <p className="text-sm">
                Scan the code with your authenticator app, or enter this key:
              </p>
              <code className="block rounded bg-surface-2 px-2 py-1 text-sm break-all select-all">
                {setup.secret.match(/.{1,4}/g)?.join(" ")}
              </code>
              <Field label="Code from the app">
                {(id) => (
                  <Input
                    id={id}
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    autoFocus
                    value={code}
                    onChange={(e) => setCode(e.target.value)}
                  />
                )}
              </Field>
              <div className="flex gap-2">
                <Button
                  variant="primary"
                  onClick={() => enable.mutate()}
                  disabled={code.length < 6 || enable.isPending}
                >
                  Turn on
                </Button>
                <Button variant="ghost" onClick={() => setSetup(null)}>
                  Cancel
                </Button>
              </div>
            </div>
          </div>
        ) : (
          <Button onClick={() => start.mutate()} disabled={start.isPending}>
            Set up two-factor sign-in
          </Button>
        )}
        {(start.error ?? enable.error ?? disable.error) && (
          <Alert tone="error">
            {(start.error ?? enable.error ?? disable.error)!.message}
          </Alert>
        )}
      </div>
    </Card>
  );
}
