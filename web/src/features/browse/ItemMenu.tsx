import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "@/components/Toast";
import { useNavigate } from "@tanstack/react-router";
import { Bookmark, BookmarkMinus, CircleCheck, CircleSlash, Compass, Disc3, EyeOff, Heart, HeartOff, Layers, ListEnd, ListPlus, ListStart, MoreHorizontal, Radio, Shuffle, UserRound } from "lucide-react";
import { clsx } from "clsx";
import { useState, type MouseEvent } from "react";
import { api, unwrap } from "@/api/client";
import { fetchLeaves, meQuery } from "@/api/queries";
import type { ItemSummary } from "@/api/types";
import { Menu, MenuDivider, MenuItem } from "@/components/Menu";
import { JourneyDialog } from "../music/Journey";
import { useRadio } from "../music/useRadio";
import { AddToPlaylistDialog } from "../playlists/AddToPlaylist";
import { useMusicActions } from "../player/MusicPlayer";
import { AddToCollectionDialog } from "./AddToCollection";
import { MoveTrackDialog } from "./MusicFixes";
import { favoriteTypes, useSetFavorite } from "./Favorite";
import { isWatched, markableTypes, useMarkWatched, useRemoveFromContinueWatching } from "./watched";

const music = new Set(["track", "album", "artist"]);
const watchable = new Set(["movie", "show", "episode", "video"]);

/** The "…" menu on tracks, albums, artists and videos: queue, playlist and watch-state actions. */
export function ItemMenu({
  item,
  className,
  continueWatching,
  open,
  onOpenChange,
  floating,
}: {
  item: ItemSummary;
  className?: string;
  /** The item is a Continue Watching tile: offer to remove it from the row (USER-6). */
  continueWatching?: boolean;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  floating?: boolean;
}) {
  const markWatched = useMarkWatched();
  const removeCw = useRemoveFromContinueWatching();
  const watched = isWatched(item);
  const player = useMusicActions();
  const navigate = useNavigate();
  const [adding, setAdding] = useState(false);
  const [journey, setJourney] = useState(false);
  const [collecting, setCollecting] = useState(false);
  const [moving, setMoving] = useState(false);
  const radio = useRadio();
  const qc = useQueryClient();
  const admin = !!useQuery(meQuery).data?.isAdmin;
  const watchlist = useMutation({
    mutationFn: (on: boolean) =>
      on
        ? unwrap(api.PUT("/items/{itemId}/watchlist", { params: { path: { itemId: item.id } } }))
        : unwrap(api.DELETE("/items/{itemId}/watchlist", { params: { path: { itemId: item.id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["items"] }),
  });
  const setFavorite = useSetFavorite();
  const isMusic = music.has(item.type);
  const leaves = async () => (item.type === "track" ? [item] : await fetchLeaves(item.id));
  // Runs a menu action that loads tracks first; a failed load shows instead of vanishing.
  const withTracks = (fn: () => Promise<void>) => () =>
    void fn().catch((e: Error) => toast(`Couldn't load the tracks. ${e.message}`, { tone: "error" }));
  return (
    <>
      <Menu label={`More actions for ${item.title}`} trigger={<MoreHorizontal className="size-4" />} className={className} open={open} onOpenChange={onOpenChange} floating={floating}>
        {markableTypes.has(item.type) && (
          <MenuItem icon={watched ? <CircleSlash /> : <CircleCheck />} onClick={() => markWatched.mutate({ item, watched: !watched })}>
            {watched ? "Mark as unwatched" : "Mark as watched"}
          </MenuItem>
        )}
        {continueWatching && (
          <MenuItem icon={<EyeOff />} onClick={() => removeCw.mutate(item.id)}>
            Remove from Continue Watching
          </MenuItem>
        )}
        {isMusic && (
          <>
            <MenuItem icon={<ListStart />} onClick={withTracks(async () => player.playNext(await leaves()))}>
              Play next
            </MenuItem>
            <MenuItem icon={<ListEnd />} onClick={withTracks(async () => player.addToQueue(await leaves()))}>
              Add to queue
            </MenuItem>
            <MenuItem icon={<Radio />} onClick={() => radio.mutate({ seed: "item", itemId: item.id })}>
              Start Radio
            </MenuItem>
            {item.type === "track" && (
              <MenuItem icon={<Compass />} onClick={() => setJourney(true)}>
                Sound Journey…
              </MenuItem>
            )}
            {item.type !== "track" && (
              <MenuItem icon={<Shuffle />} onClick={withTracks(async () => player.play(await fetchLeaves(item.id, { shuffle: true }), 0, { source: item.title }))}>
                Shuffle play
              </MenuItem>
            )}
          </>
        )}
        {favoriteTypes.has(item.type) && (
          <MenuItem icon={item.favorite ? <HeartOff /> : <Heart />} onClick={() => setFavorite.mutate({ itemId: item.id, on: !item.favorite })}>
            {item.favorite ? "Remove from favourites" : "Add to favourites"}
          </MenuItem>
        )}
        {watchable.has(item.type) && (
          <MenuItem icon={item.watchlisted ? <BookmarkMinus /> : <Bookmark />} onClick={() => watchlist.mutate(!item.watchlisted)}>
            {item.watchlisted ? "Remove from watchlist" : "Add to watchlist"}
          </MenuItem>
        )}
        {item.type !== "collection" && (
          <MenuItem icon={<ListPlus />} onClick={() => setAdding(true)}>
            Add to playlist…
          </MenuItem>
        )}
        {admin && (item.type === "movie" || item.type === "show" || item.type === "video") && (
          <MenuItem icon={<Layers />} onClick={() => setCollecting(true)}>
            Add to collection…
          </MenuItem>
        )}
        {admin && item.type === "track" && (
          <MenuItem icon={<Disc3 />} onClick={() => setMoving(true)}>
            Move to album…
          </MenuItem>
        )}
        {item.type === "track" && (item.parentId || item.grandparentId) && <MenuDivider />}
        {item.type === "track" && item.parentId && (
          <MenuItem icon={<Disc3 />} onClick={() => navigate({ to: "/item/$itemId", params: { itemId: String(item.parentId) } })}>
            Go to album
          </MenuItem>
        )}
        {item.type === "track" && item.grandparentId && (
          <MenuItem icon={<UserRound />} onClick={() => navigate({ to: "/item/$itemId", params: { itemId: String(item.grandparentId) } })}>
            Go to artist
          </MenuItem>
        )}
      </Menu>
      {adding && <AddToPlaylistDialog item={item} onClose={() => setAdding(false)} />}
      {collecting && <AddToCollectionDialog item={item} onClose={() => setCollecting(false)} />}
      {moving && <MoveTrackDialog track={item} onClose={() => setMoving(false)} />}
      {journey && <JourneyDialog from={item} onClose={() => setJourney(false)} />}
    </>
  );
}

/**
 * A poster tile's "…" menu: shows on hover in the tile's corner, and right-click on the tile opens it.
 * Put it inside a `relative group/tile` element and pass that element `onContextMenu`.
 */
export function useTileMenu(item: ItemSummary, opts?: { continueWatching?: boolean }) {
  const [open, setOpen] = useState(false);
  return {
    onContextMenu: (e: MouseEvent) => {
      e.preventDefault();
      setOpen(true);
    },
    menu: (
      <div
        className={clsx(
          "absolute top-1.5 right-1.5 z-20 rounded-full bg-black/70 text-white transition-opacity [&>div>button]:text-white",
          open ? "opacity-100" : "opacity-0 group-hover/tile:opacity-100 focus-within:opacity-100",
        )}
      >
        <ItemMenu item={item} continueWatching={opts?.continueWatching} open={open} onOpenChange={setOpen} floating />
      </div>
    ),
  };
}
