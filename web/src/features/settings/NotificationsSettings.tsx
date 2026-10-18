import { useMutation, useQuery } from "@tanstack/react-query";
import { Send } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { meQuery, usersQuery } from "@/api/queries";
import type { components } from "@/api/schema.gen";
import { Alert, Button, Card, Field, Input, Select, Spinner, Toggle } from "@/components/ui";
import { SaveBar } from "./SaveBar";
import { SecretField } from "./ServerSections";
import { portForSecurity, type SmtpSecurity } from "./smtp";
import { useSectionDraft } from "./useSectionDraft";

type AlertType = components["schemas"]["AlertType"];
type Draft = components["schemas"]["NotificationSettings"] & { smtpPassword?: string };

export const alertTypes: { id: AlertType; label: string; help: string }[] = [
  { id: "task_failed", label: "A scheduled task failed", help: "Including the nightly database backup." },
  { id: "disk_low", label: "Disk space is low", help: "The config, transcode or recordings folder, or a library folder. Checked every 15 minutes." },
  { id: "recording_failed", label: "A recording failed", help: "Live TV DVR recordings." },
  { id: "source_failed", label: "A Live TV or movie source couldn't be loaded", help: "Checked whenever the channel and film lists refresh." },
  { id: "integration_down", label: "A connected service isn't answering", help: "Seerr, Bazarr, Ollama (AI) and Soundprint, if set up. Reported after two checks in a row fail." },
  { id: "failed_signins", label: "Repeated failed sign-ins", help: "Five wrong passwords for one account within 15 minutes." },
  { id: "new_admin_device", label: "An administrator signed in from a new device", help: "A browser or app that account hasn't used before." },
  { id: "playback_failed", label: "Playback failed", help: "The transcoder couldn't produce video for a stream." },
  { id: "vod_save_failed", label: "VOD download failed", help: "Saving a movie or episode on demand into the library failed after its retries; see Settings → VOD → Downloads. Emailed by default." },
  { id: "vod_save_done", label: "Saving to the library finished", help: "One summary when the save queue empties, not one per episode." },
];

/** Email (SMTP) for reset links, and which alerts administrators get. */
export function NotificationsSettings() {
  // The SMTP password is write-only: the server returns smtpPasswordSet; the draft carries a new one.
  const s = useSectionDraft("notifications", (d) => {
    const x = d as Draft;
    return {
      smtpHost: x.smtpHost,
      smtpPort: x.smtpPort,
      smtpSecurity: x.smtpSecurity,
      smtpUsername: x.smtpUsername,
      smtpPassword: x.smtpPassword,
      fromAddress: x.fromAddress,
      fromName: x.fromName,
      linkUrl: x.linkUrl,
      alerts: x.alerts,
      dvrQuotaAlert: x.dvrQuotaAlert,
      plexSyncAlert: x.plexSyncAlert,
      optimizeFailedAlert: x.optimizeFailedAlert,
      libraryUnreadableAlert: x.libraryUnreadableAlert,
      recipientsAllAdmins: x.recipientsAllAdmins,
      recipients: x.recipients,
      diskThresholdPercent: x.diskThresholdPercent,
    };
  });
  const remoteUrl = s.query.data?.remoteAccess.remoteUrl;
  if (!s.draft) return <Spinner />;
  const d = s.draft as Draft;
  const update = (p: Partial<Draft>) => s.update(p);
  const alerts = d.alerts ?? [];
  const toggleAlert = (id: AlertType, on: boolean) => update({ alerts: on ? [...alerts.filter((a) => a !== id), id] : alerts.filter((a) => a !== id) });

  return (
    <>
      <div className="space-y-6">
        <Card title="Email" description="Marquee sends password and PIN reset links, and alerts, through your mail server or provider (SMTP).">
          <div className="grid gap-4 sm:grid-cols-[1fr_8rem]">
            <Field label="Mail server" help="Empty turns email off.">
              {(id) => <Input id={id} autoComplete="off" placeholder="smtp.example.com" value={d.smtpHost ?? ""} onChange={(e) => update({ smtpHost: e.target.value })} />}
            </Field>
            <Field label="Port">
              {(id) => <Input id={id} type="number" min={1} max={65535} value={d.smtpPort || ""} onChange={(e) => update({ smtpPort: Number(e.target.value) || 0 })} />}
            </Field>
          </div>
          <Field label="Security" help="STARTTLS is usual on port 587, TLS on 465. Without either, a password can't be sent.">
            {(id) => (
              <Select
                id={id}
                value={d.smtpSecurity ?? "starttls"}
                onChange={(e) => {
                  const v = e.target.value as SmtpSecurity;
                  update({ smtpSecurity: v, smtpPort: portForSecurity(v, d.smtpPort) });
                }}
              >
                <option value="starttls">STARTTLS</option>
                <option value="tls">TLS</option>
                <option value="none">None</option>
              </Select>
            )}
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Username" help="Optional; leave empty if the server doesn't need a sign-in.">
              {(id) => <Input id={id} autoComplete="off" value={d.smtpUsername ?? ""} onChange={(e) => update({ smtpUsername: e.target.value })} />}
            </Field>
            <SecretField
              label="Password"
              help="Stored on the server and only sent to the mail server."
              placeholder="Password or app password"
              isSet={!!d.smtpPasswordSet}
              value={d.smtpPassword}
              onChange={(v) => update({ smtpPassword: v })}
            />
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="From address">
              {(id) => <Input id={id} type="email" autoComplete="off" placeholder="marquee@example.com" value={d.fromAddress ?? ""} onChange={(e) => update({ fromAddress: e.target.value })} />}
            </Field>
            <Field label="From name">
              {(id) => <Input id={id} placeholder="Marquee" value={d.fromName ?? ""} onChange={(e) => update({ fromName: e.target.value })} />}
            </Field>
          </div>
          <Field
            label="Link address"
            help={
              <>
                Links in mail (reset links) start with this. Empty uses the Remote Access address{remoteUrl ? ` (${remoteUrl})` : ", which isn't set, so reset links can't be sent"}.
              </>
            }
          >
            {(id) => <Input id={id} placeholder={remoteUrl || "https://marquee.example.com"} value={d.linkUrl ?? ""} onChange={(e) => update({ linkUrl: e.target.value })} />}
          </Field>
          <TestEmail disabled={s.dirty} configured={!!d.mailConfigured} />
        </Card>

        <Card
          title="Email alerts"
          description="Every alert appears under the bell in the top bar for administrators, and goes to webhooks subscribed to “Alerts”. Choose which are also emailed to the recipients below (when email is set up); VOD download failures, Large files conversion failures and unreadable library folders are chosen by default. The same alert is raised at most once an hour."
        >
          <div className="space-y-3">
            {alertTypes.map((a) => (
              <Toggle key={a.id} label={a.label} help={a.help} checked={alerts.includes(a.id)} onChange={(v) => toggleAlert(a.id, v)} />
            ))}
            <Toggle
              label="Someone is over their recording quota"
              help="A recording wasn't made, or their oldest watched recordings went to the media trash to make room."
              checked={!!d.dvrQuotaAlert}
              onChange={(v) => update({ dvrQuotaAlert: v })}
            />
            <Toggle
              label="Plex sync is failing"
              help="Plex rejects the token, or hasn't been reachable for three sync runs in a row."
              checked={!!d.plexSyncAlert}
              onChange={(v) => update({ plexSyncAlert: v })}
            />
            <Toggle
              label="Large files conversion failed"
              help="A conversion in Library Health → Large files failed (not one skipped for saving too little); the original is unchanged. Emailed by default."
              checked={!!d.optimizeFailedAlert}
              onChange={(v) => update({ optimizeFailedAlert: v })}
            />
            <Toggle
              label="Library folder can't be read"
              help="A scan couldn't read one of a library's folders (a drive that's offline, permissions). Its titles are kept until it can be read again; another notice says when it can. Emailed by default."
              checked={!!d.libraryUnreadableAlert}
              onChange={(v) => update({ libraryUnreadableAlert: v })}
            />
          </div>
          <Field label="Low disk space threshold" help="Alert when a volume has less free space than this.">
            {(id) => (
              <div className="relative w-32">
                <Input id={id} type="number" min={1} max={50} value={d.diskThresholdPercent ?? 10} onChange={(e) => update({ diskThresholdPercent: Number(e.target.value) })} className="pr-8" />
                <span className="pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 text-sm text-faint">%</span>
              </div>
            )}
          </Field>
        </Card>

        <Recipients draft={d} update={update} />
      </div>
      <SaveBar dirty={s.dirty} saving={s.saving} error={s.error} savedAt={s.savedAt} onSave={s.save} onReset={s.reset} />
    </>
  );
}

function Recipients({ draft, update }: { draft: Draft; update: (p: Partial<Draft>) => void }) {
  const users = useQuery(usersQuery);
  const admins = (users.data ?? []).filter((u) => u.isAdmin);
  const chosen = draft.recipients ?? [];
  return (
    <Card title="Alert email recipients" description="Administrators with an email address (set it in Users or on their Account page).">
      <Toggle label="All administrators with an email address" checked={!!draft.recipientsAllAdmins} onChange={(v) => update({ recipientsAllAdmins: v })} />
      {!draft.recipientsAllAdmins && (
        <fieldset className="space-y-2">
          <legend className="sr-only">Recipients</legend>
          {users.isPending && <Spinner />}
          {admins.map((u) => (
            <label key={u.id} className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                className="accent-[var(--color-accent)]"
                disabled={!u.email}
                checked={!!u.email && chosen.includes(u.id)}
                onChange={(e) => update({ recipients: e.target.checked ? [...chosen.filter((x) => x !== u.id), u.id] : chosen.filter((x) => x !== u.id) })}
              />
              {u.displayName}
              <span className="text-muted">{u.email || "no email address"}</span>
            </label>
          ))}
        </fieldset>
      )}
    </Card>
  );
}

function TestEmail({ disabled, configured }: { disabled: boolean; configured: boolean }) {
  const me = useQuery(meQuery);
  const [to, setTo] = useState<string | null>(null);
  const address = to ?? me.data?.email ?? "";
  const test = useMutation({ mutationFn: () => unwrap(api.POST("/notifications/test-email", { body: { to: address } })) });
  return (
    <div className="space-y-2 rounded-md border border-border bg-surface-2/50 p-3">
      <div className="flex flex-wrap items-end gap-3">
        <Field label="Send a test email to">
          {(id) => <Input id={id} type="email" className="w-72 max-w-full" value={address} placeholder="you@example.com" onChange={(e) => setTo(e.target.value)} />}
        </Field>
        <Button size="sm" onClick={() => test.mutate()} loading={test.isPending} disabled={disabled || !configured || !address.includes("@")}>
          <Send className="size-4" /> Send test
        </Button>
      </div>
      {disabled && <p className="text-xs text-muted">Save your changes first; the test uses the saved settings.</p>}
      {!disabled && !configured && <p className="text-xs text-muted">Enter a mail server, port and from address, and save.</p>}
      {test.data && (test.data.ok ? <Alert tone="success">Sent. Check the inbox (and the spam folder).</Alert> : <Alert tone="error">Couldn't send: {test.data.error}</Alert>)}
      {test.isError && <Alert tone="error">{test.error.message}</Alert>}
    </div>
  );
}
