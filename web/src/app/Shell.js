import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Clapperboard, Film, Home, Image, LogOut, Menu, Music, Settings, Sparkles, Tv, UserRound, Users, Video } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { librariesQuery, meQuery, systemInfoQuery } from "@/api/queries";
import { ActivityIndicator } from "@/features/activity/ActivityIndicator";
import { UpdateBanner } from "@/features/activity/UpdateBanner";
import { SearchBox } from "@/features/search/SearchBox";
import { Avatar } from "@/features/users/Avatar";
import { ProfileSwitcher } from "@/features/users/ProfileSwitcher";
import { useMusic } from "@/features/player/MusicPlayer";
import { useAuth } from "@/lib/auth";
export const libraryIcons = {
    movies: Film,
    shows: Tv,
    anime: Sparkles,
    music: Music,
    videos: Video,
    photos: Image,
};
function Logo() {
    return (_jsxs(Link, { to: "/", className: "flex items-center gap-2 text-lg font-bold tracking-tight", children: [_jsx(Clapperboard, { className: "size-6 text-accent", "aria-hidden": true }), "Marquee"] }));
}
const navItem = "flex items-center gap-3 rounded-md px-3 py-2 text-sm text-muted hover:bg-surface-2 hover:text-text";
const navActive = { className: "bg-surface-2 !text-text font-medium" };
function UserMenu({ name, avatarUrl, onSwitch, onSignOut }) {
    const [open, setOpen] = useState(false);
    const ref = useRef(null);
    useEffect(() => {
        if (!open)
            return;
        const onDown = (e) => !ref.current?.contains(e.target) && setOpen(false);
        document.addEventListener("mousedown", onDown);
        return () => document.removeEventListener("mousedown", onDown);
    }, [open]);
    const item = "flex w-full items-center gap-3 px-4 py-2 text-left text-sm hover:bg-surface-2";
    return (_jsxs("div", { className: "relative", ref: ref, children: [_jsxs("button", { onClick: () => setOpen((v) => !v), className: "flex items-center gap-2 rounded-full p-0.5 pr-2 hover:bg-surface-2", "aria-expanded": open, "aria-label": "Account menu", children: [_jsx(Avatar, { name: name, url: avatarUrl, className: "size-8 !bg-accent text-sm text-black" }), _jsx("span", { className: "hidden text-sm text-muted sm:inline", children: name })] }), open && (_jsxs("div", { className: "absolute right-0 z-40 mt-2 w-52 overflow-hidden rounded-lg border border-border bg-surface py-1 shadow-2xl", onClick: () => setOpen(false), children: [_jsxs(Link, { to: "/account", className: item, children: [_jsx(UserRound, { className: "size-4", "aria-hidden": true }), " Account"] }), _jsxs("button", { onClick: onSwitch, className: item, children: [_jsx(Users, { className: "size-4", "aria-hidden": true }), " Switch profile"] }), _jsxs("button", { onClick: onSignOut, className: item, children: [_jsx(LogOut, { className: "size-4", "aria-hidden": true }), " Sign out"] })] }))] }));
}
export function Shell({ children }) {
    const { signOut } = useAuth();
    const me = useQuery(meQuery);
    const info = useQuery(systemInfoQuery);
    const libraries = useQuery(librariesQuery);
    const [navOpen, setNavOpen] = useState(false);
    const [switching, setSwitching] = useState(false);
    const music = useMusic();
    const sidebar = (_jsxs("nav", { "aria-label": "Libraries", className: "flex h-full flex-col gap-1 p-3", children: [_jsxs(Link, { to: "/", className: navItem, activeProps: navActive, activeOptions: { exact: true }, onClick: () => setNavOpen(false), children: [_jsx(Home, { className: "size-5", "aria-hidden": true }), " Home"] }), _jsx("div", { className: "mt-4 mb-1 px-3 text-xs font-semibold tracking-wider text-faint uppercase", children: info.data?.serverName ?? "Libraries" }), libraries.data?.map((lib) => {
                const Icon = libraryIcons[lib.type];
                return (_jsxs(Link, { to: "/library/$libraryId", params: { libraryId: String(lib.id) }, className: navItem, activeProps: navActive, onClick: () => setNavOpen(false), children: [_jsx(Icon, { className: "size-5", "aria-hidden": true }), " ", _jsx("span", { className: "truncate", children: lib.name }), lib.scanStatus !== "idle" && _jsx("span", { className: "ml-auto size-2 animate-pulse rounded-full bg-accent", title: "Scanning" })] }, lib.id));
            }), libraries.data?.length === 0 && _jsx("p", { className: "px-3 text-sm text-faint", children: "No libraries yet" }), _jsx("div", { className: "mt-auto" }), me.data?.isAdmin && (_jsxs(Link, { to: "/settings", className: navItem, activeProps: navActive, onClick: () => setNavOpen(false), children: [_jsx(Settings, { className: "size-5", "aria-hidden": true }), " Settings"] }))] }));
    return (_jsxs("div", { className: "flex h-full flex-col", children: [_jsx(UpdateBanner, {}), _jsxs("header", { className: "flex h-14 shrink-0 items-center gap-4 border-b border-border bg-surface px-4", children: [_jsx("button", { className: "rounded p-1 text-muted hover:text-text lg:hidden", onClick: () => setNavOpen((v) => !v), "aria-label": "Toggle navigation", children: _jsx(Menu, { className: "size-6" }) }), _jsx(Logo, {}), _jsx(SearchBox, {}), _jsxs("div", { className: "ml-auto flex items-center gap-3", children: [me.data?.isAdmin && _jsx(ActivityIndicator, {}), _jsx(UserMenu, { name: me.data?.displayName ?? "", avatarUrl: me.data?.avatarUrl, onSwitch: () => setSwitching(true), onSignOut: () => void signOut() })] })] }), _jsxs("div", { className: "flex min-h-0 flex-1", children: [_jsx("aside", { className: "hidden w-60 shrink-0 border-r border-border bg-surface lg:block", children: sidebar }), navOpen && (_jsxs("div", { className: "fixed inset-0 top-14 z-30 lg:hidden", children: [_jsx("div", { className: "absolute inset-0 bg-black/60", onClick: () => setNavOpen(false) }), _jsx("aside", { className: "relative h-full w-64 border-r border-border bg-surface", children: sidebar })] })), _jsxs("main", { className: "min-w-0 flex-1 overflow-y-auto", children: [children, music.index >= 0 && _jsx("div", { className: "h-20", "aria-hidden": true })] })] }), switching && _jsx(ProfileSwitcher, { onClose: () => setSwitching(false) })] }));
}
