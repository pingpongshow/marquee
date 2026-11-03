import { useQuery } from "@tanstack/react-query";
import { useEffect, useSyncExternalStore } from "react";
import { appearanceQuery, applyLook, type Look } from "./apply";

// The admin's unsaved choice, shown in their own tab only until Save.
let preview: Look | null = null;
const listeners = new Set<() => void>();

export function setAppearancePreview(look: Look | null) {
  if (JSON.stringify(look) === JSON.stringify(preview)) return;
  preview = look;
  listeners.forEach((fn) => fn());
}

function subscribe(fn: () => void) {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}

/** Keeps the page on what the server says (USER-20): every web user sees the admin's choice. */
export function AppearanceSync() {
  const { data } = useQuery(appearanceQuery);
  const previewing = useSyncExternalStore(subscribe, () => preview);
  useEffect(() => {
    if (previewing) applyLook(previewing, false);
    else if (data) applyLook({ themeId: data.activeThemeId, background: data.background }, true);
  }, [data, previewing]);
  return null;
}
