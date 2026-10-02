import { jsx as _jsx } from "react/jsx-runtime";
import { useParams, useSearch } from "@tanstack/react-router";
import { lazy, Suspense } from "react";
import { Spinner } from "@/components/ui";
// hls.js is large; load the player only when something is played.
const VideoPlayer = lazy(() => import("./VideoPlayer").then((m) => ({ default: m.VideoPlayer })));
export function PlayerPage() {
    const { itemId } = useParams({ from: "/play/$itemId" });
    const { t } = useSearch({ from: "/play/$itemId" });
    return (_jsx(Suspense, { fallback: _jsx("div", { className: "fixed inset-0 z-50 flex items-center justify-center bg-black", children: _jsx(Spinner, { label: "Loading player" }) }), children: _jsx(VideoPlayer, { itemId: Number(itemId), startMs: t }, itemId) }));
}
