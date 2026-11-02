import { clsx } from "clsx";
import { Loader2, Music2, Radio } from "lucide-react";
import type { MouseEvent } from "react";
import { Button } from "@/components/ui";
import { useStartRadio } from "./useRadio";

/** A song row's small radio button: starts a station seeded with the track. Works inside a row's link. */
export function TrackRadioButton({ item, className }: { item: { id: number; title: string }; className?: string }) {
  const radio = useStartRadio();
  const start = (e: MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    radio.start({ seed: "item", itemId: item.id });
  };
  return (
    <button
      type="button"
      onClick={start}
      disabled={radio.pending}
      aria-label={`Track Radio: ${item.title}`}
      title="Track Radio"
      data-testid="track-radio"
      className={clsx("shrink-0 rounded-full p-0.5 text-muted transition-colors hover:text-text disabled:opacity-50", className)}
    >
      {radio.pending ? <Loader2 className="size-3.5 animate-spin" aria-hidden /> : <Music2 className="size-3.5" aria-hidden />}
    </button>
  );
}

/** The artist page's radio, beside Play and Shuffle: a station from all of the artist's music. */
export function ArtistRadioButton({ itemId }: { itemId: number }) {
  const radio = useStartRadio();
  return (
    <Button loading={radio.pending} onClick={() => radio.start({ seed: "item", itemId })} data-testid="artist-radio">
      {!radio.pending && <Radio className="size-4 text-accent" aria-hidden />} Artist Radio
    </Button>
  );
}
