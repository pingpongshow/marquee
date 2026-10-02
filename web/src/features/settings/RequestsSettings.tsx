import { useMutation, useQuery } from "@tanstack/react-query";
import { Check, X } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { settingsQuery, useUpdateSettings } from "@/api/queries";
import {
  Alert,
  Badge,
  Button,
  Card,
  Dialog,
  Field,
  Input,
  Spinner,
} from "@/components/ui";
import {
  allRequestsQuery,
  type MediaRequest,
  statusLabel,
  useRequestAction,
} from "../requests/api";
import { SaveBar } from "./SaveBar";

/** Requests (REQ-1): the Seerr connection, and approving what people ask for. */
export function RequestsSettings() {
  return (
    <div className="space-y-6">
      <Approvals />
      <Connection />
    </div>
  );
}

function Connection() {
  const query = useQuery(settingsQuery);
  const save = useUpdateSettings();
  const saved = query.data?.integrations;
  const [url, setUrl] = useState<string | null>(null);
  const [key, setKey] = useState("");
  const [savedAt, setSavedAt] = useState<number | null>(null);
  const test = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/requests/seerr/test", {
          body: { url: url ?? saved?.seerrUrl ?? "", apiKey: key || undefined },
        }),
      ),
  });
  if (query.isPending) return <Spinner />;
  const value = url ?? saved?.seerrUrl ?? "";
  const dirty = (url !== null && url !== (saved?.seerrUrl ?? "")) || key !== "";
  return (
    <Card
      title="Seerr"
      description="People search for movies and shows that aren't here and request them from any Marquee app. Each request waits for an admin's approval below, then goes to Seerr (which hands it to Radarr or Sonarr)."
    >
      <div className="space-y-4">
        <Field
          label="Address"
          help="For example http://10.1.1.10:5055. Leave empty to turn requests off."
        >
          {(id) => (
            <Input
              id={id}
              value={value}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="http://10.1.1.10:5055"
            />
          )}
        </Field>
        <Field
          label="API key"
          help={
            saved?.seerrApiKeySet
              ? "Saved. Enter a new key to replace it."
              : "Seerr → Settings → General → API Key."
          }
        >
          {(id) => (
            <Input
              id={id}
              type="password"
              autoComplete="off"
              value={key}
              onChange={(e) => setKey(e.target.value)}
              placeholder={saved?.seerrApiKeySet ? "••••••••" : ""}
            />
          )}
        </Field>
        <div className="flex items-center gap-3">
          <Button
            onClick={() => test.mutate()}
            disabled={!value || test.isPending}
          >
            Test connection
          </Button>
          {test.data &&
            (test.data.ok ? (
              <span className="text-sm text-green-500">
                Connected to Seerr {test.data.version}
              </span>
            ) : (
              <span className="text-sm text-danger">{test.data.error}</span>
            ))}
        </div>
        <p className="text-sm text-muted">
          Who may request is set per person in Settings → Users.
        </p>
      </div>
      <SaveBar
        dirty={dirty}
        saving={save.isPending}
        error={save.error}
        savedAt={savedAt}
        onSave={() =>
          save.mutate(
            {
              integrations: { seerrUrl: value, seerrApiKey: key || undefined },
            },
            {
              onSuccess: () => {
                setUrl(null);
                setKey("");
                setSavedAt(Date.now());
              },
            },
          )
        }
        onReset={() => {
          setUrl(null);
          setKey("");
        }}
      />
    </Card>
  );
}

function Approvals() {
  const list = useQuery(allRequestsQuery);
  const approve = useRequestAction((id: number) =>
    unwrap(
      api.POST("/requests/{requestId}/approve", {
        params: { path: { requestId: id } },
      }),
    ),
  );
  const decline = useRequestAction((v: { id: number; reason: string }) =>
    unwrap(
      api.POST("/requests/{requestId}/decline", {
        params: { path: { requestId: v.id } },
        body: { reason: v.reason || undefined },
      }),
    ),
  );
  const [declining, setDeclining] = useState<MediaRequest | null>(null);
  const [reason, setReason] = useState("");
  if (list.isPending) return <Spinner />;
  const pending = list.data?.filter((r) => r.status === "pending") ?? [];
  const decided =
    list.data?.filter((r) => r.status !== "pending").slice(0, 50) ?? [];
  return (
    <>
      <Card
        title={`Waiting for approval${pending.length ? ` (${pending.length})` : ""}`}
      >
        {pending.length === 0 && (
          <p className="text-sm text-muted">Nothing to approve.</p>
        )}
        <ul className="divide-y divide-border">
          {pending.map((r) => (
            <li key={r.id} className="flex flex-wrap items-center gap-3 py-3">
              {r.posterUrl && (
                <img src={r.posterUrl} alt="" className="w-10 rounded" />
              )}
              <div className="min-w-0 flex-1">
                <div className="font-medium">
                  {r.title}{" "}
                  {r.year && <span className="text-faint">({r.year})</span>}
                </div>
                <div className="text-xs text-muted">
                  {r.userName} ·{" "}
                  {r.mediaType === "tv"
                    ? r.seasons.length
                      ? `Seasons ${r.seasons.join(", ")}`
                      : "All seasons"
                    : "Movie"}{" "}
                  · {new Date(r.createdAt).toLocaleString()}
                </div>
              </div>
              <Button
                size="sm"
                variant="primary"
                onClick={() => approve.mutate(r.id)}
                disabled={approve.isPending}
                aria-label={`Approve ${r.title}`}
              >
                <Check className="size-4" /> Approve
              </Button>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  setReason("");
                  setDeclining(r);
                }}
                aria-label={`Decline ${r.title}`}
              >
                <X className="size-4" /> Decline
              </Button>
            </li>
          ))}
        </ul>
        {approve.error && <Alert tone="error">{approve.error.message}</Alert>}
      </Card>
      {declining && (
        <Dialog
          open
          onClose={() => setDeclining(null)}
          title={`Decline ${declining.title}?`}
          footer={
            <>
              <Button variant="ghost" onClick={() => setDeclining(null)}>
                Cancel
              </Button>
              <Button
                variant="danger"
                onClick={() =>
                  decline.mutate(
                    { id: declining.id, reason },
                    { onSuccess: () => setDeclining(null) },
                  )
                }
              >
                Decline
              </Button>
            </>
          }
        >
          <Field
            label="Reason (optional)"
            help={`${declining.userName} sees this with the request.`}
          >
            {(id) => (
              <Input
                id={id}
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                autoFocus
              />
            )}
          </Field>
        </Dialog>
      )}
      {decided.length > 0 && (
        <Card title="Recent requests">
          <ul className="divide-y divide-border text-sm">
            {decided.map((r) => (
              <li key={r.id} className="flex items-center gap-3 py-2">
                <span className="min-w-0 flex-1 truncate">
                  {r.title} <span className="text-faint">· {r.userName}</span>
                  {r.reason && (
                    <span className="text-faint"> · {r.reason}</span>
                  )}
                </span>
                <Badge
                  tone={
                    r.status === "failed" || r.status === "declined"
                      ? "danger"
                      : "accent"
                  }
                >
                  {statusLabel[r.status]}
                </Badge>
              </li>
            ))}
          </ul>
        </Card>
      )}
    </>
  );
}
