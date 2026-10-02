import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { clsx } from "clsx";
import { Search, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { api, unwrap } from "@/api/client";
import {
  Alert,
  Badge,
  Button,
  Dialog,
  Input,
  Select,
  Spinner,
} from "@/components/ui";
import {
  availabilityLabel,
  type DiscoverItem,
  myRequestsQuery,
  requestsStatusQuery,
  statusLabel,
  useRequestAction,
} from "./api";
import { RequestDialog } from "./RequestDialog";

const categories = [
  { id: "trending", label: "Trending" },
  { id: "movies", label: "Movies" },
  { id: "tv", label: "TV Shows" },
  { id: "upcoming", label: "Upcoming movies" },
  { id: "upcoming_tv", label: "Upcoming shows" },
] as const;

const sorts = [
  { id: "popular", label: "Popular" },
  { id: "rating", label: "Top rated" },
  { id: "newest", label: "Newest" },
  { id: "title", label: "A–Z" },
] as const;
type Sort = (typeof sorts)[number]["id"];

/** Discover (REQ-1, REQ-2): find titles that aren't here and ask for them. */
export function DiscoverPage() {
  const status = useQuery(requestsStatusQuery);
  const [q, setQ] = useState("");
  const [term, setTerm] = useState("");
  const [category, setCategory] =
    useState<(typeof categories)[number]["id"]>("trending");
  const [picked, setPicked] = useState<DiscoverItem | null>(null);
  const [network, setNetwork] = useState<number | null>(null);
  const [studio, setStudio] = useState<number | null>(null);
  const [genre, setGenre] = useState<number | null>(null);
  const [sort, setSort] = useState<Sort>("popular");
  const browsing = category === "movies" || category === "tv";
  const filters = useQuery({
    queryKey: ["requests", "filters"],
    queryFn: () => unwrap(api.GET("/requests/discover/filters")),
    staleTime: 3_600_000,
    enabled: !!status.data?.enabled && !!status.data?.canRequest,
  });
  const pickCategory = (c: (typeof categories)[number]["id"]) => {
    setCategory(c);
    setNetwork(null);
    setStudio(null);
    setGenre(null);
    setSort("popular");
  };
  const [viewing, setViewing] = useState<DiscoverItem | null>(null);
  useEffect(() => {
    const t = setTimeout(() => setTerm(q.trim()), 350);
    return () => clearTimeout(t);
  }, [q]);
  // Pages load as you scroll (Seerr has thousands of titles per list).
  const results = useInfiniteQuery({
    queryKey: [
      "requests",
      "discover",
      term,
      category,
      network,
      studio,
      genre,
      sort,
    ],
    queryFn: ({ pageParam }) =>
      term
        ? unwrap(
            api.GET("/requests/search", {
              params: { query: { q: term, page: pageParam } },
            }),
          )
        : unwrap(
            api.GET("/requests/discover", {
              params: {
                query: {
                  category,
                  page: pageParam,
                  ...(browsing
                    ? {
                        network: network ?? undefined,
                        studio: studio ?? undefined,
                        genre: genre ?? undefined,
                        sort,
                      }
                    : {}),
                },
              },
            }),
          ),
    initialPageParam: 1,
    getNextPageParam: (last) =>
      last.page < last.totalPages ? last.page + 1 : undefined,
    enabled: !!status.data?.enabled && !!status.data?.canRequest,
  });
  const list = useMemo(() => {
    // Pages can overlap as lists shift; show each title once.
    const seen = new Set<string>();
    return (results.data?.pages ?? [])
      .flatMap((p) => p.results)
      .filter((it) => {
        const k = `${it.mediaType}-${it.tmdbId}`;
        if (seen.has(k)) return false;
        seen.add(k);
        return true;
      });
  }, [results.data]);
  const more = useRef<HTMLDivElement>(null);
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = results;
  useEffect(() => {
    const el = more.current;
    if (!el || !hasNextPage) return;
    const io = new IntersectionObserver(
      (e) => {
        if (e[0]?.isIntersecting && !isFetchingNextPage) void fetchNextPage();
      },
      { rootMargin: "600px" },
    );
    io.observe(el);
    return () => io.disconnect();
  }, [hasNextPage, isFetchingNextPage, fetchNextPage, list.length]);

  if (status.isPending) return <Spinner />;
  if (!status.data?.enabled)
    return (
      <Alert>
        Requests aren't set up on this server yet. An admin can connect Seerr in
        Settings → Requests.
      </Alert>
    );
  if (!status.data.canRequest)
    return <Alert>Ask an admin to let you request movies and shows.</Alert>;

  return (
    <div className="space-y-6 p-4 lg:p-6">
      <div className="flex flex-wrap items-center gap-3">
        <h1 className="text-2xl font-bold">Discover</h1>
        <div className="relative ml-auto w-full max-w-md">
          <Search
            className="absolute top-1/2 left-3 size-4 -translate-y-1/2 text-faint"
            aria-hidden
          />
          <Input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Find a movie or show to request…"
            className="pl-9"
            aria-label="Find a title to request"
          />
          {q && (
            <button
              className="absolute top-1/2 right-2 -translate-y-1/2 text-faint"
              onClick={() => setQ("")}
              aria-label="Clear"
            >
              <X className="size-4" />
            </button>
          )}
        </div>
      </div>
      {!term && (
        <div className="flex flex-wrap gap-2" role="tablist">
          {categories.map((c) => (
            <button
              key={c.id}
              role="tab"
              aria-selected={category === c.id}
              onClick={() => pickCategory(c.id)}
              className={clsx(
                "rounded-full px-4 py-1.5 text-sm",
                category === c.id
                  ? "bg-accent text-black font-medium"
                  : "bg-surface-2 text-muted hover:text-text",
              )}
            >
              {c.label}
            </button>
          ))}
        </div>
      )}
      {!term && browsing && (
        <div className="grid grid-cols-2 gap-3 sm:flex sm:flex-wrap">
          {category === "tv" ? (
            <FilterSelect
              label="Network"
              value={network}
              onChange={setNetwork}
              options={filters.data?.networks ?? []}
              all="All networks"
            />
          ) : (
            <FilterSelect
              label="Studio"
              value={studio}
              onChange={setStudio}
              options={filters.data?.studios ?? []}
              all="All studios"
            />
          )}
          <FilterSelect
            label="Genre"
            value={genre}
            onChange={setGenre}
            options={
              (category === "tv"
                ? filters.data?.tvGenres
                : filters.data?.movieGenres) ?? []
            }
            all="All genres"
          />
          <div className="sm:w-40">
            <Select
              aria-label="Sort"
              value={sort}
              onChange={(e) => setSort(e.target.value as Sort)}
            >
              {sorts.map((o) => (
                <option key={o.id} value={o.id}>
                  {o.label}
                </option>
              ))}
            </Select>
          </div>
        </div>
      )}
      <MyRequests />
      {results.isPending ? (
        <Spinner />
      ) : results.error ? (
        <Alert tone="error">{results.error.message}</Alert>
      ) : (
        <>
          <div className="grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-4">
            {list.map((it) => (
              <Card
                key={`${it.mediaType}-${it.tmdbId}`}
                item={it}
                onRequest={() => setPicked(it)}
                onOpen={() => setViewing(it)}
              />
            ))}
          </div>
          {list.length === 0 && <p className="text-muted">Nothing found.</p>}
          <div ref={more} className="flex justify-center py-4">
            {results.isFetchingNextPage ? (
              <Spinner />
            ) : (
              results.hasNextPage && (
                <Button variant="ghost" onClick={() => results.fetchNextPage()}>
                  Load more
                </Button>
              )
            )}
          </div>
        </>
      )}
      {picked && (
        <RequestDialog item={picked} onClose={() => setPicked(null)} />
      )}
      {viewing && (
        <DetailsDialog
          item={viewing}
          onClose={() => setViewing(null)}
          onRequest={() => {
            setPicked(viewing);
            setViewing(null);
          }}
        />
      )}
    </div>
  );
}

const requestable = (item: DiscoverItem) =>
  item.availability === "none" ||
  (item.mediaType === "tv" && item.availability === "partial");

/** A title's description, with Request or Open (REQ-2). */
function DetailsDialog({
  item,
  onClose,
  onRequest,
}: {
  item: DiscoverItem;
  onClose: () => void;
  onRequest: () => void;
}) {
  const label = availabilityLabel[item.availability];
  return (
    <Dialog
      open
      onClose={onClose}
      title={item.title}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Close
          </Button>
          {item.itemId ? (
            <Link
              to="/item/$itemId"
              params={{ itemId: String(item.itemId) }}
              className="rounded-md bg-accent px-4 py-2 text-sm font-medium text-black"
            >
              Open in library
            </Link>
          ) : null}
          {requestable(item) && (
            <Button variant="primary" onClick={onRequest}>
              Request
            </Button>
          )}
        </>
      }
    >
      <div className="space-y-4">
        {item.backdropUrl && (
          <img
            src={item.backdropUrl}
            alt=""
            className="aspect-video w-full rounded-md object-cover"
          />
        )}
        <div className="flex gap-4">
          {item.posterUrl && !item.backdropUrl && (
            <img
              src={item.posterUrl}
              alt=""
              className="w-28 shrink-0 rounded-md object-cover"
            />
          )}
          <div className="space-y-2">
            <p className="text-sm text-muted">
              {[item.year, item.mediaType === "tv" ? "Series" : "Movie", label]
                .filter(Boolean)
                .join(" · ")}
            </p>
            <TitleRatings item={item} />
            <p className="text-sm leading-relaxed">
              {item.overview || "No description available."}
            </p>
          </div>
        </div>
      </div>
    </Dialog>
  );
}

/** Critic and audience ratings: Rotten Tomatoes, IMDb and TMDB, where known. */
function TitleRatings({ item }: { item: DiscoverItem }) {
  const ratings = useQuery({
    queryKey: ["requests", "ratings", item.mediaType, item.tmdbId],
    queryFn: () =>
      unwrap(
        api.GET("/requests/ratings/{mediaType}/{tmdbId}", {
          params: { path: { mediaType: item.mediaType, tmdbId: item.tmdbId } },
        }),
      ),
    staleTime: 24 * 3_600_000,
  });
  const rt = ratings.data?.rottenTomatoes;
  const imdb = ratings.data?.imdb;
  const chips: {
    label: string;
    value: string;
    title?: string;
    url?: string;
  }[] = [];
  if (rt?.criticsScore != null)
    chips.push({
      label: "Critics",
      value: `${rt.criticsScore}%`,
      title: rt.criticsRating
        ? `Rotten Tomatoes: ${rt.criticsRating}`
        : "Rotten Tomatoes",
      url: rt.url,
    });
  if (rt?.audienceScore != null)
    chips.push({
      label: "Audience",
      value: `${rt.audienceScore}%`,
      title: "Rotten Tomatoes audience score",
      url: rt.url,
    });
  if (imdb?.rating != null)
    chips.push({ label: "IMDb", value: imdb.rating.toFixed(1), url: imdb.url });
  if (item.tmdbRating)
    chips.push({ label: "TMDB", value: item.tmdbRating.toFixed(1) });
  if (!chips.length) return ratings.isPending ? <div className="h-7" /> : null;
  return (
    <div className="flex flex-wrap gap-2">
      {chips.map((c) => {
        const body = (
          <>
            <span className="text-faint">{c.label}</span>{" "}
            <span className="font-semibold">{c.value}</span>
          </>
        );
        return c.url ? (
          <a
            key={c.label}
            href={c.url}
            target="_blank"
            rel="noreferrer"
            title={c.title}
            className="rounded bg-surface-2 px-2 py-1 text-xs hover:bg-surface-3"
          >
            {body}
          </a>
        ) : (
          <span
            key={c.label}
            title={c.title}
            className="rounded bg-surface-2 px-2 py-1 text-xs"
          >
            {body}
          </span>
        );
      })}
    </div>
  );
}

function FilterSelect({
  label,
  value,
  onChange,
  options,
  all,
}: {
  label: string;
  value: number | null;
  onChange: (v: number | null) => void;
  options: { id: number; name: string }[];
  all: string;
}) {
  return (
    <div className="sm:w-48">
      <Select
        aria-label={label}
        value={value ?? ""}
        onChange={(e) =>
          onChange(e.target.value ? Number(e.target.value) : null)
        }
      >
        <option value="">{all}</option>
        {options.map((o) => (
          <option key={o.id} value={o.id}>
            {o.name}
          </option>
        ))}
      </Select>
    </div>
  );
}

function Card({
  item,
  onRequest,
  onOpen,
}: {
  item: DiscoverItem;
  onRequest: () => void;
  onOpen: () => void;
}) {
  const label = availabilityLabel[item.availability];
  const poster = (
    <div className="relative aspect-[2/3] overflow-hidden rounded-md bg-surface-2">
      {item.posterUrl ? (
        <img
          src={item.posterUrl}
          alt=""
          loading="lazy"
          className="size-full object-cover"
        />
      ) : (
        <div className="p-2 text-sm text-muted">{item.title}</div>
      )}
      {label && (
        <span
          className={clsx(
            "absolute top-1.5 left-1.5 rounded px-1.5 py-0.5 text-[11px] font-medium",
            item.availability === "available"
              ? "bg-green-600 text-white"
              : "bg-black/70 text-white",
          )}
        >
          {label}
        </span>
      )}
    </div>
  );
  return (
    <div className="space-y-1.5">
      <button
        type="button"
        className="block w-full text-left"
        onClick={onOpen}
        aria-label={`About ${item.title}`}
      >
        {poster}
      </button>
      <div className="truncate text-sm font-medium" title={item.title}>
        {item.title}
      </div>
      <div className="flex items-center gap-2 text-xs text-faint">
        <span>
          {[item.year, item.mediaType === "tv" ? "Series" : "Movie"]
            .filter(Boolean)
            .join(" · ")}
        </span>
        {requestable(item) && (
          <Button
            size="sm"
            variant="primary"
            className="ml-auto"
            onClick={onRequest}
          >
            Request
          </Button>
        )}
      </div>
    </div>
  );
}

function MyRequests() {
  const list = useQuery(myRequestsQuery);
  const cancel = useRequestAction((id: number) =>
    unwrap(
      api.DELETE("/requests/{requestId}", {
        params: { path: { requestId: id } },
      }),
    ),
  );
  if (!list.data?.length) return null;
  return (
    <section className="space-y-3">
      <h2 className="text-lg font-semibold">My requests</h2>
      <ul className="divide-y divide-border rounded-md bg-surface">
        {list.data.map((r) => (
          <li key={r.id} className="flex items-center gap-3 p-3">
            {r.posterUrl && (
              <img src={r.posterUrl} alt="" className="w-10 rounded" />
            )}
            <div className="min-w-0 flex-1">
              <div className="truncate font-medium">
                {r.title}{" "}
                {r.year && <span className="text-faint">({r.year})</span>}
              </div>
              <div className="text-xs text-muted">
                {r.mediaType === "tv" &&
                  (r.seasons.length
                    ? `Seasons ${r.seasons.join(", ")} · `
                    : "All seasons · ")}
                {new Date(r.createdAt).toLocaleDateString()}
                {r.reason && ` · ${r.reason}`}
              </div>
            </div>
            <Badge
              tone={
                r.status === "available" || r.status === "approved"
                  ? "accent"
                  : r.status === "pending"
                    ? "muted"
                    : "danger"
              }
            >
              {statusLabel[r.status]}
            </Badge>
            {r.status === "pending" && (
              <Button
                size="sm"
                variant="ghost"
                onClick={() => cancel.mutate(r.id)}
                aria-label={`Withdraw ${r.title}`}
              >
                Withdraw
              </Button>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
