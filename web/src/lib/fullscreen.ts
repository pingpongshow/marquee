/**
 * Full screen for the video player on every browser. Most take the player's own element full
 * screen, controls and subtitles included. Safari on iPhone can't: only a <video> goes full
 * screen there, in the system's own player (webkitEnterFullscreen). Older iPad Safari has only
 * the prefixed webkitRequestFullscreen. A phone held upright turns to landscape where the
 * browser allows it (Android).
 */

type WebkitDocument = Document & {
  webkitFullscreenElement?: Element | null;
  webkitExitFullscreen?: () => void;
  webkitFullscreenEnabled?: boolean;
};
type WebkitElement = HTMLElement & { webkitRequestFullscreen?: () => void };
type WebkitVideo = HTMLVideoElement & {
  webkitEnterFullscreen?: () => void;
  webkitExitFullscreen?: () => void;
  webkitDisplayingFullscreen?: boolean;
  webkitSupportsFullscreen?: boolean;
};

const doc = () => document as WebkitDocument;

/** Whether the page (or, on iPhone, the video) is full screen now. */
export function isFullscreen(video?: HTMLVideoElement | null): boolean {
  const d = doc();
  return !!(d.fullscreenElement || d.webkitFullscreenElement || (video as WebkitVideo | null)?.webkitDisplayingFullscreen);
}

/** Whether there's any way to go full screen here (the button shows only then). */
export function canFullscreen(root?: HTMLElement | null, video?: HTMLVideoElement | null): boolean {
  const d = doc();
  if (d.fullscreenEnabled && typeof root?.requestFullscreen === "function") return true;
  if (d.webkitFullscreenEnabled && typeof (root as WebkitElement | null)?.webkitRequestFullscreen === "function") return true;
  return typeof (video as WebkitVideo | null)?.webkitEnterFullscreen === "function";
}

/** Goes full screen, or back out of it. */
export function toggleFullscreen(root: HTMLElement | null, video: HTMLVideoElement | null) {
  if (isFullscreen(video)) {
    exitFullscreen(video);
    return;
  }
  const d = doc();
  const el = root as WebkitElement | null;
  if (el && d.fullscreenEnabled && typeof el.requestFullscreen === "function") {
    el.requestFullscreen({ navigationUI: "hide" })
      .then(() => landscape())
      .catch(() => enterVideo(video));
    return;
  }
  if (el && d.webkitFullscreenEnabled && typeof el.webkitRequestFullscreen === "function") {
    el.webkitRequestFullscreen();
    return;
  }
  enterVideo(video);
}

export function exitFullscreen(video?: HTMLVideoElement | null) {
  const d = doc();
  if (d.fullscreenElement) void d.exitFullscreen().catch(() => {});
  else if (d.webkitFullscreenElement) d.webkitExitFullscreen?.();
  else (video as WebkitVideo | null)?.webkitExitFullscreen?.();
}

/** iPhone: the video alone, in the system player. */
function enterVideo(video: HTMLVideoElement | null) {
  const v = video as WebkitVideo | null;
  try {
    v?.webkitEnterFullscreen?.();
  } catch {
    // Not before the video has loaded: nothing to do.
  }
}

function landscape() {
  const o = screen.orientation as ScreenOrientation & { lock?: (o: string) => Promise<void> };
  if (!o?.lock || !matchMedia("(pointer: coarse)").matches || window.innerWidth > window.innerHeight) return;
  void o.lock("landscape").catch(() => {});
}

/** Calls onChange whenever full screen starts or ends, the iPhone's video player included. */
export function onFullscreenChange(video: HTMLVideoElement | null, onChange: () => void): () => void {
  const events: [EventTarget, string][] = [
    [document, "fullscreenchange"],
    [document, "webkitfullscreenchange"],
  ];
  if (video) events.push([video, "webkitbeginfullscreen"], [video, "webkitendfullscreen"]);
  for (const [t, e] of events) t.addEventListener(e, onChange);
  return () => {
    for (const [t, e] of events) t.removeEventListener(e, onChange);
  };
}
