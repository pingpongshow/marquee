import { useQuery } from "@tanstack/react-query";
import { clsx } from "clsx";
import { Download } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Button, Card, Select, Spinner } from "@/components/ui";

type Level = "debug" | "info" | "warn" | "error";
type Entry = components["schemas"]["LogEntry"];

const levelColor: Record<Level, string> = { debug: "text-faint", info: "text-muted", warn: "text-accent", error: "text-danger" };

function line(e: Entry) {
  const attrs = Object.entries(e.attrs ?? {})
    .map(([k, v]) => `${k}=${v.includes(" ") ? JSON.stringify(v) : v}`)
    .join(" ");
  return `${new Date(e.time).toLocaleString()} ${e.level.toUpperCase().padEnd(5)} ${e.message}${attrs ? " " + attrs : ""}`;
}

export function LogsSettings() {
  const [level, setLevel] = useState<Level>("info");
  const logs = useQuery({
    queryKey: ["logs", level],
    queryFn: () => unwrap(api.GET("/logs", { params: { query: { level, limit: 2000 } } })),
    refetchInterval: 5000,
  });
  const download = () => {
    const text = (logs.data ?? []).map(line).join("\n");
    const a = document.createElement("a");
    a.href = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
    a.download = `marquee-${new Date().toISOString().slice(0, 19)}.log`;
    a.click();
    URL.revokeObjectURL(a.href);
  };
  return (
    <Card
      title="Server log"
      description="The most recent 5,000 entries since the server started. Refreshes every few seconds."
      actions={
        <div className="flex gap-2">
          <Select aria-label="Minimum level" value={level} onChange={(e) => setLevel(e.target.value as Level)} className="h-8 w-28">
            <option value="debug">Debug</option>
            <option value="info">Info</option>
            <option value="warn">Warnings</option>
            <option value="error">Errors</option>
          </Select>
          <Button size="sm" variant="ghost" onClick={download} disabled={!logs.data?.length}>
            <Download className="size-4" /> Download
          </Button>
        </div>
      }
    >
      {logs.isPending ? (
        <Spinner />
      ) : (
        <pre className="max-h-[65vh] overflow-auto rounded-md bg-bg p-3 font-mono text-xs leading-relaxed">
          {logs.data?.length === 0 && <span className="text-faint">No entries.</span>}
          {logs.data?.map((e, i) => (
            <div key={i} className={clsx("break-all whitespace-pre-wrap", levelColor[e.level as Level])}>
              {line(e)}
            </div>
          ))}
        </pre>
      )}
    </Card>
  );
}
