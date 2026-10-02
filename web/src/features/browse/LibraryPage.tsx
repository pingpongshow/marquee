import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useState } from "react";
import { libraryItemsQuery, librariesQuery, type ItemSort } from "@/api/queries";
import { Alert, Button, Select, Spinner } from "@/components/ui";
import { Poster } from "./Poster";
import { subtitleFor } from "./format";

const PAGE = 120;

export function LibraryPage() {
  const { libraryId } = useParams({ from: "/library/$libraryId" });
  const id = Number(libraryId);
  const [sort, setSort] = useState<ItemSort>("title");
  const [limit, setLimit] = useState(PAGE);
  const lib = useQuery(librariesQuery).data?.find((l) => l.id === id);
  const items = useQuery({ ...libraryItemsQuery(id, sort, 0, limit), placeholderData: (prev) => prev });
  const shape = lib?.type === "music" ? "square" : lib?.type === "videos" ? "wide" : "poster";

  return (
    <div className="p-6 lg:p-8">
      <div className="mb-6 flex flex-wrap items-center gap-4">
        <h1 className="text-2xl font-bold">{lib?.name ?? "Library"}</h1>
        {items.data && <span className="text-sm text-muted">{items.data.total.toLocaleString()} items</span>}
        <div className="ml-auto w-48">
          <Select aria-label="Sort by" value={sort} onChange={(e) => setSort(e.target.value as ItemSort)}>
            <option value="title">Title</option>
            <option value="-added">Recently added</option>
            <option value="-year">Year (newest)</option>
            <option value="year">Year (oldest)</option>
          </Select>
        </div>
      </div>
      {items.isPending && <Spinner />}
      {items.isError && <Alert tone="error">{items.error.message}</Alert>}
      {items.data?.total === 0 && (
        <p className="text-muted">
          Nothing here yet. {lib?.scanStatus !== "idle" ? "A scan is in progress." : "Scan the library from Settings → Libraries."}
        </p>
      )}
      <ul className={shape === "wide" ? "grid grid-cols-[repeat(auto-fill,minmax(240px,1fr))] gap-x-4 gap-y-6" : "grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-x-4 gap-y-6"}>
        {items.data?.items.map((it) => (
          <li key={it.id}>
            <Link to="/item/$itemId" params={{ itemId: String(it.id) }} className="group block">
              <Poster item={it} shape={shape} className="transition-transform group-hover:scale-[1.03] group-hover:ring-2 group-hover:ring-accent" />
              <div className="mt-2 truncate text-sm font-medium" title={it.title}>
                {it.title}
              </div>
              <div className="truncate text-xs text-muted">{subtitleFor(it)}</div>
            </Link>
          </li>
        ))}
      </ul>
      {items.data && items.data.items.length < items.data.total && (
        <div className="mt-8 flex justify-center">
          <Button onClick={() => setLimit((l) => l + PAGE)} loading={items.isFetching}>
            Show more
          </Button>
        </div>
      )}
    </div>
  );
}
