import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Copy, Trash2 } from "lucide-react";
import { useState, type ReactNode } from "react";
import { api, unwrap } from "@/api/client";
import type { Invite, InviteCreated, UserRestrictions } from "@/api/types";
import { Alert, Badge, Button, Dialog, Field, Input, Spinner } from "@/components/ui";

export const invitesQuery = queryOptions({
  queryKey: ["invites"],
  queryFn: () => unwrap(api.GET("/invites")),
});

function status(i: Invite): { label: string; tone: "accent" | "muted" | "danger" } {
  if (i.usedAt) return { label: `Used by ${i.usedBy || "someone"}`, tone: "muted" };
  if (i.expiresAt && new Date(i.expiresAt).getTime() < Date.now()) return { label: "Expired", tone: "danger" };
  return { label: `Pending${i.expiresAt ? ` · expires ${new Date(i.expiresAt).toLocaleDateString()}` : ""}`, tone: "accent" };
}

/** Invites sent to friends (USER-13): who they're for, whether they've been used, and Delete. */
export function InviteList() {
  const qc = useQueryClient();
  const invites = useQuery(invitesQuery);
  const del = useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE("/invites/{inviteId}", { params: { path: { inviteId: id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["invites"] }),
  });
  if (invites.isPending) return <Spinner />;
  if (!invites.data?.length) return null;
  return (
    <div>
      <h3 className="mb-2 text-sm font-semibold text-muted">Invites</h3>
      {del.isError && <Alert tone="error">{del.error.message}</Alert>}
      <ul className="divide-y divide-border rounded-md border border-border" aria-label="Invites">
        {invites.data.map((i) => {
          const s = status(i);
          return (
            <li key={i.id} className="flex items-center gap-3 px-3 py-2">
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm font-medium">{i.note || "Invite"}</div>
                <div className="text-xs text-faint">Created {new Date(i.createdAt).toLocaleDateString()}</div>
              </div>
              <Badge tone={s.tone}>{s.label}</Badge>
              <Button
                size="sm"
                variant="ghost"
                aria-label={`Delete invite ${i.note || i.id}`}
                loading={del.isPending && del.variables === i.id}
                onClick={() => del.mutate(i.id)}
              >
                <Trash2 className="size-4" />
              </Button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

/** Copies text, falling back to selecting it in the field: the Clipboard API needs HTTPS, and the LAN uses plain HTTP. */
async function copyText(text: string, input: HTMLInputElement | null) {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    /* fall through */
  }
  if (!input) return false;
  input.focus();
  input.select();
  try {
    return document.execCommand("copy");
  } catch {
    return false;
  }
}

/** Makes an invite link for a friend, with the access they get (USER-13). */
export function InviteDialog({
  onClose,
  restrictionsEditor,
}: {
  onClose: () => void;
  restrictionsEditor: (value: UserRestrictions, onChange: (r: UserRestrictions) => void) => ReactNode;
}) {
  const qc = useQueryClient();
  const [note, setNote] = useState("");
  const [days, setDays] = useState(7);
  const [restrictions, setRestrictions] = useState<UserRestrictions>({});
  const [created, setCreated] = useState<InviteCreated | null>(null);
  const [copied, setCopied] = useState<boolean | null>(null);
  const create = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/invites", {
          body: { note: note.trim() || undefined, expiresDays: days, restrictions: { ...restrictions, friend: true } },
        }),
      ),
    onSuccess: (c) => {
      setCreated(c);
      qc.invalidateQueries({ queryKey: ["invites"] });
    },
  });
  const validDays = Number.isInteger(days) && days >= 1 && days <= 90;
  const link = created ? `${location.origin}${created.path}` : "";

  return (
    <Dialog
      open
      wide
      onClose={onClose}
      title="Invite a friend"
      footer={
        created ? (
          <Button variant="primary" onClick={onClose}>
            Done
          </Button>
        ) : (
          <>
            <Button variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button variant="primary" disabled={!validDays} loading={create.isPending} onClick={() => create.mutate()}>
              Create invite link
            </Button>
          </>
        )
      }
    >
      {created ? (
        <div className="space-y-4">
          <Alert tone="success">Invite created. Send this link to {note.trim() || "your friend"}; it works once.</Alert>
          <Field label="Invite link" help="The link must use an address your friend can reach (for example your Tailscale or public address).">
            {(id) => (
              <div className="flex gap-2">
                <Input id={id} readOnly value={link} onFocus={(e) => e.currentTarget.select()} className="font-mono text-xs" />
                <Button onClick={async () => setCopied(await copyText(link, document.getElementById(id) as HTMLInputElement | null))}>
                  {copied ? <Check className="size-4" /> : <Copy className="size-4" />} {copied ? "Copied" : "Copy"}
                </Button>
              </div>
            )}
          </Field>
          {copied === false && <p className="text-sm text-muted">The link is selected; copy it with ⌘C or Ctrl+C.</p>}
        </div>
      ) : (
        <div className="space-y-5">
          {create.isError && <Alert tone="error">{create.error.message}</Alert>}
          <p className="text-sm text-muted">
            Your friend opens the link, picks a username and password, and joins with the access below. Friends sign in with their own password and aren't shown on this
            server's profile picker.
          </p>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Who it's for" help="A note for you, e.g. Mum.">
              {(id) => <Input id={id} maxLength={100} value={note} onChange={(e) => setNote(e.target.value)} />}
            </Field>
            <Field label="Expires after (days)" error={validDays ? undefined : "Between 1 and 90 days."}>
              {(id) => <Input id={id} type="number" min={1} max={90} value={Number.isNaN(days) ? "" : days} onChange={(e) => setDays(e.target.valueAsNumber)} />}
            </Field>
          </div>
          {restrictionsEditor(restrictions, setRestrictions)}
        </div>
      )}
    </Dialog>
  );
}
