import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { systemInfoQuery } from "@/api/queries";
/**
 * Detects a server upgrade while this tab is open (the page is still running the old web
 * client) and asks the user to reload.
 */
export function UpdateBanner() {
    const info = useQuery({ ...systemInfoQuery, refetchInterval: 60_000 });
    // The version this page was loaded with is the first one seen.
    const [loadedVersion, setLoadedVersion] = useState(null);
    if (info.data && loadedVersion === null)
        setLoadedVersion(info.data.version);
    if (!info.data || loadedVersion === null || info.data.version === loadedVersion)
        return null;
    return (_jsxs("div", { role: "status", className: "flex items-center justify-center gap-3 bg-accent px-4 py-2 text-sm font-medium text-black", children: ["Marquee was updated to ", info.data.version, ".", _jsx("button", { onClick: () => window.location.reload(), className: "rounded bg-black/15 px-3 py-1 font-semibold hover:bg-black/25", children: "Reload" })] }));
}
