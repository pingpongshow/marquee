import { useNavigate } from "@tanstack/react-router";
import { Disc3, ListEnd, ListPlus, ListStart, MoreHorizontal, Radio, UserRound } from "lucide-react";
import { useState } from "react";
import { fetchLeaves } from "@/api/queries";
import type { ItemSummary } from "@/api/types";
import { Menu, MenuDivider, MenuItem } from "@/components/Menu";
import { AddToPlaylistDialog } from "../playlists/AddToPlaylist";
import { useMusic } from "../player/MusicPlayer";

const music = new Set(["track", "album", "artist"]);

/** The "…" menu on tracks, albums, artists and videos: queue and playlist actions. */
export function ItemMenu({ item, className }: { item: ItemSummary; className?: string }) {
  const player = useMusic();
  const navigate = useNavigate();
  const [adding, setAdding] = useState(false);
  const isMusic = music.has(item.type);
  const leaves = async () => (item.type === "track" ? [item] : await fetchLeaves(item.id));
  return (
    <>
      <Menu label={`More actions for ${item.title}`} trigger={<MoreHorizontal className="size-4" />} className={className}>
        {isMusic && (
          <>
            <MenuItem icon={<ListStart />} onClick={async () => player.playNext(await leaves())}>
              Play next
            </MenuItem>
            <MenuItem icon={<ListEnd />} onClick={async () => player.addToQueue(await leaves())}>
              Add to queue
            </MenuItem>
            {item.type !== "track" && (
              <MenuItem icon={<Radio />} onClick={async () => player.play(await fetchLeaves(item.id, { shuffle: true }))}>
                Shuffle play
              </MenuItem>
            )}
          </>
        )}
        <MenuItem icon={<ListPlus />} onClick={() => setAdding(true)}>
          Add to playlist…
        </MenuItem>
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
    </>
  );
}
