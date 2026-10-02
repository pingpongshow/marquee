import { useMutation } from "@tanstack/react-query";
import { MonitorSmartphone } from "lucide-react";
import { useState, type FormEvent } from "react";
import { api, unwrap } from "@/api/client";
import { Alert, Button, Card, Input } from "@/components/ui";

/** Approve a TV's Quick Connect code (USER-4). */
export function LinkDeviceCard() {
  const [code, setCode] = useState("");
  const link = useMutation({
    mutationFn: () => unwrap(api.POST("/auth/quickconnect/authorize", { body: { code } })),
    onSuccess: () => setCode(""),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (code.replace(/\s/g, "").length === 6) link.mutate();
  };
  return (
    <Card title="Link a device" description="Signing in on an Apple TV or another device? Choose Quick Connect there and enter the code it shows. The device signs in as you.">
      {link.isSuccess && <Alert tone="success">{link.data.deviceName} is now signed in.</Alert>}
      {link.isError && <Alert tone="error">{link.error.message}</Alert>}
      <form onSubmit={submit} className="flex flex-wrap gap-2">
        <Input
          aria-label="Code"
          value={code}
          onChange={(e) => setCode(e.target.value.toUpperCase().replace(/[^A-Z0-9 ]/g, "").slice(0, 7))}
          placeholder="ABC123"
          autoComplete="off"
          autoCapitalize="characters"
          className="w-40 font-mono text-lg tracking-[0.3em] uppercase"
        />
        <Button type="submit" variant="primary" disabled={code.replace(/\s/g, "").length !== 6} loading={link.isPending}>
          <MonitorSmartphone className="size-4" /> Link
        </Button>
      </form>
    </Card>
  );
}

/** /link: a short address the TV can show. */
export function LinkPage() {
  return (
    <div className="mx-auto max-w-xl p-6 lg:p-8">
      <h1 className="mb-6 text-2xl font-bold">Link a device</h1>
      <LinkDeviceCard />
    </div>
  );
}
