import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { clsx } from "clsx";
import { ArrowLeft, ChevronRight, EyeOff, ExternalLink, RefreshCw, Search, Subtitles } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import type { HealthCheck, HealthIssue, ItemDetail, ItemSummary } from "@/api/types";
import { Alert, Button, Spinner } from "@/components/ui";
import { toast } from "@/components/Toast";
import { FixMatchDialog } from "../browse/ItemActions";
import { Poster } from "../browse/Poster";
import { BazarrDialog } from "../player/Bazarr";

const PAGE = 50;

const severityStyle: Record<HealthCheck["severity"], { dot: string; border: string; label: string }> = {
  error: { dot: "bg-danger", border: "border-l-danger", label: "Problem" },
  warning: { dot: "bg-accent", border: "border-l-accent", label: "Warning" },
  info: { dot: "bg-sky-400", border: "border-l-sky-400", label: "Info" },
};

const healthQuery = {
  queryKey: ["library-health"],
  queryFn: () => unwrap(api.GET("/library-health")),
};

/** Library Health (ADM-11): checks for problems in the libraries, and a way to fix or ignore each one. */
export function LibraryHealthSettings() {
  const checks = useQuery(healthQuery);
  const [open, setOpen] = useState<HealthCheck | null>(null);
  if (open) return <CheckIssues check={open} onBack={() => setOpen(null)} />;
  if (checks.isPending) return <Spinner />;
  if (checks.isError) return <Alert tone="error">{checks.error.message}</Alert>;
  return (
    <ul className="space-y-2" aria-label="Health checks">
      {checks.data.map((c) => {
        const sev = severityStyle[c.severity];
        const available = c.available !== false;
        const clean = available && c.count === 0;
        return (
          <li key={c.id}>
            <button
              disabled={!available || clean}
              onClick={() => setOpen(c)}
              aria-label={`${c.title}: ${available ? c.count : "unavailable"}`}
              className={clsx(
                "flex w-full items-center gap-4 rounded-lg border border-l-4 border-border bg-surface px-4 py-3 text-left enabled:hover:bg-surface-2",
                available && !clean ? sev.border : "border-l-border",
                !available && "opacity-50",
              )}
            >
              <span className={clsx("size-2.5 shrink-0 rounded-full", available && !clean ? sev.dot : "bg-success")} title={sev.label} aria-hidden />
              <span className="min-w-0 flex-1">
                <span className="block font-medium">{c.title}</span>
                <span className="block text-sm text-muted">{c.description}</span>
                {!available && <span className="mt-0.5 block text-xs text-faint">Unavailable on this server.</span>}
              </span>
              {available && (
                <span className={clsx("text-lg font-semibold tabular-nums", clean ? "text-success" : "text-text")} data-testid={`health-count-${c.id}`}>
                  {c.count.toLocaleString()}
                </span>
              )}
              {available && !clean && <ChevronRight className="size-5 text-faint" aria-hidden />}
            </button>
          </li>
        );
      })}
    </ul>
  );
}

function issueSubtitle(it: ItemSummary) {
  if (it.type === "episode") return `${it.grandparentTitle ?? ""} · ${it.parentTitle ?? ""} · E${it.index ?? ""}`;
  if (it.type === "album") return it.artistCredit ?? it.parentTitle ?? "";
  return it.year ? String(it.year) : it.type;
}

function CheckIssues({ check, onBack }: { check: HealthCheck; onBack: () => void }) {
  const qc = useQueryClient();
  const [offset, setOffset] = useState(0);
  const [bazarrFor, setBazarrFor] = useState<ItemSummary | null>(null);
  const [matching, setMatching] = useState<ItemDetail | null>(null);
  const key = ["library-health", check.id, offset];
  const issues = useQuery({
    queryKey: key,
    queryFn: () => unwrap(api.GET("/library-health/{checkId}", { params: { path: { checkId: check.id }, query: { offset, limit: PAGE } } })),
  });
  const refreshAll = () => qc.invalidateQueries({ queryKey: ["library-health"] });
  const unignore = useMutation({
    mutationFn: (itemId: number) => unwrap(api.DELETE("/library-health/{checkId}/ignored/{itemId}", { params: { path: { checkId: check.id, itemId } } })),
    onSettled: refreshAll,
    onError: (e) => toast(e.message, { tone: "error" }),
  });
  const ignore = useMutation({
    mutationFn: (it: ItemSummary) => unwrap(api.PUT("/library-health/{checkId}/ignored/{itemId}", { params: { path: { checkId: check.id, itemId: it.id } } })),
    onSuccess: (_d, it) => {
      toast(`“${it.title}” is ignored for ${check.title.toLowerCase()}.`, { action: { label: "Undo", onClick: () => unignore.mutate(it.id) } });
      refreshAll();
    },
    onError: (e) => toast(e.message, { tone: "error" }),
  });
  const refresh = useMutation({
    mutationFn: (it: ItemSummary) => unwrap(api.POST("/items/{itemId}/refresh", { params: { path: { itemId: it.id } } })),
    onSuccess: (_d, it) => {
      toast(`Refreshed “${it.title}”.`);
      refreshAll();
      qc.invalidateQueries({ queryKey: ["items", it.id] });
    },
    onError: (e) => toast(e.message, { tone: "error" }),
  });
  const openMatch = useMutation({
    mutationFn: (it: ItemSummary) => unwrap(api.GET("/items/{itemId}", { params: { path: { itemId: it.id } } })),
    onSuccess: setMatching,
    onError: (e) => toast(e.message, { tone: "error" }),
  });
  const page = issues.data;
  const total = page?.total ?? 0;
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <Button variant="ghost" size="sm" onClick={onBack}>
          <ArrowLeft className="size-4" /> All checks
        </Button>
        <h2 className="text-lg font-semibold">{check.title}</h2>
        {page && <span className="text-sm text-muted">{total.toLocaleString()} found</span>}
      </div>
      <p className="text-sm text-muted">{check.description}</p>
      {issues.isPending && <Spinner />}
      {issues.isError && <Alert tone="error">{issues.error.message}</Alert>}
      {page?.items.length === 0 && <p className="text-muted">Nothing to fix here.</p>}
      <ul className="divide-y divide-border rounded-lg border border-border bg-surface" aria-label="Issues">
        {page?.items.map((iss) => (
          <IssueRow
            key={`${iss.item.id}-${iss.fileId ?? 0}`}
            issue={iss}
            checkId={check.id}
            busy={(refresh.isPending && refresh.variables?.id === iss.item.id) || (ignore.isPending && ignore.variables?.id === iss.item.id)}
            onRefresh={() => refresh.mutate(iss.item)}
            onIgnore={() => ignore.mutate(iss.item)}
            onFixMatch={() => openMatch.mutate(iss.item)}
            onBazarr={() => setBazarrFor(iss.item)}
          />
        ))}
      </ul>
      {total > PAGE && (
        <div className="flex items-center justify-between text-sm">
          <Button size="sm" variant="ghost" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - PAGE))}>
            Previous
          </Button>
          <span className="text-muted">
            {offset + 1}–{Math.min(total, offset + PAGE)} of {total.toLocaleString()}
          </span>
          <Button size="sm" variant="ghost" disabled={offset + PAGE >= total} onClick={() => setOffset(offset + PAGE)}>
            Next
          </Button>
        </div>
      )}
      {bazarrFor && (
        <BazarrDialog
          itemId={bazarrFor.id}
          title={bazarrFor.title}
          onClose={() => {
            setBazarrFor(null);
            refreshAll();
          }}
        />
      )}
      {matching && (
        <FixMatchDialog
          item={matching}
          onClose={() => {
            setMatching(null);
            refreshAll();
          }}
        />
      )}
    </div>
  );
}

function IssueRow({
  issue,
  checkId,
  busy,
  onRefresh,
  onIgnore,
  onFixMatch,
  onBazarr,
}: {
  issue: HealthIssue;
  checkId: HealthCheck["id"];
  busy: boolean;
  onRefresh: () => void;
  onIgnore: () => void;
  onFixMatch: () => void;
  onBazarr: () => void;
}) {
  const it = issue.item;
  const matchable = it.type === "movie" || it.type === "show";
  const square = it.type === "album" || it.type === "artist" || it.type === "track";
  return (
    <li className="flex gap-4 p-3" data-testid={`issue-${it.id}`}>
      <div className={clsx("shrink-0", square ? "w-14" : "w-12")}>
        <Poster item={it} shape={square ? "square" : "poster"} width={56} compact />
      </div>
      <div className="min-w-0 flex-1 space-y-0.5">
        <div className="truncate font-medium">{it.title}</div>
        <div className="truncate text-xs text-faint">{issueSubtitle(it)}</div>
        <div className="text-sm text-text/90">{issue.detail}</div>
        {issue.path && (
          <div className="truncate font-mono text-[11px] text-faint" title={issue.path}>
            {issue.path}
          </div>
        )}
        {!!issue.related?.length && (
          <div className="text-xs text-muted">
            Other copies:{" "}
            {issue.related.map((r, i) => (
              <span key={r.id}>
                {i > 0 && ", "}
                <Link to="/item/$itemId" params={{ itemId: String(r.id) }} className="text-text hover:underline">
                  {r.title}
                  {r.year ? ` (${r.year})` : ""}
                </Link>
              </span>
            ))}
          </div>
        )}
        <div className="flex flex-wrap gap-1 pt-1.5">
          <Link to="/item/$itemId" params={{ itemId: String(it.id) }} className="inline-flex h-8 items-center gap-1.5 rounded-md px-2.5 text-sm text-muted hover:bg-surface-2 hover:text-text">
            <ExternalLink className="size-4" /> Open
          </Link>
          {checkId === "missingSubtitles" && (
            <Button size="sm" variant="ghost" onClick={onBazarr}>
              <Subtitles className="size-4" /> Download with Bazarr
            </Button>
          )}
          {matchable && (
            <Button size="sm" variant="ghost" onClick={onFixMatch}>
              <Search className="size-4" /> Fix match
            </Button>
          )}
          <Button size="sm" variant="ghost" onClick={onRefresh} disabled={busy}>
            <RefreshCw className="size-4" /> Refresh metadata
          </Button>
          <Button size="sm" variant="ghost" onClick={onIgnore} disabled={busy} aria-label={`Ignore ${it.title}`}>
            <EyeOff className="size-4" /> Ignore
          </Button>
        </div>
      </div>
    </li>
  );
}
