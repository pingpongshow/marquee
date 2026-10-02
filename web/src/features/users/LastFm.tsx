import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { api, unwrap } from "@/api/client";
import { Alert, Button, Card } from "@/components/ui";

/**
 * Send music plays to Last.fm (MUSIC-12). Connect goes to last.fm to approve Marquee, which
 * comes back here with ?token=…, exchanged on the server for a session.
 */
export function LastFmCard() {
  const qc = useQueryClient();
  const status = useQuery({
    queryKey: ["lastfm"],
    queryFn: () => unwrap(api.GET("/me/scrobbling/lastfm")),
  });
  const refresh = () => qc.invalidateQueries({ queryKey: ["lastfm"] });
  const start = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/me/scrobbling/lastfm", {
          body: { callbackUrl: window.location.origin + "/account" },
        }),
      ),
    onSuccess: (r) => window.location.assign(r.url),
  });
  const finish = useMutation({
    mutationFn: (token: string) =>
      unwrap(api.PUT("/me/scrobbling/lastfm", { body: { token } })),
    onSuccess: refresh,
  });
  const disconnect = useMutation({
    mutationFn: () => unwrap(api.DELETE("/me/scrobbling/lastfm")),
    onSuccess: refresh,
  });
  // Back from last.fm with a token: finish connecting, then tidy the address.
  const handled = useRef(false);
  useEffect(() => {
    const token = new URLSearchParams(window.location.search).get("token");
    if (!token || handled.current) return;
    handled.current = true;
    finish.mutate(token);
    window.history.replaceState(null, "", window.location.pathname);
  }, [finish]);
  const s = status.data;
  if (!s) return null;
  return (
    <Card
      title="Last.fm"
      description="Scrobble what you listen to on any device to your Last.fm profile."
    >
      {!s.available ? (
        <p className="text-sm text-muted">
          Last.fm isn't set up on this server yet. An admin can add it in
          Settings → Music.
        </p>
      ) : s.connected ? (
        <div className="flex flex-wrap items-center gap-3">
          <span className="text-sm">
            Connected as <span className="font-medium">{s.username}</span>
          </span>
          <Button
            variant="ghost"
            loading={disconnect.isPending}
            onClick={() => disconnect.mutate()}
          >
            Disconnect
          </Button>
          {s.error && (
            <Alert tone="error">Last scrobble wasn't sent: {s.error}</Alert>
          )}
        </div>
      ) : (
        <Button
          variant="primary"
          loading={start.isPending || finish.isPending}
          onClick={() => start.mutate()}
        >
          Connect Last.fm
        </Button>
      )}
      {(start.error || finish.error) && (
        <Alert tone="error">{(start.error ?? finish.error)?.message}</Alert>
      )}
    </Card>
  );
}
