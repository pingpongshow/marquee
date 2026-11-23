/** Whether a page address is one of the players: library video, a film or episode on demand, or Live TV. */
export function isPlayerPath(pathname: string): boolean {
  return (
    pathname.startsWith("/play/") ||
    pathname.startsWith("/livetv/watch/") ||
    /^\/vod\/[^/]+\/play$/.test(pathname) ||
    /^\/vod\/shows\/[^/]+\/play\/[^/]+$/.test(pathname)
  );
}
