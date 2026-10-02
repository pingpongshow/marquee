import { useNavigate } from "@tanstack/react-router";
import { Compass, Disc3, ListEnd, ListPlus, ListStart, MoreHorizontal, Radio, Shuffle, UserRound } from "lucide-react";
import { useState } from "react";
import { fetchLeaves } from "@/api/queries";
import type { ItemSummary } from "@/api/types";
import { Menu, MenuDivider, MenuItem } from "@/components/Menu";
import { AdventureDialog } from "../music/Adventure";
import { useRadio } from "../music/useRadio";
import { AddToPlaylistDialog } from "../playlists/AddToPlaylist";
import { useMusic } from "../player/MusicPlayer";

const music = new Set(["track", "album", "artist"]);

/** The "…" menu on tracks, albums, artists and videos: queue and playlist actions. */
export function ItemMenu({ item, className }: { item: ItemSummary; className?: string }) {
  const player = useMusic();
  const navigate = useNavigate();
  const [adding, setAdding] = useState(false);
  const [adventure, setAdventure] = useState(false);
  const radio = useRadio();
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
            <MenuItem icon={<Radio />} onClick={() => radio.mutate({ seed: "item", itemId: item.id })}>
              Start Radio
            </MenuItem>
            {item.type === "track" && (
              <MenuItem icon={<Compass />} onClick={() => setAdventure(true)}>
                Sonic Adventure…
              </MenuItem>
            )}
            {item.type !== "track" && (
              <MenuItem icon={<Shuffle />} onClick={async () => player.play(await fetchLeaves(item.id, { shuffle: true }), 0, { source: item.title })}>
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
      {adventure && <AdventureDialog from={item} onClose={() => setAdventure(false)} />}
    </>
  );
}
