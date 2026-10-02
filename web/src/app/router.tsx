import { useQuery } from "@tanstack/react-query";
import {
  Navigate,
  Outlet,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { systemInfoQuery } from "@/api/queries";
import { Alert, Spinner } from "@/components/ui";
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
import { LiveTvPage, LiveWatchPage } from "@/features/livetv/LiveTvPage";
import { DiscoverPage } from "@/features/requests/DiscoverPage";
import { SearchPage } from "@/features/search/SearchPage";
import { AccountPage } from "@/features/users/AccountPage";
import { LinkPage } from "@/features/users/LinkDevice";
import { SettingsLayout } from "@/features/settings/SettingsLayout";
import { SettingsSectionPage } from "@/features/settings/SettingsSectionPage";
import { useAuth } from "@/lib/auth";
import { Shell } from "./Shell";

/** Decides between first-run setup, sign-in, and the app itself. */
function Gate() {
  const { isAuthenticated } = useAuth();
  const info = useQuery(systemInfoQuery);
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
const itemRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/item/$itemId",
  component: ItemPage,
});

const searchRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/search",
  validateSearch: (s: Record<string, unknown>) => ({
    q: typeof s.q === "string" ? s.q : "",
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
  liveTvRoute,
  liveWatchRoute,
  discoverRoute,
  homeRoute,
  playRoute,
  libraryRoute,
  itemRoute,
  searchRoute,
  accountRoute,
  playlistsRoute,
  playlistRoute,
  personRoute,
  linkRoute,
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
