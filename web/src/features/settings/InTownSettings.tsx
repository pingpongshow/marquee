import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { settingsQuery, useUpdateSettings } from "@/api/queries";
import { Alert, Button, Card, Field, Input, Toggle } from "@/components/ui";

/** Settings → Music → In Town (MUSIC-27): the admin's own Ticketmaster key. Saved on its own. */
export function InTownSettings() {
  const settings = useQuery(settingsQuery);
  const save = useUpdateSettings();
  const cur = settings.data?.inTown;
  const [key, setKey] = useState("");
  const [enabled, setEnabled] = useState<boolean | null>(null);
  const [budget, setBudget] = useState<number | null>(null);
  const on = enabled ?? !!cur?.enabled;
  const keySet = !!cur?.ticketmasterKeySet;
  const dailyBudget = budget ?? cur?.dailyBudget ?? 2000;
  const status = useQuery({
    queryKey: ["in-town", "status"],
    queryFn: () => unwrap(api.GET("/music/in-town/status")),
    enabled: !!cur?.enabled,
    refetchInterval: (q) => (q.state.data?.running ? 5000 : 60_000),
  });
  const st = status.data;
  const dirty = key.trim() !== "" || (enabled !== null && enabled !== !!cur?.enabled) || (budget !== null && budget !== cur?.dailyBudget);
  const clean = () => {
    setKey("");
    setEnabled(null);
    setBudget(null);
    status.refetch();
  };
  return (
    <Card
      title="In Town"
      description={
        <>
          Concerts for the people on this server: artists in their libraries on tour, and shows near them. Off by default. It needs a free key of your own: create an account on the{" "}
          <a className="underline" href="https://developer-acct.ticketmaster.com/user/register" target="_blank" rel="noreferrer">
            Ticketmaster Developer Portal
          </a>{" "}
          and enter its Consumer Key here.
        </>
      }
    >
      <Toggle
        label="Turn on In Town"
        help="Sends Ticketmaster only artist names and ids, and the postcode or city people enter, never who they are. Shows are kept in a separate cache file, left out of backups, and turning this off deletes it."
        checked={on}
        onChange={setEnabled}
      />
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Consumer Key">
          {(id) => <Input id={id} type="password" autoComplete="new-password" value={key} placeholder={keySet ? "••••••••" : ""} onChange={(e) => setKey(e.target.value)} />}
        </Field>
        <Field label="Background calls a day" help="A free key allows 5,000 a day, so this leaves room for what people do.">
          {(id) => <Input id={id} type="number" min={100} max={4000} value={dailyBudget} onChange={(e) => setBudget(Number(e.target.value))} />}
        </Field>
      </div>
      {cur?.enabled && st && (
        <div className="space-y-1 rounded-lg bg-surface-2 p-4 text-sm" data-testid="in-town-status">
          <div>
            {st.artistsChecked.toLocaleString()} of {st.artistsTotal.toLocaleString()} artists checked, {st.artistsMatched.toLocaleString()} matched · {st.showsCached.toLocaleString()} shows stored
            {st.running ? " · Looking up now" : ""}
          </div>
          <div className="text-muted">
            {st.lastSyncAt ? `Last looked up ${new Date(st.lastSyncAt).toLocaleString()}` : "Not looked up yet"} · {st.callsToday.toLocaleString()} calls today
            {st.quotaRemaining !== undefined && ` · ${st.quotaRemaining.toLocaleString()} left on the key`}
          </div>
          {st.backoffUntil && <div className="text-muted">Ticketmaster asked us to wait until {new Date(st.backoffUntil).toLocaleString()}.</div>}
          {st.lastError && <div className="text-danger">{st.lastError}</div>}
        </div>
      )}
      {save.error && <Alert tone="error">{save.error.message}</Alert>}
      <div className="flex gap-2">
        <Button
          variant="primary"
          size="sm"
          disabled={!dirty}
          loading={save.isPending}
          onClick={() => save.mutate({ inTown: { enabled: on, dailyBudget, ...(key.trim() && { ticketmasterKey: key.trim() }) } }, { onSuccess: clean })}
        >
          Save
        </Button>
        {keySet && !cur?.enabled && (
          <Button size="sm" variant="ghost" loading={save.isPending} onClick={() => save.mutate({ inTown: { ticketmasterKey: "" } }, { onSuccess: clean })}>
            Remove key
          </Button>
        )}
      </div>
    </Card>
  );
}
