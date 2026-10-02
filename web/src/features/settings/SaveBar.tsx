import { useEffect, useState } from "react";
import { Alert, Button } from "@/components/ui";

/** Sticky footer shown while a settings section has unsaved changes. */
export function SaveBar({ dirty, saving, error, savedAt, onSave, onReset }: { dirty: boolean; saving: boolean; error: Error | null; savedAt: number | null; onSave: () => void; onReset: () => void }) {
  // "Saved" shows for a moment after each save, then hides.
  const [hiddenFor, setHiddenFor] = useState<number | null>(null);
  const showSaved = savedAt !== null && hiddenFor !== savedAt;
  useEffect(() => {
    if (!savedAt) return;
    const t = setTimeout(() => setHiddenFor(savedAt), 2500);
    return () => clearTimeout(t);
  }, [savedAt]);

  if (!dirty && !showSaved && !error) return null;

  return (
    <div className="sticky bottom-0 -mx-6 mt-6 space-y-3 border-t border-border bg-bg/95 px-6 py-4 backdrop-blur lg:-mx-8 lg:px-8">
      {error && <Alert tone="error">{error.message}</Alert>}
      <div className="flex items-center justify-end gap-3">
        {showSaved && !dirty && <span className="text-sm text-success" role="status">Saved</span>}
        {dirty && <span className="mr-auto text-sm text-muted">You have unsaved changes</span>}
        <Button variant="ghost" onClick={onReset} disabled={!dirty || saving}>
          Discard
        </Button>
        <Button variant="primary" onClick={onSave} disabled={!dirty} loading={saving}>
          Save changes
        </Button>
      </div>
    </div>
  );
}
