import Hls from "hls.js";
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
    let hls: Hls | null = null;
    let session: string | null = null;
    let cancelled = false;
    setLoading(true);
    onError?.(null);
    unwrap(
      api.POST("/livetv/channels/{channelId}/play", {
        params: { path: { channelId } },
        body: { profile: deviceProfile() },
      }),
    )
      .then((s) => {
        session = s.id;
        if (cancelled) return;
        const v = video.current;
        if (!v) return;
        if (Hls.isSupported()) {
          hls = new Hls({ liveSyncDurationCount: 2, lowLatencyMode: false });
          hls.loadSource(s.url);
          hls.attachMedia(v);
          // Start once the playlist is in (browsers allow muted autoplay).
          hls.on(
            Hls.Events.MANIFEST_PARSED,
            () => void v.play().catch(() => {}),
          );
          hls.on(Hls.Events.ERROR, (_, d) => {
            if (d.fatal) onError?.("The stream stopped.");
          });
        } else {
          v.src = s.url; // Safari plays HLS itself
        }
        if (!Hls.isSupported()) v.play().catch(() => {});
      })
      .catch((e: Error) => {
        if (!cancelled) onError?.(e.message);
      })
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
      hls?.destroy();
      if (session)
        void api.DELETE("/livetv/sessions/{sessionId}", {
          params: { path: { sessionId: session } },
        });
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
