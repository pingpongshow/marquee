import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { settingsQuery, useUpdateSettings } from "@/api/queries";
import type { ServerSettings, ServerSettingsUpdate } from "@/api/types";

type SectionKey = keyof ServerSettings & keyof ServerSettingsUpdate;

/**
 * Local editable copy of one settings section. Saving PATCHes only that section,
 * so two admins editing different sections never overwrite each other.
 */
export function useSectionDraft<K extends SectionKey>(key: K, toUpdate?: (d: ServerSettings[K]) => ServerSettingsUpdate[K]) {
  const query = useQuery(settingsQuery);
  const mutation = useUpdateSettings();
  const server = query.data?.[key];
  const [draft, setDraft] = useState<ServerSettings[K] | undefined>(server);
  const [savedAt, setSavedAt] = useState<number | null>(null);

  // Adopt fresh server data whenever it changes (initial load, after save).
  const [seen, setSeen] = useState(server);
  if (server !== seen) {
    setSeen(server);
    setDraft(server);
  }

  const dirty = draft !== undefined && JSON.stringify(draft) !== JSON.stringify(server);

  return {
    query,
    draft,
    update: (patch: Partial<ServerSettings[K]>) => setDraft((d) => (d ? { ...d, ...patch } : d)),
    dirty,
    reset: () => setDraft(server),
    save: () => {
      if (!draft) return;
      const body = { [key]: toUpdate ? toUpdate(draft) : draft } as ServerSettingsUpdate;
      mutation.mutate(body, { onSuccess: () => setSavedAt(Date.now()) });
    },
    saving: mutation.isPending,
    error: mutation.error,
    savedAt,
  };
}
