const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);

/**
 * A form's unsaved draft carried over to newer server data: the fields changed since `base`
 * (what the draft started from, or what was just saved) stay as typed, and every other field
 * takes the server's value. So a change saved elsewhere on the page, another admin's, or the
 * answer to this form's own save doesn't wipe what's still being edited; and what was saved
 * (a pasted key the server doesn't send back, say) leaves the draft.
 */
export function rebaseDraft<T>(draft: T, base: T | undefined, server: T): T {
  if (!draft || !base || !server || typeof draft !== "object" || typeof server !== "object") return server;
  const d = draft as Record<string, unknown>;
  const b = base as Record<string, unknown>;
  const out: Record<string, unknown> = { ...(server as Record<string, unknown>) };
  for (const k of new Set([...Object.keys(d), ...Object.keys(b)])) if (!same(d[k], b[k])) out[k] = d[k];
  return out as T;
}
