import { useEffect, useRef, useState } from "react";
import { api, unwrap } from "@/api/client";
import { deviceProfile } from "@/features/player/deviceProfile";

/** Plays a channel's live stream (LIVE-3). Stops the server stream when it goes away. */
export function LivePlayer({
  channelId,
  muted,
  controls,
  className,
  onError,
}: {
  channelId: number;
  muted?: boolean;
  controls?: boolean;
  className?: string;
  onError?: (msg: string | null) => void;
}) {
  const video = useRef<HTMLVideoElement>(null);
  const [loading, setLoading] = useState(true);
  // React doesn't keep the muted property in step with the attribute; autoplay needs it.
  useEffect(() => {
    if (video.current) video.current.muted = !!muted;
  }, [muted]);
  useEffect(() => {
    let hls: { destroy: () => void } | null = null;
    let session: string | null = null;
    let cancelled = false;
    const v = video.current;
    const stop = (id: string) =>
      void api
        .DELETE("/livetv/sessions/{sessionId}", {
          params: { path: { sessionId: id } },
        })
        .catch(() => {});
    setLoading(true);
    onError?.(null);
    (async () => {
      const [s, { default: Hls }] = await Promise.all([
        unwrap(
          api.POST("/livetv/channels/{channelId}/play", {
            params: { path: { channelId } },
            body: { profile: deviceProfile() },
          }),
        ),
        import("hls.js"),
      ]);
      // Switched channel (or closed) while tuning: end the session nobody will watch.
      if (cancelled) return stop(s.id);
      session = s.id;
      if (!v) return;
      if (Hls.isSupported()) {
        const h = new Hls({ liveSyncDurationCount: 2, lowLatencyMode: false });
        hls = h;
        h.loadSource(s.url);
        h.attachMedia(v);
        // Start once the playlist is in (browsers allow muted autoplay).
        h.on(Hls.Events.MANIFEST_PARSED, () => void v.play().catch(() => {}));
        // Try to recover once, as the video player does, before giving up.
        let recovered = false;
        h.on(Hls.Events.ERROR, (_, d) => {
          if (!d.fatal) return;
          if (!recovered && d.type === Hls.ErrorTypes.NETWORK_ERROR) {
            recovered = true;
            h.startLoad();
          } else if (!recovered && d.type === Hls.ErrorTypes.MEDIA_ERROR) {
            recovered = true;
            h.recoverMediaError();
          } else onError?.("The stream stopped.");
        });
      } else {
        v.src = s.url; // Safari plays HLS itself
        v.play().catch(() => {});
      }
    })()
      .catch((e: Error) => {
        if (!cancelled) onError?.(e.message);
      })
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
      hls?.destroy();
      if (v?.getAttribute("src")) {
        v.removeAttribute("src");
        v.load();
      }
      if (session) stop(session);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [channelId]);
  return (
    <div className={`relative bg-black ${className ?? ""}`}>
      <video
        ref={video}
        className="size-full object-contain"
        muted={muted}
        controls={controls}
        playsInline
        autoPlay
        aria-label="Live TV"
      />
      {loading && (
        <div className="absolute inset-0 grid place-items-center text-sm text-muted">
          Tuning…
        </div>
      )}
    </div>
  );
}
