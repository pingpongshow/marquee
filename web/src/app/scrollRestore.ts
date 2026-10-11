import { useRouter } from "@tanstack/react-router";
import { useEffect, type RefObject } from "react";

const STORE = "marquee.scroll";
const RESTORE_MS = 4000;

function load(): Record<string, number> {
  try {
    return JSON.parse(sessionStorage.getItem(STORE) ?? "{}") as Record<string, number>;
  } catch {
    return {};
  }
}

function save(positions: Record<string, number>) {
  try {
    // Only the most recent entries matter.
    const keys = Object.keys(positions);
    for (const k of keys.slice(0, Math.max(0, keys.length - 200))) delete positions[k];
    sessionStorage.setItem(STORE, JSON.stringify(positions));
  } catch {
    /* private mode or full: positions just aren't kept across reloads */
  }
}

type Loc = { href: string; state: { __TSR_key?: string; key?: string } };
const entryKey = (l: Loc) => l.state.__TSR_key ?? l.state.key ?? l.href;

/**
 * Remembers the scroll position of the app's scrolling `<main>` for each history entry, and
 * puts it back when the person goes Back or Forward to that entry. Pages that load as they
 * scroll (virtualized library grids) grow after the navigation, so the position is re-applied
 * for a few seconds until it holds, or until the person scrolls themselves. A new page starts
 * at the top.
 */
export function useMainScrollRestoration(main: RefObject<HTMLElement | null>) {
  const router = useRouter();
  useEffect(() => {
    const el = main.current;
    if (!el) return;
    const history = router.history;
    const positions = load();
    let key = entryKey(history.location as Loc);
    let raf = 0;
    let saveTimer = 0;

    const stopRestore = () => {
      cancelAnimationFrame(raf);
      raf = 0;
    };
    const onScroll = () => {
      positions[key] = el.scrollTop;
      window.clearTimeout(saveTimer);
      saveTimer = window.setTimeout(() => save(positions), 250);
    };
    const restore = (target: number) => {
      stopRestore();
      const until = performance.now() + RESTORE_MS;
      let held = 0;
      const step = () => {
        if (Math.abs(el.scrollTop - target) > 1) {
          el.scrollTop = target;
          held = 0;
        } else held++;
        // Done once it has stayed put for a few frames, or when time is up.
        if (held < 10 && performance.now() < until) raf = requestAnimationFrame(step);
        else raf = 0;
      };
      raf = requestAnimationFrame(step);
    };
    const unsubscribe = history.subscribe(({ location, action }) => {
      key = entryKey(location as Loc);
      if (action.type === "PUSH") {
        stopRestore();
        requestAnimationFrame(() => (el.scrollTop = 0));
      } else if (action.type !== "REPLACE") {
        restore(positions[key] ?? 0);
      }
    });
    // The person scrolling takes over from a restore in progress.
    const userScroll = () => stopRestore();
    el.addEventListener("scroll", onScroll, { passive: true });
    el.addEventListener("wheel", userScroll, { passive: true });
    el.addEventListener("touchstart", userScroll, { passive: true });
    el.addEventListener("keydown", userScroll);
    return () => {
      unsubscribe();
      stopRestore();
      window.clearTimeout(saveTimer);
      el.removeEventListener("scroll", onScroll);
      el.removeEventListener("wheel", userScroll);
      el.removeEventListener("touchstart", userScroll);
      el.removeEventListener("keydown", userScroll);
    };
  }, [main, router]);
}
