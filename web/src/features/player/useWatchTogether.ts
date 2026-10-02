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
export function useWatchTogether(
  video: RefObject<HTMLVideoElement | null>,
  itemId: number,
  meId: number | undefined,
  initialGroup?: string,
) {
  const [group, setGroup] = useState<WatchGroup | null>(null);
  const [error, setError] = useState<string | null>(null);
  const groupRef = useRef<WatchGroup | null>(null);
  const applying = useRef(0); // until this time, video events are ours
  const offset = useRef(0); // server clock minus ours

  const apply = useCallback(
    (g: WatchGroup) => {
      groupRef.current = g;
      setGroup(g);
      offset.current = new Date(g.serverTime).getTime() - Date.now();
      const v = video.current;
      if (!v || v.readyState < 1) return;
      const serverNow = Date.now() + offset.current;
      const target = g.playing
        ? g.positionMs + (serverNow - new Date(g.at).getTime())
        : g.positionMs;
      const quiet = () => (applying.current = Date.now() + 800);
      if (Math.abs(v.currentTime * 1000 - target) > DRIFT) {
        quiet();
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
    [video],
  );

  const send = useCallback(async (action: Action) => {
    const g = groupRef.current;
    const v = video.current;
    if (!g || !v) return;
    try {
      const next = await unwrap(
        api.POST("/syncplay/groups/{groupId}/command", {
          params: { path: { groupId: g.id } },
          body: { action, positionMs: Math.round(v.currentTime * 1000) },
        }),
      );
      groupRef.current = next;
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
    (async () => {
      let failures = 0;
      while (!stop) {
        const since = groupRef.current?.version ?? 0;
        try {
          const g = await unwrap(
            api.GET("/syncplay/groups/{groupId}", {
              params: { path: { groupId: id }, query: { since } },
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
    };
  }, [id, apply]);

  // A joiner gets the group's state before the video has loaded; apply it once it can be.
  useEffect(() => {
    const v = video.current;
    if (!v) return;
    const onMeta = () => groupRef.current && apply(groupRef.current);
    v.addEventListener("loadedmetadata", onMeta);
    return () => v.removeEventListener("loadedmetadata", onMeta);
  }, [video, apply]);

  // Send what the viewer does.
  useEffect(() => {
    const v = video.current;
    if (!v || !id) return;
    const mine = () => Date.now() > applying.current;
    const onPlay = () => mine() && void send("play");
    const onPause = () => mine() && !v.ended && void send("pause");
    const onSeeked = () => mine() && void send("seek");
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
  }, [id, video, send, meId]);

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
    const v = video.current;
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
  }, [itemId, video, apply]);

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
