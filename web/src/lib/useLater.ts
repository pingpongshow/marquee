import { useCallback, useEffect, useRef } from "react";

/**
 * A timer that goes with the component: a dialog that closes itself a moment after it's done
 * mustn't, once closed by hand, close the next one opened.
 */
export function useLater() {
  const timer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  return useCallback((fn: () => void, ms: number) => {
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(fn, ms);
  }, []);
}
