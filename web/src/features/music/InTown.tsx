import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { ChevronRight, ExternalLink, MapPin, Music2, Ticket } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { itemQuery, meQuery, systemInfoQuery } from "@/api/queries";
import type { components } from "@/api/schema.gen";
import type { ItemDetail } from "@/api/types";
import { Alert, Badge, Button, Card, Field, Input, Select, Spinner } from "@/components/ui";
import { Poster } from "../browse/Poster";
import {
  formatDistance,
  formatShowDate,
  formatShowTime,
  guessCountry,
  inTownCountries,
  nearLine,
  showsHeadline,
  unitForCountry,
} from "./inTownFormat";

type InTownShow = components["schemas"]["InTownShow"];
type InTownArtist = components["schemas"]["InTownArtist"];
type InTownLocation = components["schemas"]["InTownLocation"];
type InTownAttribution = components["schemas"]["InTownAttribution"];

// Under a key of its own: a heart or a rating changes the order, and that's fine a few minutes on.
export const inTownQuery = queryOptions({
  queryKey: ["in-town", "page"],
  queryFn: () => unwrap(api.GET("/music/in-town")),
  staleTime: 5 * 60_000,
});

const itemShowsQuery = (itemId: number) =>
  queryOptions({
    queryKey: ["in-town", "item", itemId],
    queryFn: () => unwrap(api.GET("/items/{itemId}/shows", { params: { path: { itemId } } })),
    staleTime: 5 * 60_000,
  });

const locationQuery = queryOptions({
  queryKey: ["in-town", "location"],
  queryFn: () => unwrap(api.GET("/me/location")),
});

/** Whether In Town is for this person: an admin has turned it on, and they aren't a kid. Nothing
 *  about it shows otherwise. */
export function useInTownOn() {
  const info = useQuery(systemInfoQuery);
  const me = useQuery(meQuery);
  return !!info.data?.inTown && !!me.data && !me.data.restrictions.isKid;
}

/** Banner on an artist's page (MUSIC-27): nothing while it loads, when there are no shows or when
 *  it's off, so it never moves the page about or holds it up. */
export function ArtistShowsBanner({ item }: { item: ItemDetail }) {
  const on = useInTownOn();
  const shows = useQuery({ ...itemShowsQuery(item.id), enabled: on && item.type === "artist" });
  const list = shows.data?.shows ?? [];
  if (!on || !shows.data?.enabled || !list.length) return null;
  const next = list[0]!;
  const near = nearLine(shows.data.nearCount);
  return (
    <Link
      to="/in-town"
      search={{ artist: item.id }}
      data-testid="artist-shows-banner"
      className="mt-6 flex items-center gap-3 rounded-lg border border-border bg-surface px-4 py-2.5 text-sm hover:bg-surface-2"
    >
      <Ticket className="size-5 shrink-0 text-accent" aria-hidden />
      <span className="min-w-0 flex-1">
        <span className="font-medium">{showsHeadline(item.title, list.length)}</span>
        <span className="text-muted">
          {" "}
          · Next: {formatShowDate(next.localDate)}, {next.city}
          {near && ` · ${near}`}
        </span>
      </span>
      <ChevronRight className="size-4 shrink-0 text-faint" aria-hidden />
    </Link>
  );
}

function Attribution({ a }: { a: InTownAttribution }) {
  return (
    <p className="text-xs text-faint">
      <a href={a.url} target="_blank" rel="noreferrer" className="underline hover:text-muted">
        {a.text}
      </a>
      . Tickets are sold on {a.name}'s site.
    </p>
  );
}

/** One show: when and where at the venue's own time, why it's here, and a link to its tickets. */
function ShowRow({ show, attribution, showArtist = true }: { show: InTownShow; attribution: string; showArtist?: boolean }) {
  const when = [formatShowDate(show.localDate), formatShowTime(show.localTime)].filter(Boolean).join(", ");
  const place = [show.venueName, show.city, show.region].filter(Boolean).join(", ");
  const distance = formatDistance(show.distance, show.distanceUnit);
  const headliner = show.support ? show.lineup[0] : undefined;
  return (
    <li className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3" data-testid="in-town-show">
      {showArtist &&
        (show.artist ? (
          <Link to="/item/$itemId" params={{ itemId: String(show.artist.id) }} className="w-12 shrink-0">
            <Poster item={show.artist} shape="square" width={96} compact />
          </Link>
        ) : (
          <span className="flex size-12 shrink-0 items-center justify-center rounded bg-surface-2 text-muted" aria-hidden>
            <Music2 className="size-5" />
          </span>
        ))}
      <div className="min-w-0 flex-1 basis-56">
        {showArtist && (
          <div className="font-medium">
            {show.artistItemId ? (
              <Link to="/item/$itemId" params={{ itemId: String(show.artistItemId) }} className="hover:underline">
                {show.artistName}
              </Link>
            ) : (
              show.artistName
            )}
            {headliner && <span className="font-normal text-muted"> supporting {headliner}</span>}
          </div>
        )}
        {!showArtist && headliner && <div className="text-xs text-muted">Supporting {headliner}</div>}
        <div className="text-sm">
          {when}
          {show.status !== "scheduled" && (
            <span className="ml-2">
              <Badge>{show.status === "postponed" ? "Postponed" : "Rescheduled"}</Badge>
            </span>
          )}
        </div>
        <div className="text-sm text-muted">
          {place}
          {distance && ` · ${distance}`}
        </div>
        {show.reason && <div className="mt-0.5 text-xs text-accent">{show.reason}</div>}
      </div>
      <a
        href={show.ticketsUrl}
        target="_blank"
        rel="noreferrer"
        className="inline-flex shrink-0 items-center gap-1.5 rounded-lg bg-surface-2 px-3 py-1.5 text-sm hover:bg-surface-3"
      >
        Tickets on {attribution} <ExternalLink className="size-3.5" aria-hidden />
      </a>
    </li>
  );
}

function ArtistCard({ row, attribution }: { row: InTownArtist; attribution: string }) {
  const more = row.showCount - row.shows.length;
  return (
    <li className="overflow-hidden rounded-lg border border-border bg-surface" data-testid="in-town-artist">
      <div className="flex items-center gap-4 border-b border-border px-4 py-3">
        <Link to="/item/$itemId" params={{ itemId: String(row.artist.id) }} className="w-14 shrink-0">
          <Poster item={row.artist} shape="square" width={112} compact />
        </Link>
        <div className="min-w-0 flex-1">
          <Link to="/item/$itemId" params={{ itemId: String(row.artist.id) }} className="block truncate font-semibold hover:underline">
            {row.artist.title}
          </Link>
          <div className="text-sm text-muted">
            {row.showCount} upcoming {row.showCount === 1 ? "show" : "shows"}
            {row.nearCount > 0 && ` · ${nearLine(row.nearCount)}`}
          </div>
          {row.reason && <div className="text-xs text-accent">{row.reason}</div>}
        </div>
      </div>
      <ul className="divide-y divide-border">
        {row.shows.map((s) => (
          <ShowRow key={s.id} show={s} attribution={attribution} showArtist={false} />
        ))}
      </ul>
      {more > 0 && (
        <Link to="/in-town" search={{ artist: row.artist.id }} className="flex items-center gap-1 border-t border-border px-4 py-2 text-sm text-muted hover:text-text">
          {more} more <ChevronRight className="size-4" aria-hidden />
        </Link>
      )}
    </li>
  );
}

/** Where someone is, for shows near them: a postcode or a city, never GPS. */
export function LocationForm({ onSaved }: { onSaved?: () => void }) {
  const qc = useQueryClient();
  const current = useQuery(locationQuery);
  // Unset fields follow what's saved until they're edited. Typing in the postcode or the city
  // clears the other, as only one is sent.
  const [edit, setEdit] = useState<Partial<InTownLocation>>({});
  const saved = current.data?.location;
  const country = edit.countryCode ?? saved?.countryCode ?? guessCountry();
  const unit = edit.unit ?? saved?.unit ?? unitForCountry(country);
  const postalCode = edit.postalCode ?? saved?.postalCode ?? "";
  const city = edit.city ?? saved?.city ?? "";
  const distance = edit.distance ?? saved?.distance ?? (unit === "km" ? 80 : 50);
  const done = () => {
    setEdit({});
    qc.invalidateQueries({ queryKey: ["in-town"] });
    onSaved?.();
  };
  const save = useMutation({
    mutationFn: () =>
      unwrap(
        api.PUT("/me/location", {
          body: { countryCode: country, postalCode: postalCode.trim() || undefined, city: city.trim() || undefined, distance, unit },
        }),
      ),
    onSuccess: done,
  });
  const remove = useMutation({
    mutationFn: () => unwrap(api.DELETE("/me/location")),
    onSuccess: done,
  });
  const names = new Intl.DisplayNames(undefined, { type: "region" });
  return (
    <div className="space-y-4" data-testid="in-town-location">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Postcode or ZIP">
          {(id) => <Input id={id} maxLength={12} autoComplete="off" value={postalCode} onChange={(e) => setEdit((x) => ({ ...x, postalCode: e.target.value, ...(e.target.value && { city: "" }) }))} />}
        </Field>
        <Field label="Or city">
          {(id) => <Input id={id} maxLength={80} autoComplete="off" value={city} onChange={(e) => setEdit((x) => ({ ...x, city: e.target.value, ...(e.target.value && { postalCode: "" }) }))} />}
        </Field>
        <Field label="Country">
          {(id) => (
            <Select
              id={id}
              value={country}
              onChange={(e) => setEdit((x) => ({ ...x, countryCode: e.target.value }))}
            >
              {inTownCountries.map((c) => (
                <option key={c} value={c}>
                  {names.of(c)}
                </option>
              ))}
              {!inTownCountries.includes(country) && <option value={country}>{country}</option>}
            </Select>
          )}
        </Field>
        <Field label="How far you'd go">
          {(id) => (
            <div className="flex gap-2">
              <Input
                id={id}
                type="number"
                min={1}
                max={500}
                value={distance}
                onChange={(e) => setEdit((x) => ({ ...x, distance: Number(e.target.value) }))}
              />
              <Select aria-label="Unit" value={unit} onChange={(e) => setEdit((x) => ({ ...x, unit: e.target.value as "miles" | "km" }))}>
                <option value="miles">miles</option>
                <option value="km">km</option>
              </Select>
            </div>
          )}
        </Field>
      </div>
      <p className="text-sm text-muted">
        To find shows, this server sends your postcode or city to Ticketmaster, without your name. It never uses GPS, and only you can see what you enter here. In the UK,
        Ireland, Canada and the Netherlands only the first part of a postcode is kept. If a postcode finds nothing, try your city.
      </p>
      {save.error && <Alert tone="error">{save.error.message}</Alert>}
      {remove.error && <Alert tone="error">{remove.error.message}</Alert>}
      <div className="flex gap-2">
        <Button variant="primary" size="sm" loading={save.isPending} disabled={!postalCode.trim() && !city.trim()} onClick={() => save.mutate()}>
          Save
        </Button>
        {saved && (
          <Button size="sm" variant="ghost" loading={remove.isPending} onClick={() => remove.mutate()}>
            Remove
          </Button>
        )}
      </div>
    </div>
  );
}

/** Account → In Town. */
export function LocationCard() {
  const on = useInTownOn();
  if (!on) return null;
  return (
    <Card title="In Town" description="Where you are, for concerts near you.">
      <LocationForm />
    </Card>
  );
}

/** The artist the page was opened for, with all their shows. */
function FocusedArtist({ itemId, attribution }: { itemId: number; attribution: string }) {
  const navigate = useNavigate();
  const shows = useQuery(itemShowsQuery(itemId));
  const artist = useQuery(itemQuery(itemId));
  if (shows.isPending) return <Spinner />;
  if (shows.isError) return <Alert tone="error">{shows.error.message}</Alert>;
  const list = shows.data.shows;
  return (
    <section aria-label="This artist's shows" data-testid="in-town-focus">
      <div className="mb-3 flex items-baseline gap-3">
        <h2 className="text-lg font-semibold">{artist.data ? showsHeadline(artist.data.title, list.length) : "Upcoming shows"}</h2>
        <button type="button" className="ml-auto text-sm text-muted hover:text-text" onClick={() => navigate({ to: "/in-town", search: {} })}>
          All artists
        </button>
      </div>
      {list.length ? (
        <ul className="divide-y divide-border overflow-hidden rounded-lg border border-border bg-surface">
          {list.map((s) => (
            <ShowRow key={s.id} show={s} attribution={attribution} showArtist={false} />
          ))}
        </ul>
      ) : (
        <p className="text-muted">No upcoming shows found.</p>
      )}
    </section>
  );
}

/** In Town (MUSIC-27): artists in your libraries on tour, then shows near you. */
export function InTownPage() {
  const { artist } = useSearch({ from: "/in-town" });
  const page = useQuery(inTownQuery);
  const me = useQuery(meQuery);
  const [editing, setEditing] = useState(false);
  if (page.isPending) return <Spinner label="Looking for shows" />;
  if (page.isError) {
    return (
      <div className="p-6 lg:p-8">
        <Alert tone="error">{page.error.message}</Alert>
      </div>
    );
  }
  const d = page.data;
  if (!d.enabled) {
    return (
      <div className="p-6 lg:p-8">
        <h1 className="mb-4 text-2xl font-bold">In Town</h1>
        <p className="text-muted">{me.data?.isAdmin ? "In Town isn't turned on. You can turn it on in Settings → Music." : "In Town isn't available."}</p>
      </div>
    );
  }
  const name = d.attribution.name;
  const where = d.location ? d.location.city || d.location.postalCode : "";
  return (
    <div className="space-y-10 p-6 lg:p-8">
      <h1 className="text-2xl font-bold">In Town</h1>
      {artist !== undefined && <FocusedArtist itemId={artist} attribution={name} />}

      <section aria-label="Your artists on tour" data-testid="in-town-artists">
        <h2 className="mb-3 text-lg font-semibold">Your artists on tour</h2>
        {d.artists.length ? (
          <ul className="grid gap-4 lg:grid-cols-2">
            {d.artists.map((a) => (
              <ArtistCard key={a.artist.id} row={a} attribution={name} />
            ))}
          </ul>
        ) : (
          <p className="text-muted">No shows found yet for artists in your libraries. The server looks them up a few at a time, so check back later.</p>
        )}
      </section>

      <section aria-label="Shows near you" data-testid="in-town-near">
        <div className="mb-3 flex flex-wrap items-baseline gap-3">
          <h2 className="text-lg font-semibold">{where ? `Near ${where}` : "Near you"}</h2>
          {d.location && (
            <span className="text-sm text-muted">
              Within {d.location.distance} {d.location.unit}
            </span>
          )}
          {d.location && (
            <button type="button" className="ml-auto inline-flex items-center gap-1 text-sm text-muted hover:text-text" onClick={() => setEditing((v) => !v)}>
              <MapPin className="size-4" aria-hidden /> Change
            </button>
          )}
        </div>
        {(d.needsLocation || editing) && (
          <div className="mb-4 rounded-lg border border-border bg-surface p-5">
            {d.needsLocation && <p className="mb-4 font-medium">Where are you? Set a location to see shows near you.</p>}
            <LocationForm onSaved={() => setEditing(false)} />
          </div>
        )}
        {d.nearError && (
          <div className="mb-4">
            <Alert tone="info">{d.nearError}</Alert>
          </div>
        )}
        {!d.needsLocation && d.locationFound === false && !d.near.length && (
          <p className="text-muted">Ticketmaster doesn't recognise that location. Try your nearest city instead.</p>
        )}
        {d.near.length > 0 && (
          <ul className="divide-y divide-border overflow-hidden rounded-lg border border-border bg-surface">
            {d.near.map((s) => (
              <ShowRow key={s.id} show={s} attribution={name} />
            ))}
          </ul>
        )}
        {!d.needsLocation && d.locationFound !== false && !d.near.length && !d.nearError && <p className="text-muted">No shows found near you right now.</p>}
      </section>

      <Attribution a={d.attribution} />
    </div>
  );
}
