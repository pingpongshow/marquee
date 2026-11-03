import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { clsx } from "clsx";
import { Check, ImagePlus, Trash2 } from "lucide-react";
import { useEffect, useRef, useState, type CSSProperties } from "react";
import { api, ApiError, session, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Alert, Button, Card, Spinner, Toggle } from "@/components/ui";
import { toast } from "@/components/Toast";
import { appearanceQuery, lookVars, type Appearance } from "@/features/appearance/apply";
import { setAppearancePreview } from "@/features/appearance/AppearanceSync";
import { themeById, themeGroups, themePattern, themes, themeVars, type Theme } from "@/features/appearance/themes";
import { SaveBar } from "./SaveBar";

type Update = Required<components["schemas"]["AppearanceUpdate"]>;

const MAX_UPLOAD = 20 << 20;

function toDraft(a: Appearance): Update {
  return { themeId: a.themeId, autoHolidays: a.autoHolidays, backgroundMode: a.background.mode, backgroundDim: a.background.dim };
}

async function sendBackground(method: "PUT" | "DELETE", body?: Blob): Promise<Appearance> {
  const res = await fetch("/api/v1/appearance/background", {
    method,
    body,
    headers: {
      Authorization: `Bearer ${session.token}`,
      ...(body ? { "Content-Type": "application/octet-stream" } : {}),
    },
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, data.code ?? "error", data.message ?? res.statusText);
  return data as Appearance;
}

/** A small page in the theme's own colours: its variables apply inside this block only. */
function ThemeTile({ theme, selected, onSelect }: { theme: Theme; selected: boolean; onSelect: () => void }) {
  const style = { ...themeVars(theme), fontFamily: theme.font, backgroundImage: themePattern(theme), backgroundSize: theme.pattern ? "50%" : undefined } as CSSProperties;
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-pressed={selected}
      data-theme-tile={theme.id}
      className={clsx("group overflow-hidden rounded-lg border text-left", selected ? "border-accent ring-2 ring-accent" : "border-border hover:border-muted")}
    >
      <span className="block space-y-1.5 bg-bg p-2 text-text" style={style} aria-hidden>
        <span className="flex items-center gap-1 rounded-sm bg-surface px-1.5 py-1">
          <span className="size-1.5 rounded-full bg-accent" />
          <span className="h-1 w-8 rounded-full bg-muted" />
        </span>
        <span className="block rounded-md border border-border bg-surface p-1.5">
          <span className="block text-xs leading-tight font-semibold">Aa</span>
          <span className="mt-1 block h-1 w-3/4 rounded-full bg-muted" />
          <span className="mt-1.5 inline-flex rounded-md bg-accent px-1.5 py-0.5 text-[9px] leading-tight font-medium text-on-accent">Play</span>
        </span>
      </span>
      <span className="flex items-center gap-1 bg-surface-2 px-2 py-1.5 text-xs font-medium">
        <span className="min-w-0 flex-1 truncate">{theme.name}</span>
        {selected && <Check className="size-3.5 shrink-0 text-accent" aria-label="Chosen" />}
      </span>
    </button>
  );
}

/** The server's errors start in lower case. */
const sentence = (message: string) => message.charAt(0).toUpperCase() + message.slice(1) + ".";

function dimLabel(dim: number) {
  return dim === 0 ? "As uploaded" : `${dim}% towards the theme's background`;
}

/** Admin-set look of the web app for everyone (USER-20). The native apps keep theirs. */
export function AppearanceSettings() {
  const qc = useQueryClient();
  const query = useQuery(appearanceQuery);
  const server = query.data;
  const [draft, setDraft] = useState<Update | undefined>(server && toDraft(server));
  const [savedAt, setSavedAt] = useState<number | null>(null);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const file = useRef<HTMLInputElement>(null);

  // Adopt fresh server data whenever it changes (initial load, after save, another admin).
  const [seen, setSeen] = useState(server && JSON.stringify(toDraft(server)));
  const fresh = server && JSON.stringify(toDraft(server));
  if (fresh !== seen) {
    setSeen(fresh);
    setDraft(server && toDraft(server));
  }
  const dirty = !!draft && JSON.stringify(draft) !== fresh;

  // Unsaved choices show in this tab only, and end on leaving the page.
  useEffect(() => {
    if (!server || !draft || !dirty) {
      setAppearancePreview(null);
      return;
    }
    setAppearancePreview({
      // While a holiday's theme is showing, only a different choice is worth previewing.
      themeId: draft.themeId !== server.themeId ? draft.themeId : server.activeThemeId,
      background: { imageUrl: server.background.imageUrl, mode: draft.backgroundMode, dim: draft.backgroundDim },
    });
  }, [server, draft, dirty]);
  useEffect(() => () => setAppearancePreview(null), []);

  const save = useMutation({
    mutationFn: (body: Update) => unwrap(api.PUT("/appearance", { body })),
    onSuccess: (data) => {
      qc.setQueryData(appearanceQuery.queryKey, data);
      setSavedAt(Date.now());
    },
  });
  const upload = useMutation({
    mutationFn: (f: File) => sendBackground("PUT", f),
    onMutate: () => setUploadError(null),
    onSuccess: (data) => qc.setQueryData(appearanceQuery.queryKey, data),
    onError: (e) => setUploadError(sentence(e.message)),
  });
  const remove = useMutation({
    mutationFn: () => sendBackground("DELETE"),
    onMutate: () => setUploadError(null),
    onSuccess: (data) => {
      qc.setQueryData(appearanceQuery.queryKey, data);
      toast("Background removed");
    },
    onError: (e) => setUploadError(e.message),
  });

  if (query.isPending) return <Spinner />;
  if (!server || !draft) return <Alert tone="error">{query.error?.message ?? "Couldn't load the appearance."}</Alert>;

  const set = (patch: Partial<Update>) => setDraft((d) => (d ? { ...d, ...patch } : d));
  const image = server.background.imageUrl;
  const previewVars = lookVars({ themeId: draft.themeId, background: { imageUrl: image, mode: draft.backgroundMode, dim: draft.backgroundDim } });
  const holiday = server.holidayId ? themeById(server.holidayId) : null;

  return (
    <div className="space-y-6">
      <Alert>These apply to the web app for everyone, with no way to change them per person. The native apps keep their own look.</Alert>
      <Card title="Theme" description="Pick one to preview it in this tab. Nobody else sees it until you save.">
        <Toggle
          label="Automatic holidays"
          help="Around each holiday its theme shows for everyone, then your chosen theme returns. Dates follow the server's time zone."
          checked={draft.autoHolidays}
          onChange={(autoHolidays) => set({ autoHolidays })}
        />
        {holiday && (
          <Alert>
            The {holiday.name} theme is showing now. {themeById(server.themeId).name} returns when the holiday ends.
          </Alert>
        )}
        {themeGroups.map((g) => (
          <div key={g.id}>
            <h3 className="text-sm font-semibold">{g.label}</h3>
            {g.help && <p className="mt-0.5 text-sm text-muted">{g.help}</p>}
            <div className="mt-2 grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4">
              {themes
                .filter((t) => t.group === g.id)
                .map((t) => (
                  <ThemeTile key={t.id} theme={t} selected={draft.themeId === t.id} onSelect={() => set({ themeId: t.id })} />
                ))}
            </div>
          </div>
        ))}
      </Card>

      <Card
        title="Background"
        description="An image behind the main content, typically a pattern. It replaces the theme's own background, whichever theme is showing; the sidebar and bars stay solid."
      >
        <div className="flex flex-wrap items-center gap-4">
          <div
            className="h-24 w-40 shrink-0 rounded-md border border-border bg-bg"
            data-testid="background-preview"
            style={{
              backgroundImage: previewVars["--page-layers"],
              backgroundRepeat: previewVars["--page-repeat"],
              backgroundSize: previewVars["--page-size"],
              backgroundPosition: previewVars["--page-position"],
            }}
          />
          <div className="flex flex-wrap items-center gap-2">
            <input
              ref={file}
              type="file"
              accept="image/png,image/jpeg,image/webp"
              className="hidden"
              aria-label="Background image"
              onChange={(e) => {
                const f = e.target.files?.[0];
                e.target.value = "";
                if (!f) return;
                if (f.size > MAX_UPLOAD) setUploadError("That image is larger than 20 MB.");
                else upload.mutate(f);
              }}
            />
            <Button onClick={() => file.current?.click()} loading={upload.isPending}>
              <ImagePlus className="size-4" aria-hidden /> {image ? "Replace image" : "Upload image"}
            </Button>
            {image && (
              <Button variant="ghost" onClick={() => remove.mutate()} loading={remove.isPending}>
                <Trash2 className="size-4" aria-hidden /> Remove
              </Button>
            )}
          </div>
        </div>
        <p className="text-sm text-muted">PNG, JPEG or WebP, up to 20 MB. A new image shows for everyone straight away.</p>
        {uploadError && <Alert tone="error">{uploadError}</Alert>}
        <fieldset className="space-y-1.5">
          <legend className="text-sm font-medium">Layout</legend>
          <div className="inline-flex rounded-md border border-border p-0.5">
            {(
              [
                ["tile", "Tile"],
                ["fill", "Fill"],
              ] as const
            ).map(([mode, label]) => (
              <label key={mode} className={clsx("cursor-pointer rounded px-4 py-1.5 text-sm", draft.backgroundMode === mode ? "bg-accent font-medium text-on-accent" : "text-muted hover:text-text")}>
                <input type="radio" name="background-mode" className="sr-only" checked={draft.backgroundMode === mode} onChange={() => set({ backgroundMode: mode })} />
                {label}
              </label>
            ))}
          </div>
          <p className="text-sm text-muted">Tile repeats the image, for patterns. Fill covers the page with one copy.</p>
        </fieldset>
        <div className="space-y-1.5">
          <div className="flex items-baseline justify-between gap-3">
            <label htmlFor="background-dim" className="text-sm font-medium">
              Dim
            </label>
            <span className="text-sm text-muted tabular-nums">{dimLabel(draft.backgroundDim)}</span>
          </div>
          <input
            id="background-dim"
            type="range"
            min={0}
            max={90}
            step={5}
            value={draft.backgroundDim}
            onChange={(e) => set({ backgroundDim: Number(e.target.value) })}
            className="w-full accent-accent"
          />
          <div className="flex justify-between text-xs text-faint">
            <span>Image as uploaded</span>
            <span>Mostly the theme's background</span>
          </div>
          <p className="text-sm text-muted">Fades the image towards the background of whichever theme is showing, light or dark, holiday themes included. Adjust until the text on the page reads easily over it.</p>
        </div>
      </Card>
      <SaveBar dirty={dirty} saving={save.isPending} error={save.error} savedAt={savedAt} onSave={() => save.mutate(draft)} onReset={() => setDraft(toDraft(server))} />
    </div>
  );
}
