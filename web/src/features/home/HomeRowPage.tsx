import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { api, unwrap } from "@/api/client";
import { Alert, Spinner } from "@/components/ui";
import { HubItem } from "./ContinueWatching";
import { VodSuggestionTile } from "../livetv/VodSuggestions";

/** See all (USER-12): one Home row in full, library titles and on-demand ones (marked VOD) together. */
export function HomeRowPage() {
  const { hubId } = useParams({ from: "/home/row/$hubId" });
  // Under "items" so watch-state changes refresh it, as they do Home.
  const hub = useQuery({
    queryKey: ["items", "hubs", hubId],
    queryFn: () => unwrap(api.GET("/hubs/home/{hubId}", { params: { path: { hubId }, query: { mixed: true, limit: 100 } } })),
  });
  if (hub.isPending) return <Spinner />;
  if (hub.isError)
    return (
      <div className="p-8">
        <Alert tone="error">{hub.error.message}</Alert>
      </div>
    );
  const h = hub.data;
  // The Watch on VOD row (older Home layouts) holds its titles in vodEntries.
  const mixed = h.mixedEntries ?? h.vodEntries?.map((v) => ({ kind: "vod" as const, key: v.key, vod: v, item: undefined }));
  const count = mixed ? mixed.length : h.items.length;
  return (
    <div className="space-y-6 p-6 lg:p-8">
      <div className="flex items-baseline gap-3">
        <h1 className="text-2xl font-bold">{h.title}</h1>
        <span className="text-sm text-muted">{count}</span>
      </div>
      {count === 0 ? (
        <p className="text-muted">Nothing here right now.</p>
      ) : (
        <ul className="grid grid-cols-[repeat(auto-fill,minmax(9rem,1fr))] gap-x-4 gap-y-6">
          {mixed
            ? mixed.map((e) =>
                e.vod ? (
                  <VodSuggestionTile key={e.key} s={e.vod} badge className="min-w-0" />
                ) : e.item ? (
                  <HubItem key={e.key} it={e.item} wide={false} continueWatching={false} className="min-w-0" />
                ) : null,
              )
            : h.items.map((it) => <HubItem key={it.id} it={it} wide={false} continueWatching={false} className="min-w-0" />)}
        </ul>
      )}
    </div>
  );
}
