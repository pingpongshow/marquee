import { useMutation } from "@tanstack/react-query";
import { api, unwrap } from "@/api/client";
import type { RadioRequest } from "@/api/types";
import { useMusicActions } from "../player/MusicPlayer";

/** Starts a station (MUSIC-3) and keeps it topped up as it plays. */
/** A radio request; the limit has a server default. */
export type RadioInput = Omit<RadioRequest, "limit"> & { limit?: number };

export function useRadio() {
  const music = useMusicActions();
  return useMutation({
    mutationFn: (input: RadioInput) => {
      const req: RadioRequest = { limit: 50, ...input };
      return unwrap(api.POST("/music/radio", { body: req })).then((st) => ({
        st,
        req,
      }));
    },
    onSuccess: ({ st, req }) => {
      if (st.items.length) music.playStation(st, req);
    },
  });
}

/** Builds a playlist from a description (Muse, MUSIC-5) and plays it. */
export function useMuse() {
  const music = useMusicActions();
  return useMutation({
    mutationFn: (body: { prompt: string; libraryId?: number }) =>
      unwrap(api.POST("/music/muse", { body: { ...body, limit: 40 } })),
    onSuccess: (st) => {
      if (st.items.length) music.playStation(st);
    },
  });
}
