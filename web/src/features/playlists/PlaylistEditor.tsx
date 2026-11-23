import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useBlocker, useParams } from "@tanstack/react-router";
import { clsx } from "clsx";
import {
  ArrowDown,
  ArrowDownToLine,
  ArrowLeft,
  ArrowUp,
  ArrowUpToLine,
  Combine,
  CopyX,
  GripVertical,
  Plus,
  Save,
  Search,
  Shuffle,
  Trash2,
  Undo2,
  X,
} from "lucide-react";
import { memo, useCallback, useEffect, useMemo, useRef, useState, type DragEvent, type MouseEvent } from "react";
import { api, imageUrl, unwrap } from "@/api/client";
import { playlistQuery, searchQuery } from "@/api/queries";
import type { ItemSummary, PlaylistEntry } from "@/api/types";
import { Alert, Button, Input, Select, Spinner } from "@/components/ui";
import { ignoreShortcut } from "@/lib/keys";
import { formatDuration, formatTrackTime } from "../browse/format";
import { CombinePlaylistsDialog } from "./Combine";
import {
  artistOf,
  duplicateCount,
  matchesFilter,
  moveSelected,
  nudgeSelected,
  rangeIds,
  sameOrder,
  shuffleEntries,
  sortEntries,
  sortLabels,
  withoutDuplicates,
  type SortKey,
} from "./editor";

/** Every entry of a playlist, a page at a time (the list endpoint gives at most 2,000 at once). */
async function allEntries(playlistId: number): Promise<PlaylistEntry[]> {
  const out: PlaylistEntry[] = [];
  for (;;) {
    const page = await unwrap(
      api.GET("/playlists/{playlistId}/items", { params: { path: { playlistId }, query: { offset: out.length, limit: 2000 } } }),
    );
    out.push(...page.items);
    if (!page.items.length || out.length >= page.total) return out;
  }
}

const videoTypes = new Set(["movie", "episode", "video", "show", "season"]);
const musicTypes = new Set(["track", "album", "artist"]);

/**
 * The full playlist editor (MUSIC-29), for your own playlists: select rows (click, shift-click
 * for a range, Ctrl or ⌘-click to add one), then remove them or move them (drag, or the move
 * buttons); sort, shuffle, take out duplicates, filter, and add from the library. Changes are a
 * draft, with undo, until Save sends them in one go. Other playlists can be added to the end of
 * it (Combine).
 */
export function PlaylistEditorPage() {
  const { playlistId } = useParams({ from: "/playlist/$playlistId/edit" });
  const id = Number(playlistId);
  const qc = useQueryClient();
  const pl = useQuery(playlistQuery(id));
  const entries = useQuery({ queryKey: ["playlists", id, "items", "all"], queryFn: () => allEntries(id), staleTime: Infinity });

  // What the server has, and the draft being edited.
  const [saved, setSaved] = useState<PlaylistEntry[] | null>(null);
  const [draft, setDraft] = useState<PlaylistEntry[]>([]);
  const [history, setHistory] = useState<PlaylistEntry[][]>([]);
  const [known, setKnown] = useState<ReadonlySet<number>>(new Set());
  const [seenData, setSeenData] = useState<PlaylistEntry[] | undefined>();
  const [selected, setSelected] = useState<ReadonlySet<number>>(new Set());
  const anchor = useRef<number | null>(null);
  const [filter, setFilter] = useState("");
  const [sortKey, setSortKey] = useState<SortKey>("title");
  const [descending, setDescending] = useState(false);
  const [dragging, setDragging] = useState(false);
  const [over, setOver] = useState<number | "end" | null>(null);
  const [adding, setAdding] = useState(false);
  const [combining, setCombining] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);

  // The first load fills the draft; later loads (after adding from the library or other
  // playlists) add what's new to the end of both, leaving the draft's unsaved changes alone.
  if (entries.data && entries.data !== seenData) {
    const data = entries.data;
    setSeenData(data);
    if (saved === null) {
      setKnown(new Set(data.map((e) => e.entryId)));
      setSaved(data);
      setDraft(data);
    } else {
      const fresh = data.filter((e) => !known.has(e.entryId));
      if (fresh.length) {
        setKnown(new Set([...known, ...fresh.map((e) => e.entryId)]));
        setSaved([...saved, ...fresh]);
        setDraft([...draft, ...fresh]);
        // Earlier drafts get them too: they're in the playlist already, and saving a draft
        // without them (after Undo) would take them out again.
        setHistory(history.map((h) => [...h, ...fresh]));
      }
    }
  }

  const dirty = saved !== null && !sameOrder(saved, draft);
  useBlocker({
    shouldBlockFn: () => dirty && !window.confirm("Leave without saving your changes to this playlist?"),
    enableBeforeUnload: dirty,
  });

  /** A change to the draft, which Undo can take back. */
  const change = useCallback(
    (next: PlaylistEntry[], message?: string) => {
      setHistory((h) => [...h.slice(-49), draft]);
      setDraft(next);
      setNotice(message ?? null);
    },
    [draft],
  );
  const undo = useCallback(() => {
    setHistory((h) => {
      const last = h.at(-1);
      if (last) setDraft(last);
      return h.slice(0, -1);
    });
    setNotice(null);
  }, []);

  const save = useMutation({
    mutationFn: (list: PlaylistEntry[]) =>
      unwrap(api.PUT("/playlists/{playlistId}/items", { params: { path: { playlistId: id } }, body: { entryIds: list.map((e) => e.entryId) } })),
    onSuccess: (p, list) => {
      setSaved(list);
      setNotice("Saved.");
      qc.setQueryData(playlistQuery(id).queryKey, p);
      void qc.invalidateQueries({ queryKey: ["playlists", "list"] });
      void qc.invalidateQueries({ queryKey: ["playlists", id, "items"], exact: true });
    },
  });

  const visible = useMemo(() => (filter.trim() ? draft.filter((e) => matchesFilter(e, filter)) : draft), [draft, filter]);
  const positions = useMemo(() => new Map(draft.map((e, i) => [e.entryId, i + 1])), [draft]);
  const selectedCount = draft.filter((e) => selected.has(e.entryId)).length;
  const dupes = useMemo(() => duplicateCount(draft), [draft]);
  const totalMs = useMemo(() => draft.reduce((n, e) => n + (e.item.durationMs ?? 0), 0), [draft]);

  const removeSelected = useCallback(() => {
    if (!selected.size) return;
    const next = draft.filter((e) => !selected.has(e.entryId));
    const n = draft.length - next.length;
    change(next, `Removed ${n} ${n === 1 ? "entry" : "entries"}.`);
    setSelected(new Set());
  }, [draft, selected, change]);
  const moveTo = (where: "top" | "bottom") => {
    if (!selected.size) return;
    change(moveSelected(draft, selected, where === "top" ? (draft.find((e) => !selected.has(e.entryId))?.entryId ?? null) : null));
  };
  const nudge = (by: -1 | 1) => selected.size && change(nudgeSelected(draft, selected, by));
  const selectAll = useCallback(() => setSelected(new Set(visible.map((e) => e.entryId))), [visible]);

  // Clicking a row selects it; shift-click a range from the last one clicked; Ctrl or ⌘-click
  // adds or takes away one.
  const onRowClick = useCallback(
    (entryId: number, e: MouseEvent) => {
      if (e.shiftKey && anchor.current !== null) {
        const range = rangeIds(visible, anchor.current, entryId);
        setSelected((s) => new Set([...(e.metaKey || e.ctrlKey ? s : []), ...range]));
        return;
      }
      anchor.current = entryId;
      if (e.metaKey || e.ctrlKey) {
        setSelected((s) => {
          const n = new Set(s);
          if (n.has(entryId)) n.delete(entryId);
          else n.add(entryId);
          return n;
        });
      } else setSelected((s) => (s.size === 1 && s.has(entryId) ? new Set() : new Set([entryId])));
    },
    [visible],
  );
  const onCheck = useCallback((entryId: number, shift: boolean) => {
    if (shift && anchor.current !== null) {
      const range = rangeIds(visible, anchor.current, entryId);
      setSelected((s) => new Set([...s, ...range]));
      return;
    }
    anchor.current = entryId;
    setSelected((s) => {
      const n = new Set(s);
      if (n.has(entryId)) n.delete(entryId);
      else n.add(entryId);
      return n;
    });
  }, [visible]);
  const removeOne = useCallback(
    (entryId: number) => {
      change(draft.filter((e) => e.entryId !== entryId));
      setSelected((s) => {
        const n = new Set(s);
        n.delete(entryId);
        return n;
      });
    },
    [draft, change],
  );

  // Dragging a selected row takes the whole selection with it; any other row goes alone.
  const dragSet = useRef<ReadonlySet<number>>(new Set());
  const onDragStart = useCallback(
    (entryId: number, e: DragEvent) => {
      dragSet.current = selected.has(entryId) ? selected : new Set([entryId]);
      if (!selected.has(entryId)) setSelected(new Set([entryId]));
      e.dataTransfer.effectAllowed = "move";
      e.dataTransfer.setData("text/plain", String(entryId));
      setDragging(true);
    },
    [selected],
  );
  const dropBefore = useCallback(
    (beforeId: number | null) => {
      setDragging(false);
      setOver(null);
      if (!dragSet.current.size) return;
      const next = moveSelected(draft, dragSet.current, beforeId);
      dragSet.current = new Set();
      if (!sameOrder(next, draft)) change(next);
    },
    [draft, change],
  );
  const endDrag = useCallback(() => {
    setDragging(false);
    setOver(null);
  }, []);

  // Keyboard: Delete removes the selection, ⌘/Ctrl+A selects all, ⌘/Ctrl+Z undoes, ⌘/Ctrl+S saves,
  // Escape clears the selection.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey;
      if (mod && e.key.toLowerCase() === "s") {
        e.preventDefault();
        if (dirty && !save.isPending) save.mutate(draft);
        return;
      }
      // ⌘/Ctrl+A and Z are this page's too, except in a text field or under a dialog (theirs).
      if (ignoreShortcut(e, true)) return;
      if (mod && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "a") {
        e.preventDefault();
        selectAll();
        return;
      }
      if (mod && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "z") {
        e.preventDefault();
        undo();
        return;
      }
      if (ignoreShortcut(e)) return;
      if ((e.key === "Delete" || e.key === "Backspace") && selected.size) {
        e.preventDefault();
        removeSelected();
      } else if (e.key === "Escape" && selected.size) {
        setSelected(new Set());
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [dirty, draft, save, selected, removeSelected, selectAll, undo]);

  if (pl.isPending || (entries.isPending && saved === null)) return <Spinner />;
  if (pl.isError) return <div className="p-8"><Alert tone="error">{pl.error.message}</Alert></div>;
  if (entries.isError) return <div className="p-8"><Alert tone="error">{entries.error.message}</Alert></div>;
  const p = pl.data;
  if (p.readOnly || p.rules)
    return (
      <div className="space-y-4 p-8">
        <Alert tone="info">{p.readOnly ? "This playlist was shared with you, so only its owner can edit it." : "A smart playlist fills itself from its rules: change them on its page."}</Alert>
        <Link to="/playlist/$playlistId" params={{ playlistId }} className="text-sm text-accent hover:underline">
          Back to the playlist
        </Link>
      </div>
    );

  const allShownSelected = visible.length > 0 && visible.every((e) => selected.has(e.entryId));
  return (
    <div className="flex min-h-full flex-col p-4 lg:p-8" data-testid="playlist-editor">
      <header className="mb-4 flex flex-wrap items-center gap-3">
        <Link to="/playlist/$playlistId" params={{ playlistId }} className="rounded-full p-2 text-muted hover:bg-surface-2 hover:text-text" aria-label="Back to the playlist">
          <ArrowLeft className="size-5" />
        </Link>
        <div className="min-w-0 flex-1">
          <div className="text-xs font-semibold tracking-wider text-faint uppercase">Edit playlist</div>
          <h1 className="truncate text-2xl font-bold">{p.title}</h1>
          <div className="text-sm text-muted" data-testid="editor-summary">
            {draft.length} {p.kind === "audio" ? "tracks" : "items"} · {formatDuration(totalMs)}
            {selectedCount > 0 && ` · ${selectedCount} selected`}
            {dirty && <span className="text-accent"> · Unsaved changes</span>}
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="ghost" size="sm" onClick={undo} disabled={!history.length} title="Undo (⌘Z / Ctrl+Z)">
            <Undo2 className="size-4" /> Undo
          </Button>
          <Button variant="ghost" size="sm" onClick={() => saved && change(saved, "Changes discarded.")} disabled={!dirty}>
            Discard changes
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setCombining(true)}>
            <Combine className="size-4" /> Add playlists
          </Button>
          <Button variant="primary" size="sm" onClick={() => save.mutate(draft)} disabled={!dirty} loading={save.isPending} data-testid="editor-save" title="Save (⌘S / Ctrl+S)">
            <Save className="size-4" /> Save
          </Button>
        </div>
      </header>

      {save.isError && <Alert tone="error">{save.error.message}</Alert>}
      {notice && !save.isError && (
        <div className="mb-2 text-sm text-muted" role="status">
          {notice}
        </div>
      )}

      <div className="sticky top-0 z-10 -mx-4 mb-3 flex flex-wrap items-center gap-2 border-b border-border bg-bg/95 px-4 py-2 backdrop-blur lg:-mx-8 lg:px-8">
        <div className="relative w-full sm:w-64">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-faint" aria-hidden />
          <Input aria-label="Filter this playlist" placeholder="Filter" value={filter} onChange={(e) => setFilter(e.target.value)} className="pl-8" />
        </div>
        <div className="flex items-center gap-1">
          <Select aria-label="Sort by" value={sortKey} onChange={(e) => setSortKey(e.target.value as SortKey)} className="w-auto">
            {(Object.keys(sortLabels) as SortKey[]).map((k) => (
              <option key={k} value={k}>
                {sortLabels[k]}
              </option>
            ))}
          </Select>
          <Button size="sm" variant="ghost" onClick={() => setDescending((d) => !d)} aria-label={descending ? "Descending" : "Ascending"} title={descending ? "Descending" : "Ascending"}>
            {descending ? <ArrowDown className="size-4" /> : <ArrowUp className="size-4" />}
          </Button>
          <Button size="sm" onClick={() => change(sortEntries(draft, sortKey, descending), `Sorted by ${sortLabels[sortKey].toLowerCase()}.`)}>
            Sort
          </Button>
        </div>
        <Button size="sm" variant="ghost" onClick={() => change([...draft].reverse(), "Reversed.")} disabled={draft.length < 2}>
          Reverse
        </Button>
        <Button size="sm" variant="ghost" onClick={() => change(shuffleEntries(draft), "Shuffled.")} disabled={draft.length < 2}>
          <Shuffle className="size-4" /> Shuffle
        </Button>
        <Button size="sm" variant="ghost" onClick={() => change(withoutDuplicates(draft), `Removed ${dupes} duplicate${dupes === 1 ? "" : "s"}.`)} disabled={!dupes} data-testid="editor-dedupe">
          <CopyX className="size-4" /> Remove duplicates{dupes ? ` (${dupes})` : ""}
        </Button>
        <Button size="sm" variant={adding ? "secondary" : "ghost"} onClick={() => setAdding((a) => !a)}>
          <Plus className="size-4" /> Add from library
        </Button>
      </div>

      {selectedCount > 0 && (
        <div className="mb-3 flex flex-wrap items-center gap-2 rounded-lg bg-surface-2 px-3 py-2 text-sm" data-testid="editor-selection">
          <span className="font-medium">{selectedCount} selected</span>
          <Button size="sm" variant="danger" onClick={removeSelected}>
            <Trash2 className="size-4" /> Remove
          </Button>
          <Button size="sm" variant="ghost" onClick={() => moveTo("top")} title="Move to the top">
            <ArrowUpToLine className="size-4" /> Top
          </Button>
          <Button size="sm" variant="ghost" onClick={() => nudge(-1)} aria-label="Move up">
            <ArrowUp className="size-4" />
          </Button>
          <Button size="sm" variant="ghost" onClick={() => nudge(1)} aria-label="Move down">
            <ArrowDown className="size-4" />
          </Button>
          <Button size="sm" variant="ghost" onClick={() => moveTo("bottom")} title="Move to the bottom">
            <ArrowDownToLine className="size-4" /> Bottom
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setSelected(new Set())}>
            Clear selection
          </Button>
        </div>
      )}

      <div className="flex flex-1 flex-col gap-4 lg:flex-row">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-3 px-3 py-1.5 text-xs text-muted">
            <input
              type="checkbox"
              className="accent-accent"
              checked={allShownSelected}
              onChange={() => (allShownSelected ? setSelected(new Set()) : selectAll())}
              aria-label={allShownSelected ? "Select none" : "Select all"}
            />
            <span>{filter.trim() ? `${visible.length} of ${draft.length} match` : "Select all"}</span>
            <span className="ml-auto max-sm:hidden">Shift-click selects a range · drag to move</span>
          </div>
          {draft.length === 0 && <p className="p-4 text-muted">This playlist is empty. Add from the library, or add other playlists to it.</p>}
          <ol className="empty:hidden divide-y divide-border rounded-lg border border-border bg-surface" data-testid="editor-rows">
            {visible.map((e) => (
              <EditorRow
                key={e.entryId}
                entry={e}
                position={positions.get(e.entryId) ?? 0}
                selected={selected.has(e.entryId)}
                over={over === e.entryId}
                dragging={dragging && selected.has(e.entryId)}
                onClick={onRowClick}
                onCheck={onCheck}
                onRemove={removeOne}
                onDragStart={onDragStart}
                onDragOver={setOver}
                onDrop={dropBefore}
                onDragEnd={endDrag}
              />
            ))}
          </ol>
          {dragging && (
            <div
              onDragOver={(ev) => {
                ev.preventDefault();
                setOver("end");
              }}
              onDrop={(ev) => {
                ev.preventDefault();
                dropBefore(null);
              }}
              className={clsx("mt-1 rounded-lg border-2 border-dashed p-3 text-center text-sm text-muted", over === "end" ? "border-accent" : "border-border")}
            >
              Drop here to move to the end
            </div>
          )}
        </div>
        {adding && <AddFromLibrary playlistId={id} kind={p.kind} onAdded={() => void entries.refetch()} onClose={() => setAdding(false)} />}
      </div>
      {combining && <CombinePlaylistsDialog into={p} onClose={() => (setCombining(false), void entries.refetch())} />}
    </div>
  );
}

const EditorRow = memo(function EditorRow({
  entry: e,
  position,
  selected,
  over,
  dragging,
  onClick,
  onCheck,
  onRemove,
  onDragStart,
  onDragOver,
  onDrop,
  onDragEnd,
}: {
  entry: PlaylistEntry;
  position: number;
  selected: boolean;
  over: boolean;
  dragging: boolean;
  onClick: (entryId: number, e: MouseEvent) => void;
  onCheck: (entryId: number, shift: boolean) => void;
  onRemove: (entryId: number) => void;
  onDragStart: (entryId: number, e: DragEvent) => void;
  onDragOver: (entryId: number) => void;
  onDrop: (beforeId: number) => void;
  onDragEnd: () => void;
}) {
  const it = e.item;
  const art = it.type === "episode" ? (it.images?.thumb ?? it.images?.poster) : it.images?.poster;
  return (
    <li
      draggable
      onDragStart={(ev) => onDragStart(e.entryId, ev)}
      onDragOver={(ev) => {
        ev.preventDefault();
        onDragOver(e.entryId);
      }}
      onDrop={(ev) => {
        ev.preventDefault();
        onDrop(e.entryId);
      }}
      onDragEnd={onDragEnd}
      onClick={(ev) => onClick(e.entryId, ev)}
      aria-selected={selected}
      data-testid="editor-row"
      className={clsx(
        "group flex cursor-default items-center gap-3 px-3 py-1.5 select-none [contain-intrinsic-size:auto_52px] [content-visibility:auto]",
        selected ? "bg-accent/10" : "hover:bg-surface-2",
        dragging && "opacity-40",
        over && "border-t-2 border-t-accent",
      )}
    >
      <input
        type="checkbox"
        className="accent-accent"
        checked={selected}
        onClick={(ev) => ev.stopPropagation()}
        onChange={(ev) => onCheck(e.entryId, (ev.nativeEvent as globalThis.MouseEvent).shiftKey)}
        aria-label={`Select ${it.title}`}
      />
      <GripVertical className="size-4 shrink-0 cursor-grab text-faint max-sm:hidden" aria-hidden />
      <span className="w-8 shrink-0 text-right text-xs text-faint tabular-nums max-sm:hidden">{position}</span>
      <span className={clsx("shrink-0 overflow-hidden rounded bg-surface-3", it.type === "track" ? "size-9" : "aspect-video w-16")}>
        {art && <img src={imageUrl(art, it.type === "track" ? 40 : 80)} alt="" loading="lazy" draggable={false} className="size-full object-cover" />}
      </span>
      <span className="min-w-0 flex-1">
        <span className={clsx("block truncate text-sm", !it.available && "text-faint line-through")}>{it.title}</span>
        <span className="block truncate text-xs text-muted">{[artistOf(it), it.parentTitle].filter(Boolean).join(" · ")}</span>
      </span>
      <span className="w-14 shrink-0 text-right text-sm text-muted tabular-nums max-sm:hidden">{it.type === "track" ? formatTrackTime(it.durationMs) : formatDuration(it.durationMs)}</span>
      <button
        type="button"
        onClick={(ev) => {
          ev.stopPropagation();
          onRemove(e.entryId);
        }}
        className="rounded p-1 text-muted opacity-0 group-hover:opacity-100 hover:text-text focus:opacity-100 [@media(hover:none)]:opacity-100"
        aria-label={`Remove ${it.title}`}
      >
        <X className="size-4" />
      </button>
    </li>
  );
});

/** Search the library and add to the end of the playlist (saved at once; albums and artists add their tracks). */
function AddFromLibrary({ playlistId, kind, onAdded, onClose }: { playlistId: number; kind: "audio" | "video"; onAdded: () => void; onClose: () => void }) {
  const [q, setQ] = useState("");
  const [typed, setTyped] = useState("");
  useEffect(() => {
    const t = window.setTimeout(() => setTyped(q), 250);
    return () => window.clearTimeout(t);
  }, [q]);
  const results = useQuery(searchQuery(typed, 20));
  const fits = kind === "audio" ? musicTypes : videoTypes;
  const groups = results.data?.groups.filter((g) => fits.has(g.type)) ?? [];
  const [added, setAdded] = useState<Set<number>>(new Set());
  const add = useMutation({
    mutationFn: (item: ItemSummary) => unwrap(api.POST("/playlists/{playlistId}/items", { params: { path: { playlistId } }, body: { itemIds: [item.id] } })),
    onSuccess: (_, item) => {
      setAdded((s) => new Set(s).add(item.id));
      onAdded();
    },
  });
  const label: Record<string, string> = { track: "Songs", album: "Albums", artist: "Artists", movie: "Films", show: "Shows", season: "Seasons", episode: "Episodes", video: "Videos" };
  return (
    <aside className="w-full shrink-0 rounded-lg border border-border bg-surface p-3 lg:sticky lg:top-16 lg:max-h-[calc(100vh-8rem)] lg:w-96 lg:overflow-y-auto" data-testid="editor-add">
      <div className="mb-2 flex items-center justify-between">
        <h2 className="font-semibold">Add from the library</h2>
        <button onClick={onClose} className="rounded p-1 text-muted hover:bg-surface-2 hover:text-text" aria-label="Close">
          <X className="size-4" />
        </button>
      </div>
      <Input autoFocus aria-label="Search the library" placeholder={kind === "audio" ? "Songs, albums or artists" : "Films, shows or episodes"} value={q} onChange={(e) => setQ(e.target.value)} />
      {add.isError && <div className="mt-2"><Alert tone="error">{add.error.message}</Alert></div>}
      {results.isFetching && <Spinner />}
      {groups.map((g) => (
        <div key={g.type} className="mt-3">
          <div className="mb-1 text-xs font-semibold tracking-wider text-faint uppercase">{label[g.type] ?? g.type}</div>
          <ul>
            {g.items.map((it) => (
              <li key={it.id} className="flex items-center gap-2 rounded px-1 py-1 hover:bg-surface-2">
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm">{it.title}</span>
                  <span className="block truncate text-xs text-muted">{[artistOf(it), it.parentTitle].filter(Boolean).join(" · ")}</span>
                </span>
                <Button size="sm" variant={added.has(it.id) ? "ghost" : "secondary"} onClick={() => add.mutate(it)} aria-label={`Add ${it.title}`}>
                  {added.has(it.id) ? "Added" : <Plus className="size-4" />}
                </Button>
              </li>
            ))}
          </ul>
        </div>
      ))}
      {typed.trim() && results.isSuccess && !groups.length && <p className="mt-3 text-sm text-muted">Nothing found.</p>}
    </aside>
  );
}
