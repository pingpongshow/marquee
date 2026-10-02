import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { settingsQuery, useUpdateSettings } from "@/api/queries";
/**
 * Local editable copy of one settings section. Saving PATCHes only that section,
 * so two admins editing different sections never overwrite each other.
 */
export function useSectionDraft(key, toUpdate) {
    const query = useQuery(settingsQuery);
    const mutation = useUpdateSettings();
    const server = query.data?.[key];
    const [draft, setDraft] = useState(server);
    const [savedAt, setSavedAt] = useState(null);
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
        update: (patch) => setDraft((d) => (d ? { ...d, ...patch } : d)),
        dirty,
        reset: () => setDraft(server),
        save: () => {
            if (!draft)
                return;
            const body = { [key]: toUpdate ? toUpdate(draft) : draft };
            mutation.mutate(body, { onSuccess: () => setSavedAt(Date.now()) });
        },
        saving: mutation.isPending,
        error: mutation.error,
        savedAt,
    };
}
