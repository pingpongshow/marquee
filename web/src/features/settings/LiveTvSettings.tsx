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
import { liveStatusQuery } from "../livetv/api";
import { SaveBar } from "./SaveBar";

type Source = components["schemas"]["LiveTvSource"];

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
                  `${st.channels} channels, ${st.programmes} guide entries${st.refreshedAt ? ` · loaded ${new Date(st.refreshedAt).toLocaleString()}` : ""}`
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
                    ? "For example http://10.1.1.10:9191"
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
                        ? "http://10.1.1.10:9191"
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
