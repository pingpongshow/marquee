import {
  type RefObject,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { ApiError, api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";

export type WatchGroup = components["schemas"]["WatchGroup"];
type Action = "play" | "pause" | "seek" | "buffering" | "ready";

/** How far apart players may drift before being pulled back (ms). */
const DRIFT = 1500;

/**
 * Watch together (SyncPlay): keeps a video element in step with a group. The group's state
 * is long-polled; local play, pause, seek and buffering are sent as commands. Events caused
 * by applying the group's state aren't echoed back.
 */
/** Server clock minus ours, from a group state that has just arrived. */
export const clockOffset = (g: Pick<WatchGroup, "serverTime">, now: number) => new Date(g.serverTime).getTime() - now;

/** Where the group is now (ms): its position when it last changed, plus the time played since. */
export function groupPosition(g: Pick<WatchGroup, "playing" | "positionMs" | "at">, offset: number, now: number) {
  return g.playing ? g.positionMs + (now + offset - new Date(g.at).getTime()) : g.positionMs;
}

export function useWatchTogether(
  videoRef: RefObject<HTMLVideoElement | null>,
  itemId: number,
  meId: number | undefined,
  initialGroup?: string,
) {
  const [group, setGroup] = useState<WatchGroup | null>(null);
  const [error, setError] = useState<string | null>(null);
  const groupRef = useRef<WatchGroup | null>(null);
  const applying = useRef(0); // until this time, video events are ours
  const offset = useRef(0); // server clock minus ours
  // Until this time the next "seeked" is ours (a seek we applied, which can take seconds to
  // finish on a transcode: longer than the window above, so it was sent back to the group).
  const ownSeek = useRef(0);

  // `fresh`: the state has just come from the server, so its serverTime is now. A stored state
  // applied again later (once the video has loaded) keeps the offset measured when it arrived:
  // measuring it again then put the viewer behind by however long the loading took.
  const apply = useCallback(
    (g: WatchGroup, fresh = true) => {
      groupRef.current = g;
      setGroup(g);
      if (fresh) offset.current = clockOffset(g, Date.now());
      const v = videoRef.current;
      if (!v || v.readyState < 1) return;
      const target = groupPosition(g, offset.current, Date.now());
      const quiet = () => (applying.current = Date.now() + 800);
      if (Math.abs(v.currentTime * 1000 - target) > DRIFT) {
        quiet();
        ownSeek.current = Date.now() + 15_000;
        v.currentTime = target / 1000;
      }
      if (g.playing && v.paused) {
        quiet();
        v.play().catch(() => {});
      } else if (!g.playing && !v.paused) {
        quiet();
        v.pause();
      }
    },
    [videoRef],
  );

  const send = useCallback(async (action: Action) => {
    const g = groupRef.current;
    const v = videoRef.current;
    if (!g || !v) return;
    try {
      const next = await unwrap(
        api.POST("/syncplay/groups/{groupId}/command", {
          params: { path: { groupId: g.id } },
          body: { action, positionMs: Math.round(v.currentTime * 1000) },
        }),
      );
      groupRef.current = next;
      offset.current = clockOffset(next, Date.now());
      setGroup(next);
    } catch (e) {
      setError((e as Error).message);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Join a group given in the link.
  useEffect(() => {
    if (!initialGroup) return;
    unwrap(
      api.POST("/syncplay/groups/{groupId}/join", {
        params: { path: { groupId: initialGroup } },
      }),
    )
      .then(apply)
      .catch((e: Error) => setError(e.message));
  }, [initialGroup, apply]);

  // Follow the group: long polling. Network hiccups are retried with backoff; the group is
  // only given up when it's gone (404/410) or the server stays unreachable.
  const id = group?.id;
  useEffect(() => {
    if (!id) return;
    let stop = false;
    // Ends the waiting request on leaving, so it doesn't hold a connection for its full wait.
    const abort = new AbortController();
    (async () => {
      let failures = 0;
      while (!stop) {
        const since = groupRef.current?.version ?? 0;
        try {
          const g = await unwrap(
            api.GET("/syncplay/groups/{groupId}", {
              params: { path: { groupId: id }, query: { since } },
              signal: abort.signal,
            }),
          );
          failures = 0;
          if (!stop) apply(g);
        } catch (e) {
          if (stop) return;
          const status = e instanceof ApiError ? e.status : 0;
          if (status !== 404 && status !== 410 && ++failures <= 3) {
            await new Promise((r) => setTimeout(r, 1000 * 2 ** (failures - 1)));
            continue;
          }
          setError((e as Error).message);
          setGroup(null);
          groupRef.current = null;
          if (status !== 404 && status !== 410)
            void api
              .POST("/syncplay/groups/{groupId}/leave", {
                params: { path: { groupId: id } },
              })
              .catch(() => {});
          return;
        }
      }
    })();
    return () => {
      stop = true;
      abort.abort();
    };
  }, [id, apply]);

  // A joiner gets the group's state before the video has loaded; apply it once it can be.
  useEffect(() => {
    const v = videoRef.current;
    if (!v) return;
    const onMeta = () => groupRef.current && apply(groupRef.current, false);
    v.addEventListener("loadedmetadata", onMeta);
    return () => v.removeEventListener("loadedmetadata", onMeta);
  }, [videoRef, apply]);

  // Send what the viewer does.
  useEffect(() => {
    const v = videoRef.current;
    if (!v || !id) return;
    const mine = () => Date.now() > applying.current;
    const onPlay = () => mine() && void send("play");
    const onPause = () => mine() && !v.ended && void send("pause");
    const onSeeked = () => {
      // The end of a seek applied from the group, however long it took: not the viewer's.
      if (Date.now() < ownSeek.current) {
        ownSeek.current = 0;
        return;
      }
      if (mine()) void send("seek");
    };
    const onWaiting = () => void send("buffering");
    const onPlaying = () => {
      // Only my own loading holds the group up.
      if (
        groupRef.current?.members.some((m) => m.userId === meId && m.buffering)
      )
        void send("ready");
    };
    v.addEventListener("play", onPlay);
    v.addEventListener("pause", onPause);
    v.addEventListener("seeked", onSeeked);
    v.addEventListener("waiting", onWaiting);
    v.addEventListener("playing", onPlaying);
    v.addEventListener("canplay", onPlaying);
    return () => {
      v.removeEventListener("play", onPlay);
      v.removeEventListener("pause", onPause);
      v.removeEventListener("seeked", onSeeked);
      v.removeEventListener("waiting", onWaiting);
      v.removeEventListener("playing", onPlaying);
      v.removeEventListener("canplay", onPlaying);
    };
  }, [id, videoRef, send, meId]);

  // Leave when the player closes.
  useEffect(
    () => () => {
      const g = groupRef.current;
      if (g)
        void api.POST("/syncplay/groups/{groupId}/leave", {
          params: { path: { groupId: g.id } },
        });
    },
    [],
  );

  const start = useCallback(async () => {
    const v = videoRef.current;
    try {
      const g = await unwrap(
        api.POST("/syncplay/groups", {
          body: {
            itemId,
            positionMs: Math.round((v?.currentTime ?? 0) * 1000),
          },
        }),
      );
      apply(g);
      setError(null);
    } catch (e) {
      setError((e as Error).message);
    }
  }, [itemId, videoRef, apply]);

  const leave = useCallback(() => {
    const g = groupRef.current;
    groupRef.current = null;
    setGroup(null);
    if (g)
      void api.POST("/syncplay/groups/{groupId}/leave", {
        params: { path: { groupId: g.id } },
      });
  }, []);

  return { group, error, start, leave };
}
