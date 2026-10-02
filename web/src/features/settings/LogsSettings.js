import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useQuery } from "@tanstack/react-query";
import { clsx } from "clsx";
import { Download } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { Button, Card, Select, Spinner } from "@/components/ui";
const levelColor = { debug: "text-faint", info: "text-muted", warn: "text-accent", error: "text-danger" };
function line(e) {
    const attrs = Object.entries(e.attrs ?? {})
        .map(([k, v]) => `${k}=${v.includes(" ") ? JSON.stringify(v) : v}`)
        .join(" ");
    return `${new Date(e.time).toLocaleString()} ${e.level.toUpperCase().padEnd(5)} ${e.message}${attrs ? " " + attrs : ""}`;
}
export function LogsSettings() {
    const [level, setLevel] = useState("info");
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
    return (_jsx(Card, { title: "Server log", description: "The most recent 5,000 entries since the server started. Refreshes every few seconds.", actions: _jsxs("div", { className: "flex gap-2", children: [_jsxs(Select, { "aria-label": "Minimum level", value: level, onChange: (e) => setLevel(e.target.value), className: "h-8 w-28", children: [_jsx("option", { value: "debug", children: "Debug" }), _jsx("option", { value: "info", children: "Info" }), _jsx("option", { value: "warn", children: "Warnings" }), _jsx("option", { value: "error", children: "Errors" })] }), _jsxs(Button, { size: "sm", variant: "ghost", onClick: download, disabled: !logs.data?.length, children: [_jsx(Download, { className: "size-4" }), " Download"] })] }), children: logs.isPending ? (_jsx(Spinner, {})) : (_jsxs("pre", { className: "max-h-[65vh] overflow-auto rounded-md bg-bg p-3 font-mono text-xs leading-relaxed", children: [logs.data?.length === 0 && _jsx("span", { className: "text-faint", children: "No entries." }), logs.data?.map((e, i) => (_jsx("div", { className: clsx("break-all whitespace-pre-wrap", levelColor[e.level]), children: line(e) }, i)))] })) }));
}
