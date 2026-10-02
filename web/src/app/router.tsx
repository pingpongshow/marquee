import { useQuery } from "@tanstack/react-query";
import { Navigate, Outlet, createRootRoute, createRoute, createRouter } from "@tanstack/react-router";
import { systemInfoQuery } from "@/api/queries";
import { Alert, Spinner } from "@/components/ui";
import { LoginPage } from "@/features/auth/LoginPage";
import { SetupPage } from "@/features/auth/SetupPage";
import { ItemPage } from "@/features/browse/ItemPage";
import { LibraryPage } from "@/features/browse/LibraryPage";
import { HomePage } from "@/features/home/HomePage";
import { MusicProvider } from "@/features/player/MusicPlayer";
import { PlayerPage } from "@/features/player/PlayerPage";
import { SearchPage } from "@/features/search/SearchPage";
import { AccountPage } from "@/features/users/AccountPage";
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
        <Alert tone="error">Can’t reach the Marquee server. {info.error.message}</Alert>
      </div>
    );
  if (info.data.setupRequired) return <SetupPage />;
  if (!isAuthenticated) return <LoginPage serverName={info.data.serverName} pinSignIn={!!info.data.pinSignIn} />;
  return (
    <MusicProvider>
      <Shell>
        <Outlet />
      </Shell>
    </MusicProvider>
  );
}

const rootRoute = createRootRoute({ component: Gate });

const homeRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", component: HomePage });

const libraryRoute = createRoute({ getParentRoute: () => rootRoute, path: "/library/$libraryId", component: LibraryPage });
const itemRoute = createRoute({ getParentRoute: () => rootRoute, path: "/item/$itemId", component: ItemPage });

const searchRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/search",
  validateSearch: (s: Record<string, unknown>) => ({ q: typeof s.q === "string" ? s.q : "" }),
  component: SearchPage,
});
const playRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/play/$itemId",
  validateSearch: (s: Record<string, unknown>) => ({ t: typeof s.t === "number" ? s.t : undefined }),
  component: PlayerPage,
});
const accountRoute = createRoute({ getParentRoute: () => rootRoute, path: "/account", component: AccountPage });

const settingsRoute = createRoute({ getParentRoute: () => rootRoute, path: "/settings", component: SettingsLayout });
const settingsIndexRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "/",
  component: () => <Navigate to="/settings/$section" params={{ section: "general" }} replace />,
});
export const settingsSectionRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "$section",
  component: SettingsSectionPage,
});

const routeTree = rootRoute.addChildren([homeRoute, playRoute, libraryRoute, itemRoute, searchRoute, accountRoute, settingsRoute.addChildren([settingsIndexRoute, settingsSectionRoute])]);

export const router = createRouter({
  routeTree,
  defaultNotFoundComponent: () => <div className="p-8 text-muted">Page not found.</div>,
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
