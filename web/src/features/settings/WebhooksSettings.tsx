import { useMutation, useQuery } from "@tanstack/react-query";
import { KeyRound, Plus, Send, Trash2 } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { settingsQuery, useUpdateSettings } from "@/api/queries";
import type { components } from "@/api/schema.gen";
import { Alert, Button, Card, Field, Input, Spinner, Toggle } from "@/components/ui";
import { SaveBar } from "./SaveBar";

type Webhook = components["schemas"]["Webhook"];
type Kind = Webhook["events"][number];

const kinds: { id: Kind; label: string }[] = [
  { id: "playback.started", label: "Playback started" },
  { id: "playback.paused", label: "Paused" },
  { id: "playback.resumed", label: "Resumed" },
  { id: "playback.stopped", label: "Stopped" },
  { id: "playback.watched", label: "Watched / played" },
  { id: "library.added", label: "New in a library" },
];

/** A random secret. getRandomValues works on plain-HTTP origins (randomUUID doesn't). */
function newSecret() {
  const b = new Uint8Array(20);
  crypto.getRandomValues(b);
  return Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
}

/** Webhooks (ADM-5): URLs told about playback and new titles. */
export function WebhooksSettings() {
  const query = useQuery(settingsQuery);
  const save = useUpdateSettings();
  const server = query.data?.webhooks ?? [];
  const [draft, setDraft] = useState<Webhook[] | null>(null);
  const [savedAt, setSavedAt] = useState<number | null>(null);
  const list = draft ?? server;
  const dirty = draft !== null && JSON.stringify(draft) !== JSON.stringify(server);
  const edit = (i: number, patch: Partial<Webhook>) => setDraft(list.map((w, j) => (j === i ? { ...w, ...patch } : w)));

  if (query.isPending) return <Spinner />;
  return (
    <div className="space-y-6">
      <p className="text-sm text-muted">
        Each event is sent as a JSON POST. With a secret, the body is signed: <code className="text-xs">X-Marquee-Signature: sha256=…</code> (HMAC-SHA256). Failed deliveries are retried twice.
      </p>
      {list.map((w, i) => (
        <WebhookCard key={w.id ?? `new-${i}`} hook={w} onChange={(p) => edit(i, p)} onRemove={() => setDraft(list.filter((_, j) => j !== i))} />
      ))}
      <Button onClick={() => setDraft([...list, { name: "", url: "", events: ["playback.started", "playback.stopped"], enabled: true, secret: newSecret() }])}>
        <Plus className="size-4" /> Add webhook
      </Button>
      <SaveBar
        dirty={dirty}
        saving={save.isPending}
        error={save.error}
        savedAt={savedAt}
        onSave={() =>
          save.mutate(
            { webhooks: list },
            {
              onSuccess: () => {
                setDraft(null);
                setSavedAt(Date.now());
              },
            },
          )
        }
        onReset={() => setDraft(null)}
      />
    </div>
  );
}

function WebhookCard({ hook, onChange, onRemove }: { hook: Webhook; onChange: (p: Partial<Webhook>) => void; onRemove: () => void }) {
  const test = useMutation({ mutationFn: () => unwrap(api.POST("/webhooks/test", { body: { url: hook.url, secret: hook.secret } })) });
  const toggle = (k: Kind, on: boolean) => onChange({ events: on ? [...hook.events, k] : hook.events.filter((e) => e !== k) });
  return (
    <Card
      title={hook.name || "New webhook"}
      actions={
        <Button size="sm" variant="ghost" onClick={onRemove} aria-label={`Remove ${hook.name || "webhook"}`}>
          <Trash2 className="size-4" />
        </Button>
      }
    >
      <Toggle label="Enabled" checked={hook.enabled} onChange={(v) => onChange({ enabled: v })} />
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Name">{(id) => <Input id={id} value={hook.name} placeholder="Home Assistant" maxLength={100} onChange={(e) => onChange({ name: e.target.value })} />}</Field>
        <Field label="URL">{(id) => <Input id={id} value={hook.url} placeholder="http://homeassistant.local:8123/api/webhook/…" onChange={(e) => onChange({ url: e.target.value })} />}</Field>
      </div>
      <Field label="Secret" help="Optional. Receivers can check the signature to know the request came from Marquee.">
        {(id) => (
          <div className="flex gap-2">
            <Input id={id} value={hook.secret ?? ""} className="font-mono text-xs" onChange={(e) => onChange({ secret: e.target.value })} />
            <Button onClick={() => onChange({ secret: newSecret() })} aria-label="Make a new secret">
              <KeyRound className="size-4" />
            </Button>
          </div>
        )}
      </Field>
      <fieldset>
        <legend className="mb-2 text-sm font-medium">Events</legend>
        <div className="grid gap-2 sm:grid-cols-3">
          {kinds.map((k) => (
            <label key={k.id} className="flex items-center gap-2 text-sm">
              <input type="checkbox" className="accent-[var(--color-accent)]" checked={hook.events.includes(k.id)} onChange={(e) => toggle(k.id, e.target.checked)} />
              {k.label}
            </label>
          ))}
        </div>
      </fieldset>
      <div className="flex flex-wrap items-center gap-3">
        <Button size="sm" onClick={() => test.mutate()} loading={test.isPending} disabled={!/^https?:\/\/./.test(hook.url)}>
          <Send className="size-4" /> Send test
        </Button>
        {test.data && (test.data.ok ? <span className="text-sm text-success">Delivered (HTTP {test.data.status}).</span> : <span className="text-sm text-danger">Failed: {test.data.error}</span>)}
        {test.isError && <Alert tone="error">{test.error.message}</Alert>}
      </div>
    </Card>
  );
}
