import type { RemotePlayerState } from "@/api/types";

/** What a controllable player can be told to do (USER-14). */
export type RemoteTarget = {
  state: () => RemotePlayerState;
  pause: () => void;
  resume: () => void;
  seek: (ms: number) => void;
  stop: () => void;
  next: () => void;
  previous: () => void;
  setAudio?: (streamId: number) => void;
  setSubtitle?: (streamId: number) => void;
  setVolume?: (v: number) => void;
};

let video: RemoteTarget | null = null;
const listeners = new Set<() => void>();

/** The video player registers itself while it's open; it takes precedence over music. */
export function setVideoTarget(t: RemoteTarget | null) {
  video = t;
  remoteStateChanged();
}

export function videoTarget() {
  return video;
}

/** Tells the remote host that play, pause, a seek or the item changed, so it reports at once. */
export function remoteStateChanged() {
  listeners.forEach((l) => l());
}

export function onRemoteStateChanged(fn: () => void) {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}
