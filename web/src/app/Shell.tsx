import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Clapperboard, Film, Home, Image, LogOut, Menu, Music, Settings, Sparkles, Tv, UserRound, Users, Video } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { librariesQuery, meQuery, systemInfoQuery } from "@/api/queries";
import type { LibraryType } from "@/api/types";
import { ActivityIndicator } from "@/features/activity/ActivityIndicator";
import { UpdateBanner } from "@/features/activity/UpdateBanner";
import { SearchBox } from "@/features/search/SearchBox";
import { ProfileSwitcher } from "@/features/users/ProfileSwitcher";
import { useMusic } from "@/features/player/MusicPlayer";
import { useAuth } from "@/lib/auth";

export const libraryIcons: Record<LibraryType, typeof Film> = {
  movies: Film,
  shows: Tv,
  anime: Sparkles,
  music: Music,
  videos: Video,
  photos: Image,
};

function Logo() {
  return (
    <Link to="/" className="flex items-center gap-2 text-lg font-bold tracking-tight">
      <Clapperboard className="size-6 text-accent" aria-hidden />
      Marquee
    </Link>
  );
}

const navItem = "flex items-center gap-3 rounded-md px-3 py-2 text-sm text-muted hover:bg-surface-2 hover:text-text";
const navActive = { className: "bg-surface-2 !text-text font-medium" };

function UserMenu({ name, onSwitch, onSignOut }: { name: string; onSwitch: () => void; onSignOut: () => void }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false);
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [open]);
  const item = "flex w-full items-center gap-3 px-4 py-2 text-left text-sm hover:bg-surface-2";
  return (
    <div className="relative" ref={ref}>
      <button onClick={() => setOpen((v) => !v)} className="flex items-center gap-2 rounded-full p-0.5 pr-2 hover:bg-surface-2" aria-expanded={open} aria-label="Account menu">
        <span className="flex size-8 items-center justify-center rounded-full bg-accent text-sm font-bold text-black">{name.slice(0, 1).toUpperCase()}</span>
        <span className="hidden text-sm text-muted sm:inline">{name}</span>
      </button>
      {open && (
        <div className="absolute right-0 z-40 mt-2 w-52 overflow-hidden rounded-lg border border-border bg-surface py-1 shadow-2xl" onClick={() => setOpen(false)}>
          <Link to="/account" className={item}>
            <UserRound className="size-4" aria-hidden /> Account
          </Link>
          <button onClick={onSwitch} className={item}>
            <Users className="size-4" aria-hidden /> Switch profile
          </button>
          <button onClick={onSignOut} className={item}>
            <LogOut className="size-4" aria-hidden /> Sign out
          </button>
        </div>
      )}
    </div>
  );
}

export function Shell({ children }: { children: ReactNode }) {
  const { signOut } = useAuth();
  const me = useQuery(meQuery);
  const info = useQuery(systemInfoQuery);
  const libraries = useQuery(librariesQuery);
  const [navOpen, setNavOpen] = useState(false);
  const [switching, setSwitching] = useState(false);
  const music = useMusic();

  const sidebar = (
    <nav aria-label="Libraries" className="flex h-full flex-col gap-1 p-3">
      <Link to="/" className={navItem} activeProps={navActive} activeOptions={{ exact: true }} onClick={() => setNavOpen(false)}>
        <Home className="size-5" aria-hidden /> Home
      </Link>
      <div className="mt-4 mb-1 px-3 text-xs font-semibold tracking-wider text-faint uppercase">{info.data?.serverName ?? "Libraries"}</div>
      {libraries.data?.map((lib) => {
        const Icon = libraryIcons[lib.type];
        return (
          <Link
            key={lib.id}
            to="/library/$libraryId"
            params={{ libraryId: String(lib.id) }}
            className={navItem}
            activeProps={navActive}
            onClick={() => setNavOpen(false)}
          >
            <Icon className="size-5" aria-hidden /> <span className="truncate">{lib.name}</span>
            {lib.scanStatus !== "idle" && <span className="ml-auto size-2 animate-pulse rounded-full bg-accent" title="Scanning" />}
          </Link>
        );
      })}
      {libraries.data?.length === 0 && <p className="px-3 text-sm text-faint">No libraries yet</p>}
      <div className="mt-auto" />
      {me.data?.isAdmin && (
        <Link to="/settings" className={navItem} activeProps={navActive} onClick={() => setNavOpen(false)}>
          <Settings className="size-5" aria-hidden /> Settings
        </Link>
      )}
    </nav>
  );

  return (
    <div className="flex h-full flex-col">
      <UpdateBanner />
      <header className="flex h-14 shrink-0 items-center gap-4 border-b border-border bg-surface px-4">
        <button className="rounded p-1 text-muted hover:text-text lg:hidden" onClick={() => setNavOpen((v) => !v)} aria-label="Toggle navigation">
          <Menu className="size-6" />
        </button>
        <Logo />
        <SearchBox />
        <div className="ml-auto flex items-center gap-3">
          {me.data?.isAdmin && <ActivityIndicator />}
          <UserMenu name={me.data?.displayName ?? ""} onSwitch={() => setSwitching(true)} onSignOut={() => void signOut()} />
        </div>
      </header>
      <div className="flex min-h-0 flex-1">
        <aside className="hidden w-60 shrink-0 border-r border-border bg-surface lg:block">{sidebar}</aside>
        {navOpen && (
          <div className="fixed inset-0 top-14 z-30 lg:hidden">
            <div className="absolute inset-0 bg-black/60" onClick={() => setNavOpen(false)} />
            <aside className="relative h-full w-64 border-r border-border bg-surface">{sidebar}</aside>
          </div>
        )}
        <main className="min-w-0 flex-1 overflow-y-auto">
          {children}
          {music.index >= 0 && <div className="h-20" aria-hidden />}
        </main>
      </div>
      {switching && <ProfileSwitcher onClose={() => setSwitching(false)} />}
    </div>
  );
}
