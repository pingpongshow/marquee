import { useQuery } from "@tanstack/react-query";
import { Bell, BellRing } from "lucide-react";
import { useState } from "react";
import { meQuery, settingsQuery } from "@/api/queries";
import { Alert, Button, Dialog, Field, Input, Select } from "@/components/ui";
import { formatSize } from "./api";
import { type VodFollow, type VodSeriesDetail, useFollowVodSeries, useUnfollowVodSeries, vodFollowsQuery } from "./vodApi";

// Following a show on demand: its new episodes are saved to the library automatically,
// within a size limit and the person's recording quota.

/** "Following · 3 episodes · 4.2 GB", then why it's stopped, if it has. */
export function followSummary(f: VodFollow) {
  return [
    "Following",
    `${f.episodesSaved} ${f.episodesSaved === 1 ? "episode" : "episodes"}`,
    f.usedBytes ? formatSize(f.usedBytes) : "",
    f.episodesQueued ? `${f.episodesQueued} saving` : "",
  ]
    .filter(Boolean)
    .join(" · ");
}

/** A show page's Follow button, or (followed) its summary with Change and Unfollow. Shown
 * only when the server says the person may follow (show.following is set). */
export function FollowControls({ show }: { show: VodSeriesDetail }) {
  const follows = useQuery({ ...vodFollowsQuery(), enabled: show.following != null });
  const unfollow = useUnfollowVodSeries();
  const [open, setOpen] = useState(false);
  if (show.following == null) return null;
  const f = follows.data?.find((x) => x.seriesId === show.id);
  return (
    <>
      {show.following && f ? (
        <div className="flex flex-wrap items-center gap-2" data-testid="vod-follow">
          <span className="inline-flex items-center gap-1.5 rounded-md bg-accent/15 px-2.5 py-1.5 text-sm text-accent">
            <BellRing className="size-4" aria-hidden /> {followSummary(f)}
          </span>
          <Button size="sm" variant="ghost" onClick={() => setOpen(true)}>
            Change
          </Button>
          <Button size="sm" variant="ghost" disabled={unfollow.isPending} onClick={() => unfollow.mutate({ seriesId: show.id })}>
            Unfollow
          </Button>
          {f.statusText && <span className={f.status === "following" ? "text-sm text-muted" : "text-sm text-amber-500"}>{f.statusText}</span>}
        </div>
      ) : (
        <Button
          variant="ghost"
          onClick={() => setOpen(true)}
          disabled={show.following}
          title="Save new episodes to the library automatically as they appear"
        >
          <Bell className="size-4" /> Follow
        </Button>
      )}
      {open && <FollowDialog show={show} follow={f} onClose={() => setOpen(false)} />}
    </>
  );
}

function FollowDialog({ show, follow, onClose }: { show: VodSeriesDetail; follow?: VodFollow; onClose: () => void }) {
  const me = useQuery(meQuery);
  // Admins see the default size limit; others get it by leaving the field as it is.
  const settings = useQuery({ ...settingsQuery, enabled: !!me.data?.isAdmin && !follow });
  const def = settings.data?.vodSave?.followDefaultMaxGb;
  const [maxGb, setMaxGb] = useState<string | null>(follow ? String(follow.maxGb ?? 0) : null);
  const [existing, setExisting] = useState(false);
  const dests = show.saveDestinations ?? [];
  const [dest, setDest] = useState(show.saveDefault === false && dests[0] ? dests[0].id : "");
  const save = useFollowVodSeries();
  const shown = maxGb ?? (def != null ? String(def) : "");
  const submit = () => {
    const n = shown.trim() === "" ? undefined : Math.max(0, Number(shown) || 0);
    save.mutate(
      {
        seriesId: show.id,
        maxGb: n,
        includeExisting: follow ? undefined : existing || undefined,
        destination: dests.length > 0 && !follow ? dest : undefined,
      },
      { onSuccess: onClose },
    );
  };
  return (
    <Dialog
      open
      onClose={onClose}
      title={follow ? `Following ${show.title}` : `Follow ${show.title}`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" onClick={submit} disabled={save.isPending}>
            {follow ? "Save" : "Follow"}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <p className="text-sm text-muted">
          New episodes are saved to the library as they appear on the source. They count towards your recording space with
          your recordings; saving stops while it&apos;s full.
        </p>
        <Field label="Size limit (GB)" help="Saving stops once this show's saved episodes reach it. 0 = no limit.">
          {(id) => (
            <Input
              id={id}
              type="number"
              min={0}
              step="any"
              className="max-w-40"
              placeholder="Default"
              value={shown}
              onChange={(e) => setMaxGb(e.target.value)}
            />
          )}
        </Field>
        {!follow && (
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={existing} onChange={(e) => setExisting(e.target.checked)} className="size-4 accent-[var(--color-accent)]" />
            Also save episodes already there
          </label>
        )}
        {!follow && dests.length > 0 && (
          <Field label="Save to">
            {(id) => (
              <Select id={id} value={dest} onChange={(e) => setDest(e.target.value)}>
                {show.saveDefault !== false && <option value="">Shows folder</option>}
                {dests.map((d) => (
                  <option key={d.id} value={d.id}>
                    {d.name}
                  </option>
                ))}
              </Select>
            )}
          </Field>
        )}
        {save.isError && <Alert tone="error">{save.error.message}</Alert>}
      </div>
    </Dialog>
  );
}
