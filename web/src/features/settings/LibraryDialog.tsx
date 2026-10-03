import { clsx } from "clsx";
import { Folder, X } from "lucide-react";
import { useState } from "react";
import { useCreateLibrary, useUpdateLibrary } from "@/api/queries";
import type { Library, LibraryOptions, LibraryType } from "@/api/types";
import { libraryIcons } from "@/app/Shell";
import { Alert, Button, Dialog, Field, Input, Select, StringListEditor, Toggle } from "@/components/ui";
import { FolderBrowser } from "./FolderBrowser";
import { languages } from "./ServerSections";

const types: { type: LibraryType; label: string; help: string }[] = [
  { type: "movies", label: "Movies", help: "Movie (Year)/Movie (Year).mkv" },
  { type: "shows", label: "TV Shows", help: "Show/Season 01/Show - S01E01.mkv" },
  { type: "anime", label: "Anime", help: "TV layout with anime metadata and episode numbering" },
  { type: "music", label: "Music", help: "Organised by embedded tags" },
  { type: "videos", label: "Other Videos", help: "Home and personal videos, organised by folder" },
];

const defaultNames: Record<LibraryType, string> = { movies: "Movies", shows: "TV Shows", anime: "Anime", music: "Music", videos: "Home Videos", photos: "Photos" };

type Step = "type" | "folders" | "advanced";

/** Add (library undefined) or edit a library, in three steps: type → folders → advanced. */
export function LibraryDialog({ open, onClose, library }: { open: boolean; onClose: () => void; library?: Library }) {
  const editing = !!library;
  const [step, setStep] = useState<Step>(editing ? "folders" : "type");
  const [type, setType] = useState<LibraryType>(library?.type ?? "movies");
  const [name, setName] = useState(library?.name ?? defaultNames.movies);
  const [nameTouched, setNameTouched] = useState(editing);
  const [paths, setPaths] = useState<string[]>(library?.paths ?? []);
  const [browsing, setBrowsing] = useState(!editing);
  const [opts, setOpts] = useState<LibraryOptions>(library?.options ?? {});
  const create = useCreateLibrary();
  const update = useUpdateLibrary();
  const mutation = editing ? update : create;
  const isTV = type === "shows" || type === "anime";

  const submit = () => {
    const options: LibraryOptions = { ...opts };
    if (!isTV) delete options.episodeOrdering;
    if (editing) update.mutate({ id: library.id, body: { name, paths, options } }, { onSuccess: onClose });
    else
      // The server queues the first scan itself.
      create.mutate({ name, type, paths, options }, { onSuccess: onClose });
  };

  const steps: { id: Step; label: string }[] = [
    ...(editing ? [] : [{ id: "type" as const, label: "Type" }]),
    { id: "folders", label: "Folders" },
    { id: "advanced", label: "Advanced" },
  ];

  const footer = (
    <>
      <Button variant="ghost" onClick={onClose}>
        Cancel
      </Button>
      {step === "type" && (
        <Button variant="primary" onClick={() => setStep("folders")} disabled={!name.trim()}>
          Next
        </Button>
      )}
      {step === "folders" && !editing && (
        <Button variant="secondary" onClick={() => setStep("advanced")} disabled={paths.length === 0}>
          Advanced
        </Button>
      )}
      {(step === "advanced" || step === "folders") && (
        <Button variant="primary" onClick={submit} loading={mutation.isPending} disabled={paths.length === 0 || !name.trim()}>
          {editing ? "Save changes" : "Add library"}
        </Button>
      )}
    </>
  );

  return (
    <Dialog open={open} onClose={onClose} title={editing ? `Edit ${library.name}` : "Add library"} footer={footer} wide>
      <div className="mb-5 flex gap-1 border-b border-border">
        {steps.map((s) => (
          <button
            key={s.id}
            type="button"
            onClick={() => (s.id === "type" || name.trim()) && setStep(s.id)}
            className={clsx("-mb-px border-b-2 px-3 py-2 text-sm", step === s.id ? "border-accent font-medium text-text" : "border-transparent text-muted hover:text-text")}
          >
            {s.label}
          </button>
        ))}
      </div>

      {mutation.isError && (
        <div className="mb-4">
          <Alert tone="error">{mutation.error.message}</Alert>
        </div>
      )}

      {step === "type" && (
        <div className="space-y-5">
          <div className="grid gap-2 sm:grid-cols-2">
            {types.map((t) => {
              const Icon = libraryIcons[t.type];
              return (
                <button
                  key={t.type}
                  type="button"
                  onClick={() => {
                    setType(t.type);
                    if (!nameTouched) setName(defaultNames[t.type]);
                  }}
                  aria-pressed={type === t.type}
                  className={clsx(
                    "flex items-start gap-3 rounded-lg border p-3 text-left transition-colors",
                    type === t.type ? "border-accent bg-accent/10" : "border-border hover:bg-surface-2",
                  )}
                >
                  <Icon className={clsx("mt-0.5 size-5", type === t.type ? "text-accent" : "text-muted")} aria-hidden />
                  <div>
                    <div className="text-sm font-medium">{t.label}</div>
                    <div className="text-xs text-muted">{t.help}</div>
                  </div>
                </button>
              );
            })}
          </div>
          <Field label="Library name">
            {(id) => (
              <Input
                id={id}
                maxLength={64}
                value={name}
                onChange={(e) => {
                  setName(e.target.value);
                  setNameTouched(true);
                }}
              />
            )}
          </Field>
          <Field label="Language" help="For titles, summaries and artwork.">
            {(id) => (
              <Select id={id} value={opts.language ?? "en-US"} onChange={(e) => setOpts({ ...opts, language: e.target.value })}>
                {languages.map(([v, l]) => (
                  <option key={v} value={v}>
                    {l}
                  </option>
                ))}
              </Select>
            )}
          </Field>
        </div>
      )}

      {step === "folders" && (
        <div className="space-y-4">
          {editing && (
            <Field label="Library name">
              {(id) => <Input id={id} maxLength={64} value={name} onChange={(e) => setName(e.target.value)} />}
            </Field>
          )}
          <div>
            <div className="mb-2 text-sm font-medium">Folders</div>
            {paths.length === 0 ? (
              <p className="text-sm text-muted">Choose at least one folder that contains your media.</p>
            ) : (
              <ul className="space-y-1.5">
                {paths.map((p) => (
                  <li key={p} className="flex items-center gap-2 rounded-md bg-surface-2 px-3 py-2">
                    <Folder className="size-4 text-accent" aria-hidden />
                    <span className="flex-1 truncate font-mono text-sm">{p}</span>
                    <button type="button" className="rounded p-1 text-muted hover:text-text" aria-label={`Remove ${p}`} onClick={() => setPaths(paths.filter((x) => x !== p))}>
                      <X className="size-4" />
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
          {browsing ? (
            <FolderBrowser
              onPick={(p) => {
                if (!paths.includes(p)) setPaths([...paths, p]);
                setBrowsing(false);
              }}
            />
          ) : (
            <Button variant="secondary" size="sm" onClick={() => setBrowsing(true)}>
              Browse for folder
            </Button>
          )}
        </div>
      )}

      {step === "advanced" && (
        <div className="space-y-5">
          {isTV && (
            <Field label="Episode ordering" help={type === "anime" ? "Anime is often numbered absolutely (1…n) rather than by season." : undefined}>
              {(id) => (
                <Select id={id} value={opts.episodeOrdering ?? "aired"} onChange={(e) => setOpts({ ...opts, episodeOrdering: e.target.value as LibraryOptions["episodeOrdering"] })}>
                  <option value="aired">Aired (by season)</option>
                  <option value="absolute">Absolute</option>
                  <option value="dvd">DVD</option>
                </Select>
              )}
            </Field>
          )}
          <Toggle label="Show on Home" help="Include this library’s hubs (Recently Added, Continue Watching) on the Home screen." checked={opts.includeInHome ?? true} onChange={(v) => setOpts({ ...opts, includeInHome: v })} />
          <Toggle label="Include in search" checked={opts.includeInSearch ?? true} onChange={(v) => setOpts({ ...opts, includeInSearch: v })} />
          <Field label="Full rescan every (hours)" help="New files are picked up instantly by file watching; this is a safety net. 0 = never.">
            {(id) => <Input id={id} type="number" min={0} value={opts.scanIntervalHours ?? 24} onChange={(e) => setOpts({ ...opts, scanIntervalHours: Number(e.target.value) })} />}
          </Field>
          <Field label="Ignore files matching" help="Added to the server-wide ignore patterns. Example: *_staging.*">
            {() => <StringListEditor values={opts.ignorePatterns ?? []} onChange={(v) => setOpts({ ...opts, ignorePatterns: v })} placeholder="*.sample.*" addLabel="Add pattern" />}
          </Field>
        </div>
      )}
    </Dialog>
  );
}
