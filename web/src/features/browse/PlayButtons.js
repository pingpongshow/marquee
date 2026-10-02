import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Check, Play, RotateCcw, Shuffle } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { Button } from "@/components/ui";
import { useMusic } from "../player/MusicPlayer";
import { formatDuration } from "./format";
async function children(id) {
    return (await unwrap(api.GET("/items/{itemId}/children", { params: { path: { itemId: id }, query: { limit: 500 } } }))).items;
}
/** First unwatched episode of a show (or the first episode if all are watched). */
async function nextEpisode(showId) {
    const seasons = (await children(showId)).filter((s) => (s.index ?? 0) > 0);
    let first;
    for (const s of seasons) {
        for (const e of await children(s.id)) {
            first ??= e;
            if (!e.viewCount)
                return e;
        }
    }
    return first;
}
async function albumTracks(album) {
    return (await children(album)).filter((t) => t.type === "track");
}
export function PlayButtons({ item }) {
    const navigate = useNavigate();
    const qc = useQueryClient();
    const music = useMusic();
    const [busy, setBusy] = useState(false);
    const watched = item.type === "show" || item.type === "season" ? item.leafCount > 0 && item.watchedLeafCount === item.leafCount : (item.viewCount ?? 0) > 0;
    const toggleWatched = useMutation({
        mutationFn: () => watched ? unwrap(api.DELETE("/items/{itemId}/watched", { params: { path: { itemId: item.id } } })) : unwrap(api.POST("/items/{itemId}/watched", { params: { path: { itemId: item.id } } })),
        onSuccess: () => qc.invalidateQueries({ queryKey: ["items"] }),
    });
    const playVideo = (id, t) => navigate({ to: "/play/$itemId", params: { itemId: String(id) }, search: { t } });
    const run = async (fn) => {
        setBusy(true);
        try {
            await fn();
        }
        finally {
            setBusy(false);
        }
    };
    const playable = item.type === "movie" || item.type === "episode" || item.type === "video";
    const resume = playable && (item.viewOffsetMs ?? 0) > 0;
    return (_jsxs("div", { className: "mt-5 flex flex-wrap items-center gap-3", children: [playable && (_jsxs(_Fragment, { children: [_jsxs(Button, { variant: "primary", onClick: () => playVideo(item.id), disabled: !item.available, children: [_jsx(Play, { className: "size-4 fill-current" }), " ", resume ? `Resume from ${formatDuration(item.viewOffsetMs)}` : "Play"] }), resume && (_jsxs(Button, { onClick: () => playVideo(item.id, 0), children: [_jsx(RotateCcw, { className: "size-4" }), " Play from start"] }))] })), (item.type === "show" || item.type === "season") && (_jsxs(Button, { variant: "primary", loading: busy, onClick: () => run(async () => {
                    const ep = item.type === "show" ? await nextEpisode(item.id) : (await children(item.id)).find((e) => !e.viewCount) ?? (await children(item.id))[0];
                    if (ep)
                        playVideo(ep.id);
                }), children: [_jsx(Play, { className: "size-4 fill-current" }), " ", item.watchedLeafCount ? "Continue" : "Play"] })), item.type === "album" && (_jsxs(_Fragment, { children: [_jsxs(Button, { variant: "primary", loading: busy, onClick: () => run(async () => music.play(await albumTracks(item.id))), children: [_jsx(Play, { className: "size-4 fill-current" }), " Play"] }), _jsxs(Button, { loading: busy, onClick: () => run(async () => music.play([...(await albumTracks(item.id))].sort(() => Math.random() - 0.5))), children: [_jsx(Shuffle, { className: "size-4" }), " Shuffle"] })] })), item.type === "artist" && (_jsxs(Button, { variant: "primary", loading: busy, onClick: () => run(async () => {
                    const tracks = [];
                    for (const al of await children(item.id))
                        tracks.push(...(await albumTracks(al.id)));
                    music.play(tracks.sort(() => Math.random() - 0.5));
                }), children: [_jsx(Shuffle, { className: "size-4" }), " Shuffle artist"] })), item.type === "track" && (_jsxs(Button, { variant: "primary", onClick: () => music.play([item]), children: [_jsx(Play, { className: "size-4 fill-current" }), " Play"] })), (playable || item.type === "show" || item.type === "season") && (_jsxs(Button, { variant: "ghost", onClick: () => toggleWatched.mutate(), loading: toggleWatched.isPending, children: [_jsx(Check, { className: watched ? "size-4 text-success" : "size-4" }), " ", watched ? "Watched" : "Mark watched"] }))] }));
}
