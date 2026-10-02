import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { Alert, Button, Card, Field, Input } from "@/components/ui";

/** Send music plays to ListenBrainz (MUSIC-12), from every app, via the server. */
export function ListenBrainzCard() {
  const qc = useQueryClient();
  const status = useQuery({
    queryKey: ["listenbrainz"],
    queryFn: () => unwrap(api.GET("/me/scrobbling/listenbrainz")),
  });
  const [token, setToken] = useState("");
  const done = () => {
    setToken("");
    qc.invalidateQueries({ queryKey: ["listenbrainz"] });
  };
  const connect = useMutation({
    mutationFn: () =>
      unwrap(api.PUT("/me/scrobbling/listenbrainz", { body: { token } })),
    onSuccess: done,
  });
  const disconnect = useMutation({
    mutationFn: () => unwrap(api.DELETE("/me/scrobbling/listenbrainz")),
    onSuccess: done,
  });
  const s = status.data;
  return (
    <Card
      title="ListenBrainz"
      description="Share what you listen to on ListenBrainz. Tracks you play halfway or more on any device are sent as listens."
    >
      {s?.connected ? (
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
            <Alert tone="error">Last listen wasn't sent: {s.error}</Alert>
          )}
        </div>
      ) : (
        <div className="space-y-3">
          <Field
            label="User token"
            help="Find it at listenbrainz.org → Settings → User token."
          >
            {(id) => (
              <Input
                id={id}
                value={token}
                autoComplete="off"
                onChange={(e) => setToken(e.target.value)}
              />
            )}
          </Field>
          <Button
            variant="primary"
            disabled={!token.trim()}
            loading={connect.isPending}
            onClick={() => connect.mutate()}
          >
            Connect
          </Button>
          {connect.error && <Alert tone="error">{connect.error.message}</Alert>}
        </div>
      )}
    </Card>
  );
}
