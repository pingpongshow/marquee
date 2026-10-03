import { useQuery } from "@tanstack/react-query";
import {
  Navigate,
  Outlet,
  createRootRoute,
  createRoute,
  createRouter,
  lazyRouteComponent,
  useRouterState,
} from "@tanstack/react-router";
import { systemInfoQuery } from "@/api/queries";
import { Alert, Spinner } from "@/components/ui";
import { JoinPage } from "@/features/auth/JoinPage";
import { LoginPage } from "@/features/auth/LoginPage";
import { SetupPage } from "@/features/auth/SetupPage";
import { ItemPage } from "@/features/browse/ItemPage";
import {
  LibraryPage,
  validateLibrarySearch,
} from "@/features/browse/LibraryPage";
import { PersonPage } from "@/features/browse/PersonPage";
import { HomePage } from "@/features/home/HomePage";
import { MusicProvider } from "@/features/player/MusicPlayer";
import { PlayerPage } from "@/features/player/PlayerPage";
import { PlaylistPage } from "@/features/playlists/PlaylistPage";
import { PlaylistsPage } from "@/features/playlists/PlaylistsPage";
import { MoodStylePage } from "@/features/music/MoodStylePage";
import { SearchPage } from "@/features/search/SearchPage";
import { LinkPage } from "@/features/users/LinkDevice";
import { useAuth } from "@/lib/auth";
import { Shell } from "./Shell";

// Heavier, less-visited pages load on demand to keep the first download small.
const LiveTvPage = lazyRouteComponent(
  () => import("@/features/livetv/LiveTvPage"),
  "LiveTvPage",
);
const LiveWatchPage = lazyRouteComponent(
  () => import("@/features/livetv/LiveTvPage"),
  "LiveWatchPage",
);
const DiscoverPage = lazyRouteComponent(
  () => import("@/features/requests/DiscoverPage"),
  "DiscoverPage",
);
const AccountPage = lazyRouteComponent(
  () => import("@/features/users/AccountPage"),
  "AccountPage",
);
const RemotePage = lazyRouteComponent(
  () => import("@/features/remote/RemotePage"),
  "RemotePage",
);
const RecapPage = lazyRouteComponent(
  () => import("@/features/music/Recap"),
  "RecapPage",
);
const SettingsLayout = lazyRouteComponent(
  () => import("@/features/settings/SettingsLayout"),
  "SettingsLayout",
);
const SettingsSectionPage = lazyRouteComponent(
  () => import("@/features/settings/SettingsSectionPage"),
  "SettingsSectionPage",
);

/** Decides between first-run setup, sign-in, and the app itself. */
function Gate() {
  const { isAuthenticated } = useAuth();
  const info = useQuery(systemInfoQuery);
  // Invite links are for people without an account yet (USER-13).
  const joining = useRouterState({
    select: (s) => s.location.pathname.startsWith("/join/"),
  });
  if (joining) return <Outlet />;
  if (info.isPending) return <Spinner label="Connecting to server" />;
  if (info.isError)
    return (
      <div className="mx-auto max-w-md p-8">
        <Alert tone="error">
          Can’t reach the Marquee server. {info.error.message}
        </Alert>
      </div>
    );
  if (info.data.setupRequired) return <SetupPage />;
  if (!isAuthenticated)
    return (
      <LoginPage
        serverName={info.data.serverName}
        pinSignIn={!!info.data.pinSignIn}
      />
    );
  return (
    <MusicProvider>
      <Shell>
        <Outlet />
      </Shell>
    </MusicProvider>
  );
}

const rootRoute = createRootRoute({ component: Gate });

const homeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: HomePage,
});

const libraryRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/library/$libraryId",
  validateSearch: validateLibrarySearch,
  component: LibraryPage,
});
// Music libraries' Library pages: artists, albums, songs, genres, decades, moods, muse (MUSIC-15).
const librarySectionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/library/$libraryId/$section",
  validateSearch: validateLibrarySearch,
  component: LibraryPage,
});
const itemRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/item/$itemId",
  component: ItemPage,
});

const searchRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/search",
  validateSearch: (
    s: Record<string, unknown>,
  ): { q: string; mode?: "muse"; lib?: number } => ({
    q: typeof s.q === "string" ? s.q : "",
    mode: s.mode === "muse" ? "muse" : undefined, // Muse for movies and shows (USER-15)
    lib: typeof s.lib === "number" ? s.lib : undefined,
  }),
  component: SearchPage,
});
const playRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/play/$itemId",
  validateSearch: (
    s: Record<string, unknown>,
  ): { t?: number; pl?: number; f?: number; g?: string } => ({
    t: typeof s.t === "number" ? s.t : undefined,
    pl: typeof s.pl === "number" ? s.pl : undefined,
    f: typeof s.f === "number" ? s.f : undefined, // a specific version's file (LIB-7)
    g: typeof s.g === "string" ? s.g : undefined, // a watch-together group to join
  }),
  component: PlayerPage,
});
const joinRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/join/$token",
  component: JoinPage,
});
const linkRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/link",
  component: LinkPage,
});
const personRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/person/$personId",
  component: PersonPage,
});
const playlistsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/playlists",
  component: PlaylistsPage,
});
const playlistRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/playlist/$playlistId",
  component: PlaylistPage,
});
const liveTvRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/livetv",
  component: LiveTvPage,
});
const liveWatchRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/livetv/watch/$channelId",
  component: function LiveWatch() {
    const { channelId } = liveWatchRoute.useParams();
    return <LiveWatchPage channelId={Number(channelId)} />;
  },
});
const moodStyleRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/music/$libraryId/$kind/$name",
  component: MoodStylePage,
});
const discoverRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/discover",
  component: DiscoverPage,
});
const accountRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/account",
  component: AccountPage,
});

// Remote control (USER-14): the player list, or one player's controls (?d=deviceId).
const remoteRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/remote",
  validateSearch: (s: Record<string, unknown>): { d?: number } => ({
    d: typeof s.d === "number" ? s.d : undefined,
  }),
  component: RemotePage,
});
// Year in Music (MUSIC-22).
const recapRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/recap",
  validateSearch: (s: Record<string, unknown>): { year?: number } => ({
    year: typeof s.year === "number" ? s.year : undefined,
  }),
  component: RecapPage,
});

const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/settings",
  component: SettingsLayout,
});
const settingsIndexRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "/",
  component: () => (
    <Navigate to="/settings/$section" params={{ section: "general" }} replace />
  ),
});
export const settingsSectionRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "$section",
  component: SettingsSectionPage,
});

const routeTree = rootRoute.addChildren([
  moodStyleRoute,
  liveTvRoute,
  liveWatchRoute,
  discoverRoute,
  homeRoute,
  playRoute,
  libraryRoute,
  librarySectionRoute,
  itemRoute,
  searchRoute,
  accountRoute,
  remoteRoute,
  recapRoute,
  playlistsRoute,
  playlistRoute,
  personRoute,
  linkRoute,
  joinRoute,
  settingsRoute.addChildren([settingsIndexRoute, settingsSectionRoute]),
]);

export const router = createRouter({
  routeTree,
  defaultNotFoundComponent: () => (
    <div className="p-8 text-muted">Page not found.</div>
  ),
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
