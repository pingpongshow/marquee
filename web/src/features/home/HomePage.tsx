import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronRight, FolderPlus, LayoutList, UsersRound } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { librariesQuery, meQuery } from "@/api/queries";
import type { ItemSummary } from "@/api/types";
import { Button, Spinner } from "@/components/ui";
import { Poster } from "../browse/Poster";
import { subtitleFor } from "../browse/format";
import { EditHomeDialog } from "./EditHome";
import { GettingStarted } from "./GettingStarted";
import { pinnedTarget } from "./layout";
import { MuseButton } from "../search/MuseVideo";

function HubItem({ it, wide }: { it: ItemSummary; wide: boolean }) {
  // Continue Watching episodes open the player directly.
  const playable =
    (it.type === "episode" || it.type === "movie" || it.type === "video") &&
    wide;
  const title = it.type === "episode" ? it.grandparentTitle : it.title;
  const sub =
    it.type === "episode"
      ? `${it.parentTitle ?? ""} · E${it.index ?? ""}${it.title ? ` · ${it.title}` : ""}`
      : subtitleFor(it);
  const body = (
    <>
      <Poster
        item={it}
        shape={
          wide
            ? "wide"
            : it.type === "album" || it.type === "artist"
              ? "square"
              : "poster"
        }
        width={wide ? 320 : 180}
        className="group-hover:ring-2 group-hover:ring-accent"
      />
      <div className="mt-2 truncate text-sm font-medium">{title}</div>
      <div className="truncate text-xs text-muted">{sub}</div>
    </>
  );
  return (
    <li className={wide ? "w-72 shrink-0" : "w-36 shrink-0"}>
      {playable ? (
        <Link
          to="/play/$itemId"
          params={{ itemId: String(it.id) }}
          search={{ t: undefined }}
          className="group block"
        >
          {body}
        </Link>
      ) : (
        <Link
          to="/item/$itemId"
          params={{ itemId: String(it.id) }}
          className="group block"
        >
          {body}
        </Link>
      )}
    </li>
  );
}

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
        // A pinned collection or playlist opens itself (USER-12).
        const pinned = pinnedTarget(hub.id);
        return (
          <section key={hub.id}>
            <div className="mb-3 flex items-center gap-2 px-6 lg:px-8">
              <h2 className="text-lg font-semibold">
                {pinned ? (
                  <Link {...pinned} className="hover:underline">
                    {hub.title}
                  </Link>
                ) : (
                  hub.title
                )}
              </h2>
              {pinned ? (
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
              {hub.items.map((it) => (
                <HubItem key={it.id} it={it} wide={wide} />
              ))}
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
