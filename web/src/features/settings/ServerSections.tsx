import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { systemInfoQuery } from "@/api/queries";
import { Alert, Button, Card, Field, Input, Select, Spinner, StringListEditor, Toggle } from "@/components/ui";
import { SaveBar } from "./SaveBar";
import { RestartServer } from "./TasksSettings";
import { useSectionDraft } from "./useSectionDraft";

export const languages = [
  ["en-US", "English (US)"],
  ["en-GB", "English (UK)"],
  ["ja-JP", "Japanese"],
  ["es-ES", "Spanish"],
  ["fr-FR", "French"],
  ["de-DE", "German"],
  ["it-IT", "Italian"],
  ["pt-BR", "Portuguese (Brazil)"],
  ["ko-KR", "Korean"],
  ["zh-CN", "Chinese (Simplified)"],
] as const;

/** Number input that edits a kbps value in Mbps. Empty or 0 means "not set / unlimited". */
export function MbpsInput({ id, kbps, onChange, placeholder }: { id: string; kbps: number | undefined; onChange: (kbps: number) => void; placeholder?: string }) {
  return (
    <div className="relative">
      <Input
        id={id}
        type="number"
        min={0}
        step={0.1}
        inputMode="decimal"
        placeholder={placeholder}
        value={kbps ? kbps / 1000 : ""}
        onChange={(e) => onChange(Math.round((parseFloat(e.target.value) || 0) * 1000))}
        className="pr-14"
      />
      <span className="pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 text-sm text-faint">Mbps</span>
    </div>
  );
}

export function GeneralSettings() {
  const s = useSectionDraft("general");
  if (!s.draft) return <Spinner />;
  return (
    <>
      <Card>
        <Field label="Server name" help="Shown in every Marquee app.">
          {(id) => <Input id={id} maxLength={64} value={s.draft!.serverName ?? ""} onChange={(e) => s.update({ serverName: e.target.value })} />}
        </Field>
        <Field label="Default metadata language" help="Titles, summaries and artwork are fetched in this language. Libraries can override it.">
          {(id) => (
            <Select id={id} value={s.draft!.metadataLanguage} onChange={(e) => s.update({ metadataLanguage: e.target.value })}>
              {languages.map(([v, l]) => (
                <option key={v} value={v}>
                  {l}
                </option>
              ))}
            </Select>
          )}
        </Field>
      </Card>
      <div className="mt-6">
        <RestartServer />
      </div>
      <SaveBar dirty={s.dirty} saving={s.saving} error={s.error} savedAt={s.savedAt} onSave={s.save} onReset={s.reset} />
    </>
  );
}

export function NetworkSettings() {
  const s = useSectionDraft("network");
  const info = useQuery(systemInfoQuery);
  if (!s.draft) return <Spinner />;
  return (
    <>
      <div className="space-y-6">
        {info.data && (
          <Alert tone="info">
            This browser is connected as <strong className="text-text">{info.data.networkClass === "local" ? "Local" : "Remote"}</strong>.
          </Alert>
        )}
        <Card title="Local network" description="Devices on these networks connect directly and always get full quality. Everything else, including Tailscale, is treated as remote.">
          <Field label="LAN subnets" help="CIDR notation, e.g. 10.1.1.0/24. Tailscale addresses (100.64.0.0/10) are always remote.">
            {() => <StringListEditor values={s.draft!.lanSubnets ?? []} onChange={(v) => s.update({ lanSubnets: v })} placeholder="10.1.1.0/24" addLabel="Add subnet" />}
          </Field>
          <Field label="Local server address" help="The address local apps use, e.g. http://10.1.1.10:32500.">
            {(id) => <Input id={id} placeholder="http://10.1.1.10:32500" value={s.draft!.lanUrl ?? ""} onChange={(e) => s.update({ lanUrl: e.target.value })} />}
          </Field>
          <Toggle
            label="Advertise on the local network (Bonjour)"
            help="Lets iPhone, iPad and Apple TV apps find this server automatically."
            checked={!!s.draft.bonjourEnabled}
            onChange={(v) => s.update({ bonjourEnabled: v })}
          />
        </Card>
      </div>
      <SaveBar dirty={s.dirty} saving={s.saving} error={s.error} savedAt={s.savedAt} onSave={s.save} onReset={s.reset} />
    </>
  );
}

export function RemoteAccessSettings() {
  const s = useSectionDraft("remoteAccess");
  if (!s.draft) return <Spinner />;
  const d = s.draft;
  return (
    <>
      <div className="space-y-6">
        <Card title="Tailscale" description="Remote devices reach this server through your tailnet. Local devices never use Tailscale.">
          <Toggle label="Allow remote streaming" checked={!!d.enabled} onChange={(v) => s.update({ enabled: v })} />
          <Field label="Remote server address" help={<>The HTTPS address from <code className="text-text">tailscale serve</code>, e.g. https://tower.your-tailnet.ts.net.</>}>
            {(id) => <Input id={id} placeholder="https://tower.your-tailnet.ts.net" value={d.remoteUrl ?? ""} onChange={(e) => s.update({ remoteUrl: e.target.value })} />}
          </Field>
        </Card>
        <Card
          title="Bandwidth"
          description="Remote quality is chosen automatically: the lowest of the app’s quality setting, the user’s limit, the measured connection speed, and this server’s fair share of upload bandwidth."
        >
          <Field label="Internet upload speed" help="Your server’s upload speed. Shared fairly between remote streams. Leave empty if unknown.">
            {(id) => <MbpsInput id={id} kbps={d.uploadSpeedKbps} onChange={(v) => s.update({ uploadSpeedKbps: v })} placeholder="e.g. 40" />}
          </Field>
          <div className="grid gap-5 sm:grid-cols-2">
            <Field label="Total remote limit" help="Across all remote streams. Empty = no limit.">
              {(id) => <MbpsInput id={id} kbps={d.totalRemoteLimitKbps} onChange={(v) => s.update({ totalRemoteLimitKbps: v })} placeholder="Unlimited" />}
            </Field>
            <Field label="Per-stream remote limit" help="Empty = no limit.">
              {(id) => <MbpsInput id={id} kbps={d.perStreamRemoteLimitKbps} onChange={(v) => s.update({ perStreamRemoteLimitKbps: v })} placeholder="Unlimited" />}
            </Field>
          </div>
          <Field label="Minimum per-stream bandwidth" help="Floor when many remote streams share the upload. Below this, new streams start at the lowest quality.">
            {(id) => <MbpsInput id={id} kbps={d.minStreamKbps} onChange={(v) => s.update({ minStreamKbps: v })} />}
          </Field>
        </Card>
      </div>
      <SaveBar dirty={s.dirty} saving={s.saving} error={s.error} savedAt={s.savedAt} onSave={s.save} onReset={s.reset} />
    </>
  );
}

function SecretField({ label, help, isSet, value, onChange }: { label: string; help: React.ReactNode; isSet: boolean; value: string | undefined; onChange: (v: string | undefined) => void }) {
  const [editing, setEditing] = useState(false);
  return (
    <Field label={label} help={help}>
      {(id) =>
        editing || !isSet ? (
          <Input id={id} type="password" autoComplete="off" placeholder="Paste API key" value={value ?? ""} onChange={(e) => onChange(e.target.value)} />
        ) : (
          <div className="flex items-center gap-3">
            {value === "" ? (
              <span className="rounded bg-danger/15 px-2 py-1 text-xs font-medium text-danger">Removed when you save</span>
            ) : (
              <span className="rounded bg-success/15 px-2 py-1 text-xs font-medium text-success">Configured</span>
            )}
            <Button size="sm" variant="ghost" onClick={() => setEditing(true)}>
              Replace
            </Button>
            <Button size="sm" variant="ghost" onClick={() => onChange("")}>
              Remove
            </Button>
          </div>
        )
      }
    </Field>
  );
}

export function MetadataSettings() {
  // Secrets are write-only: the server returns *Set flags; the draft carries new values to send.
  const s = useSectionDraft("metadata", (d) => {
    const x = d as typeof d & { tmdbApiKey?: string; fanartApiKey?: string; openSubtitlesApiKey?: string; omdbApiKey?: string; openSubtitlesPassword?: string };
    return {
      tmdbApiKey: x.tmdbApiKey,
      fanartApiKey: x.fanartApiKey,
      openSubtitlesApiKey: x.openSubtitlesApiKey,
      openSubtitlesUsername: d.openSubtitlesUsername,
      openSubtitlesPassword: x.openSubtitlesPassword,
      omdbApiKey: x.omdbApiKey,
      omdbDailyLimit: d.omdbDailyLimit,
      animeEpisodeOrdering: d.animeEpisodeOrdering,
    };
  });
  if (!s.draft) return <Spinner />;
  const d = s.draft as typeof s.draft & { tmdbApiKey?: string; fanartApiKey?: string; openSubtitlesApiKey?: string; omdbApiKey?: string; openSubtitlesPassword?: string };
  const set = (k: "tmdbApiKey" | "fanartApiKey" | "openSubtitlesApiKey" | "omdbApiKey" | "openSubtitlesPassword") => (v: string | undefined) => s.update({ [k]: v } as Partial<typeof d>);
  return (
    <>
      <div className="space-y-6">
        <Card title="Providers" description="Movies, TV and anime use TMDB, with AniList for anime. Music uses MusicBrainz (no key needed).">
          <SecretField label="TMDB API key" help={<>Free at themoviedb.org → Settings → API. Required for movie and TV metadata.</>} isSet={!!d.tmdbApiKeySet} value={d.tmdbApiKey} onChange={set("tmdbApiKey")} />
          <SecretField label="Fanart.tv API key" help="Optional. Adds logos, clear art and artist images." isSet={!!d.fanartApiKeySet} value={d.fanartApiKey} onChange={set("fanartApiKey")} />
          <SecretField
            label="OpenSubtitles API key"
            help="Optional. Enables “Find subtitles” in the player. Get one at opensubtitles.com → API consumers."
            isSet={!!d.openSubtitlesApiKeySet}
            value={d.openSubtitlesApiKey}
            onChange={set("openSubtitlesApiKey")}
          />
          {(d.openSubtitlesApiKeySet || d.openSubtitlesApiKey) && (
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="OpenSubtitles username" help="Optional. Signing in allows more downloads a day.">
                {(id) => <Input id={id} autoComplete="off" value={d.openSubtitlesUsername ?? ""} onChange={(e) => s.update({ openSubtitlesUsername: e.target.value })} />}
              </Field>
              <SecretField label="OpenSubtitles password" help="Stored on the server and only sent to OpenSubtitles." isSet={!!d.openSubtitlesPasswordSet} value={d.openSubtitlesPassword} onChange={set("openSubtitlesPassword")} />
            </div>
          )}
        </Card>
        <Card title="Ratings" description="IMDb, Rotten Tomatoes and Metacritic scores for movies and shows, from OMDb.">
          <SecretField
            label="OMDb API key"
            help="Optional. Free keys (omdbapi.com) allow 1,000 lookups a day; a large library fills in over a few days."
            isSet={!!d.omdbApiKeySet}
            value={d.omdbApiKey}
            onChange={set("omdbApiKey")}
          />
          <Field label="Daily lookup limit" help="Keep below your key's daily allowance. Ratings refresh every 30 days.">
            {(id) => <Input id={id} type="number" min={1} value={d.omdbDailyLimit ?? 950} onChange={(e) => s.update({ omdbDailyLimit: Number(e.target.value) })} />}
          </Field>
        </Card>
        <Card title="Anime">
          <Field label="Default episode numbering" help="Seasonal matches TVDB-style seasons. Absolute numbers episodes 1…n. Each show can override this.">
            {(id) => (
              <Select id={id} value={d.animeEpisodeOrdering} onChange={(e) => s.update({ animeEpisodeOrdering: e.target.value as "seasonal" | "absolute" })}>
                <option value="seasonal">Seasonal</option>
                <option value="absolute">Absolute</option>
              </Select>
            )}
          </Field>
        </Card>
      </div>
      <SaveBar dirty={s.dirty} saving={s.saving} error={s.error} savedAt={s.savedAt} onSave={s.save} onReset={s.reset} />
    </>
  );
}

