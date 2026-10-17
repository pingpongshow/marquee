import { useMutation, useQuery } from "@tanstack/react-query";
import { Plus, RefreshCw, Trash2 } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { settingsQuery, useUpdateSettings } from "@/api/queries";
import type { components } from "@/api/schema.gen";
import {
  Alert,
  Button,
  Card,
  Field,
  Input,
  Select,
  Spinner,
  Toggle,
} from "@/components/ui";
import { allRecordingUsageQuery, liveStatusQuery, usageLabel } from "../livetv/api";
import { FolderBrowser } from "./FolderBrowser";
import { SaveBar } from "./SaveBar";

type Source = components["schemas"]["LiveTvSource"];
type Commercials = NonNullable<
  components["schemas"]["IntegrationSettingsUpdate"]["commercials"]
>;
type QuotaAction = NonNullable<
  components["schemas"]["IntegrationSettingsUpdate"]["dvrQuotaAction"]
>;

/** Live TV sources (LIVE-1): M3U playlists with XMLTV guides, or Dispatcharr. */
export function LiveTvSettings() {
  const query = useQuery(settingsQuery);
  const status = useQuery(liveStatusQuery);
  const save = useUpdateSettings();
  const server = query.data?.integrations?.liveTvSources ?? [];
  const [draft, setDraft] = useState<Source[] | null>(null);
  const [savedAt, setSavedAt] = useState<number | null>(null);
  const refresh = useMutation({
    mutationFn: () => unwrap(api.POST("/livetv/refresh")),
    onSuccess: () => status.refetch(),
  });
  if (query.isPending) return <Spinner />;
  const list = draft ?? server;
  const dirty =
    draft !== null && JSON.stringify(draft) !== JSON.stringify(server);
  const edit = (i: number, patch: Partial<Source>) =>
    setDraft(list.map((s, j) => (j === i ? { ...s, ...patch } : s)));
  const state = (id?: string) => status.data?.sources.find((s) => s.id === id);

  return (
    <div className="space-y-6">
      <p className="text-sm text-muted">
        Channels and the guide come from an M3U playlist with an XMLTV guide, or
        from Dispatcharr (which can also bring in free channels such as Pluto
        TV). Marquee reloads them every four hours and streams channels to every
        app, transcoding on the GPU when a device needs it.
      </p>
      {list.map((s, i) => {
        const st = state(s.id);
        return (
          <Card
            key={s.id ?? `new-${i}`}
            title={s.name || "New source"}
            description={
              st ? (
                st.error ? (
                  <span className="text-danger">{st.error}</span>
                ) : (
                  `${st.channels} channels, ${st.programmes} guide entries${st.refreshedAt ? ` · loaded ${new Date(st.refreshedAt).toLocaleString()}` : ""}${st.maxStreams ? ` · ${st.inUse ?? 0} of ${st.maxStreams} connections in use` : st.inUse ? ` · ${st.inUse} connection${st.inUse === 1 ? "" : "s"} in use` : ""}`
                )
              ) : undefined
            }
            actions={
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setDraft(list.filter((_, j) => j !== i))}
                aria-label="Remove source"
              >
                <Trash2 className="size-4" />
              </Button>
            }
          >
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Name">
                {(id) => (
                  <Input
                    id={id}
                    value={s.name}
                    onChange={(e) => edit(i, { name: e.target.value })}
                  />
                )}
              </Field>
              <Field label="Type">
                {(id) => (
                  <Select
                    id={id}
                    value={s.kind}
                    onChange={(e) =>
                      edit(i, { kind: e.target.value as Source["kind"] })
                    }
                  >
                    <option value="dispatcharr">Dispatcharr</option>
                    <option value="m3u">M3U playlist</option>
                  </Select>
                )}
              </Field>
              <Field
                label={
                  s.kind === "dispatcharr"
                    ? "Dispatcharr address"
                    : "Playlist URL"
                }
                help={
                  s.kind === "dispatcharr"
                    ? "For example http://192.168.1.20:9191"
                    : undefined
                }
              >
                {(id) => (
                  <Input
                    id={id}
                    value={s.url}
                    onChange={(e) => edit(i, { url: e.target.value })}
                    placeholder={
                      s.kind === "dispatcharr"
                        ? "http://192.168.1.20:9191"
                        : "http://…/playlist.m3u"
                    }
                  />
                )}
              </Field>
              {s.kind === "m3u" && (
                <Field
                  label="Guide (XMLTV) URL"
                  help="Optional when the playlist names its guide."
                >
                  {(id) => (
                    <Input
                      id={id}
                      value={s.epgUrl ?? ""}
                      onChange={(e) => edit(i, { epgUrl: e.target.value })}
                    />
                  )}
                </Field>
              )}
              <Field
                label="Connections allowed"
                help="How many channels the provider lets you stream at once (0 = no limit). At the limit a recording keeps its connection: its channel can still be watched, other channels show as unavailable, and a recording that needs a connection stops live TV on another channel."
              >
                {(id) => (
                  <Input
                    id={id}
                    type="number"
                    min={0}
                    inputMode="numeric"
                    value={s.maxStreams ?? 0}
                    onChange={(e) =>
                      edit(i, {
                        maxStreams: Math.max(0, Math.floor(Number(e.target.value) || 0)),
                      })
                    }
                  />
                )}
              </Field>
              <Field
                label="User agent"
                help="Only if the provider needs a particular one."
              >
                {(id) => (
                  <Input
                    id={id}
                    value={s.userAgent ?? ""}
                    onChange={(e) => edit(i, { userAgent: e.target.value })}
                  />
                )}
              </Field>
            </div>
            <div className="mt-4">
              <Toggle
                label="Enabled"
                checked={s.enabled !== false}
                onChange={(v) => edit(i, { enabled: v })}
              />
            </div>
          </Card>
        );
      })}
      <div className="flex flex-wrap gap-3">
        <Button
          onClick={() =>
            setDraft([
              ...list,
              {
                name: "Dispatcharr",
                kind: "dispatcharr",
                url: "",
                enabled: true,
              },
            ])
          }
        >
          <Plus className="size-4" /> Add source
        </Button>
        {server.length > 0 && (
          <Button
            variant="ghost"
            onClick={() => refresh.mutate()}
            disabled={refresh.isPending}
          >
            <RefreshCw
              className={refresh.isPending ? "size-4 animate-spin" : "size-4"}
            />{" "}
            Reload channels and guide now
          </Button>
        )}
      </div>
      {status.data && server.length > 0 && (
        <p className="text-sm text-muted">
          {status.data.channels} channels
          {status.data.guideUntil &&
            `, guide until ${new Date(status.data.guideUntil).toLocaleString()}`}
          .
        </p>
      )}
      {refresh.error && <Alert tone="error">{refresh.error.message}</Alert>}
      {server.length > 0 && <RecordingSettings />}
      {server.length > 0 && <DvrUsageList />}
      <SaveBar
        dirty={dirty}
        saving={save.isPending}
        error={save.error}
        savedAt={savedAt}
        onSave={() =>
          save.mutate(
            { integrations: { liveTvSources: list } },
            {
              onSuccess: () => {
                setDraft(null);
                setSavedAt(Date.now());
                setTimeout(() => status.refetch(), 3000);
              },
            },
          )
        }
        onReset={() => setDraft(null)}
      />
    </div>
  );
}

/** DVR (LIVE-5): where recordings go and how much padding they get. Saved on change. */
function RecordingSettings() {
  const query = useQuery(settingsQuery);
  const status = useQuery(liveStatusQuery);
  const save = useUpdateSettings();
  const i = query.data?.integrations;
  const minutes = (n: number) => (n === 0 ? "None" : `${n} min`);
  return (
    <Card
      title="Recording"
      description={
        status.data?.dvrAvailable ? (
          "Recordings appear only under Live TV → Recordings (not in the libraries, Home or search), and everyone who may view recordings sees them (within their rating limit). Who may record or view, and per-person quotas, are in Settings → Users."
        ) : (
          <span className="text-danger">
            The recordings folder ({i?.dvrFolder || i?.dvrFolderDefault || "/recordings"}) can't be
            written, so recording is off.
          </span>
        )
      }
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Start early">
          {(id) => (
            <Select
              id={id}
              value={i?.dvrPaddingBefore ?? 1}
              onChange={(e) =>
                save.mutate({
                  integrations: { dvrPaddingBefore: Number(e.target.value) },
                })
              }
            >
              {[0, 1, 2, 5, 10].map((n) => (
                <option key={n} value={n}>
                  {minutes(n)}
                </option>
              ))}
            </Select>
          )}
        </Field>
        <Field label="End late">
          {(id) => (
            <Select
              id={id}
              value={i?.dvrPaddingAfter ?? 3}
              onChange={(e) =>
                save.mutate({
                  integrations: { dvrPaddingAfter: Number(e.target.value) },
                })
              }
            >
              {[0, 1, 3, 5, 10, 15, 30, 60].map((n) => (
                <option key={n} value={n}>
                  {minutes(n)}
                </option>
              ))}
            </Select>
          )}
        </Field>
        <Field
          label="Commercials"
          help={
            i?.comskipAvailable === false ? (
              <span className="text-danger">
                This build of Marquee doesn't include Comskip, so commercials
                can't be found.
              </span>
            ) : (
              "Found with Comskip when a recording finishes, and nightly for older ones. Removing cuts them out of the file; the original goes to the media trash, where it can be restored from the recording's menu until the media trash is emptied (every night)."
            )
          }
        >
          {(id) => (
            <Select
              id={id}
              value={i?.commercials ?? "off"}
              disabled={i?.comskipAvailable === false}
              onChange={(e) =>
                save.mutate({
                  integrations: {
                    commercials: e.target.value as Commercials,
                  },
                })
              }
            >
              <option value="off">Leave recordings alone</option>
              <option value="detect">Detect only (keep the files as they are)</option>
              <option value="remove">Remove commercials from recordings</option>
            </Select>
          )}
        </Field>
        <Field
          label="Recording quota per person"
          help="Space each person's recordings may take (finished and in progress). 0 = no limit. A person's own quota in Settings → Users overrides it."
        >
          {(id) => (
            <QuotaInput
              id={id}
              value={i?.dvrDefaultQuotaGb ?? 0}
              onSave={(gb) => save.mutate({ integrations: { dvrDefaultQuotaGb: gb } })}
            />
          )}
        </Field>
        <Field
          label="When someone is over quota"
          help="Recordings in progress always finish. Removed recordings go to the media trash (restorable until it's emptied, every night); one being watched is never removed."
        >
          {(id) => (
            <Select
              id={id}
              value={i?.dvrQuotaAction ?? "block"}
              onChange={(e) =>
                save.mutate({
                  integrations: { dvrQuotaAction: e.target.value as QuotaAction },
                })
              }
            >
              <option value="block">Don't record anything new</option>
              <option value="deleteOldest">Remove their oldest watched recordings to make room</option>
            </Select>
          )}
        </Field>
      </div>
      <DvrFolderField
        value={i?.dvrFolder ?? ""}
        fallback={i?.dvrFolderDefault ?? ""}
        inLibrary={i?.dvrFolderInLibrary}
      />
      {save.error && <Alert tone="error">{save.error.message}</Alert>}
    </Card>
  );
}

/** A whole number of GB, saved when it changes and the field loses focus. */
function QuotaInput({ id, value, onSave }: { id: string; value: number; onSave: (gb: number) => void }) {
  const [draft, setDraft] = useState<string | null>(null);
  const commit = () => {
    if (draft === null) return;
    const n = Math.max(0, Math.floor(Number(draft)));
    if (Number.isFinite(n) && n !== value) onSave(n);
    setDraft(null);
  };
  return (
    <div className="flex items-center gap-2">
      <Input
        id={id}
        type="number"
        min={0}
        inputMode="numeric"
        className="w-32"
        value={draft ?? String(value)}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => e.key === "Enter" && commit()}
      />
      <span className="text-sm text-muted">GB</span>
    </div>
  );
}

/** Where new recordings go: empty = the server's configured folder. Checked on save. */
function DvrFolderField({ value, fallback, inLibrary }: { value: string; fallback: string; inLibrary?: boolean }) {
  const save = useUpdateSettings();
  const [draft, setDraft] = useState<string | null>(null);
  const [browsing, setBrowsing] = useState(false);
  const current = draft ?? value;
  const dirty = draft !== null && draft.trim() !== value;
  const commit = (folder: string) =>
    save.mutate({ integrations: { dvrFolder: folder } }, { onSuccess: () => setDraft(null) });
  return (
    <div className="mt-4">
      <Field
        label="Recordings folder"
        help={`Where new recordings go. Empty = the server's folder${fallback ? ` (${fallback})` : ""}. Recordings already made stay where they are and keep working; the Recorded TV library takes the new folder too.`}
      >
        {(id) => (
          <div className="space-y-2">
            <div className="flex flex-wrap gap-2">
              <Input
                id={id}
                value={current}
                placeholder={fallback || "/recordings"}
                onChange={(e) => setDraft(e.target.value)}
                className="min-w-0 flex-1 font-mono"
              />
              <Button size="sm" variant="secondary" className="h-10" onClick={() => setBrowsing((b) => !b)}>
                {browsing ? "Close" : "Browse"}
              </Button>
              {value && (
                <Button size="sm" variant="ghost" className="h-10" onClick={() => commit("")}>
                  Use default
                </Button>
              )}
              {dirty && (
                <Button size="sm" variant="primary" className="h-10" disabled={save.isPending} onClick={() => commit(current.trim())}>
                  Save
                </Button>
              )}
            </div>
            {browsing && (
              <FolderBrowser
                initialPath={current || null}
                onPick={(p) => {
                  setDraft(p);
                  setBrowsing(false);
                }}
              />
            )}
          </div>
        )}
      </Field>
      {inLibrary === false && (
        <Alert tone="error">
          No library includes this folder's TV (and Movies) folder, so new recordings won't appear in the library. Add it to the Recorded TV library in Settings → Libraries.
        </Alert>
      )}
      {save.error && <Alert tone="error">{save.error.message}</Alert>}
    </div>
  );
}

/** Admins: how much space each person's recordings take, against their quota. */
function DvrUsageList() {
  const usage = useQuery(allRecordingUsageQuery);
  const rows = (usage.data ?? []).filter((u) => u.recordings > 0 || u.quotaSource === "user");
  return (
    <Card title="Recording space" description="Each person's recordings (finished and in progress), against their quota.">
      {usage.isPending ? (
        <Spinner />
      ) : rows.length === 0 ? (
        <p className="text-sm text-faint">No recordings yet.</p>
      ) : (
        <ul className="divide-y divide-border">
          {rows.map((u) => (
            <li key={u.userId} className="flex flex-wrap items-center gap-x-4 gap-y-1 py-2 text-sm">
              <span className="min-w-0 flex-1 truncate font-medium">{u.displayName}</span>
              <span className="text-muted">
                {u.recordings} recording{u.recordings === 1 ? "" : "s"}
              </span>
              <span className={u.overQuota ? "text-danger" : "text-muted"}>
                {usageLabel(u)}
                {u.quotaSource === "default" && u.quotaBytes > 0 ? " (default)" : ""}
                {u.overQuota ? " · over quota" : ""}
              </span>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}
