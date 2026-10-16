import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronRight, FolderPlus, LayoutList, UsersRound } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { librariesQuery, meQuery } from "@/api/queries";
import { Button, Spinner } from "@/components/ui";
import { ContinueTile, HubItem, ViewMoreTile } from "./ContinueWatching";
import { EditHomeDialog } from "./EditHome";
import { GettingStarted } from "./GettingStarted";
import { pinnedTarget } from "./layout";
import { MuseButton } from "../search/MuseVideo";

export function HomePage() {
  const libraries = useQuery(librariesQuery);
  const me = useQuery(meQuery);
  const hubs = useQuery({
    queryKey: ["items", "hubs"],
    queryFn: () => unwrap(api.GET("/hubs/home")),
  });
  const [editing, setEditing] = useState(false);
  if (libraries.isPending || hubs.isPending) return <Spinner />;

  if (!libraries.data?.length) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-4 p-8 text-center">
        <FolderPlus className="size-14 text-faint" aria-hidden />
        <h1 className="text-xl font-semibold">
          Your server has no libraries yet
        </h1>
        <p className="max-w-md text-muted">
          Add your movie, TV, anime and music folders and Marquee will organise
          them here.
        </p>
        {me.data?.isAdmin && (
          <Link to="/settings/$section" params={{ section: "libraries" }}>
            <Button variant="primary">Add a library</Button>
          </Link>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-10 py-6 lg:py-8">
      <div className="-mb-6 flex justify-end px-6 lg:px-8">
        <MuseButton />
      </div>
      {me.data?.isAdmin && <GettingStarted />}
      <WatchTogetherBanner myId={me.data?.id} />
      {hubs.data?.map((hub) => {
        const wide = hub.id === "continue-watching";
        // Continue Watching: entries holds the library's and on-demand titles together.
        const entries = wide ? hub.entries : undefined;
        // A pinned collection or playlist opens itself (USER-12).
        const pinned = pinnedTarget(hub.id);
        return (
          <section key={hub.id}>
            <div className="mb-3 flex items-center gap-2 px-6 lg:px-8">
              <h2 className="text-lg font-semibold">
                {wide ? (
                  <Link to="/continue-watching" className="hover:underline">
                    {hub.title}
                  </Link>
                ) : pinned ? (
                  <Link {...pinned} className="hover:underline">
                    {hub.title}
                  </Link>
                ) : (
                  hub.title
                )}
              </h2>
              {wide ? (
                <Link to="/continue-watching" className="text-muted hover:text-text" aria-label={`Open ${hub.title}`}>
                  <ChevronRight className="size-5" />
                </Link>
              ) : pinned ? (
                <Link
                  {...pinned}
                  className="text-muted hover:text-text"
                  aria-label={`Open ${hub.title}`}
                >
                  <ChevronRight className="size-5" />
                </Link>
              ) : hub.libraryId && (
                <Link
                  to="/library/$libraryId"
                  params={{ libraryId: String(hub.libraryId) }}
                  className="text-muted hover:text-text"
                  aria-label={`Open ${hub.title}`}
                >
                  <ChevronRight className="size-5" />
                </Link>
              )}
            </div>
            <ul className="flex gap-4 overflow-x-auto px-6 pb-2 lg:px-8">
              {entries
                ? entries.map((e) => <ContinueTile key={e.key} e={e} />)
                : hub.items.map((it) => <HubItem key={it.id} it={it} wide={wide} continueWatching={wide} />)}
              {entries && (hub.total ?? 0) > entries.length && <ViewMoreTile total={hub.total ?? 0} />}
            </ul>
          </section>
        );
      })}
      {hubs.data?.length === 0 && (
        <p className="px-8 text-muted">
          Nothing here yet. Libraries are still being scanned.
        </p>
      )}
      <div className="flex justify-center px-6 lg:px-8">
        <Button variant="ghost" size="sm" onClick={() => setEditing(true)}>
          <LayoutList className="size-4" /> Edit Home
        </Button>
      </div>
      {editing && <EditHomeDialog onClose={() => setEditing(false)} />}
    </div>
  );
}

/** Groups others are watching in (SyncPlay), with a Join button. */
function WatchTogetherBanner({ myId }: { myId?: number }) {
  const groups = useQuery({
    queryKey: ["syncplay", "groups"],
    queryFn: () => unwrap(api.GET("/syncplay/groups")),
    refetchInterval: 15_000,
  });
  const open =
    groups.data?.filter((g) => !g.members.some((m) => m.userId === myId)) ?? [];
  if (!open.length) return null;
  return (
    <div className="space-y-2 px-6 lg:px-8">
      {open.map((g) => (
        <div
          key={g.id}
          className="flex items-center gap-3 rounded-lg border border-accent/40 bg-accent/10 px-4 py-3"
        >
          <UsersRound className="size-5 text-accent" aria-hidden />
          <div className="min-w-0 flex-1">
            <div className="truncate font-medium">{g.title}</div>
            <div className="truncate text-sm text-muted">
              {g.members.map((m) => m.name).join(", ")}{" "}
              {g.members.length === 1 ? "is" : "are"} watching together
            </div>
          </div>
          <Link
            to="/play/$itemId"
            params={{ itemId: String(g.itemId) }}
            search={{ g: g.id }}
          >
            <Button variant="primary">Join</Button>
          </Link>
        </div>
      ))}
    </div>
  );
}
