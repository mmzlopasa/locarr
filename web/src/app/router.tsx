import type { QueryClient } from "@tanstack/react-query";
import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  Outlet,
  redirect,
  type RouterHistory,
} from "@tanstack/react-router";
import { fetchMe, fetchSystemInfo, queryKeys } from "@/api/client";
import { AppShell } from "@/features/shell/AppShell";
import { ComingSoonPage } from "@/routes/ComingSoonPage";
import { LoginPage } from "@/routes/LoginPage";
import { SetupPage } from "@/routes/SetupPage";
import { NotFoundPage } from "@/routes/NotFoundPage";
import { RouteErrorPage } from "@/routes/ErrorPage";
import { AccountPage } from "@/features/account/AccountPage";
import { UsersPage } from "@/features/users/UsersPage";
import { SettingsLayout } from "@/features/settings/SettingsLayout";
import { GeneralSettingsPage } from "@/features/settings/GeneralSettingsPage";
import { RootFoldersPage } from "@/features/settings/RootFoldersPage";
import { QualityProfilesPage } from "@/features/profiles/QualityProfilesPage";
import { QualityDefinitionsPage } from "@/features/profiles/QualityDefinitionsPage";
import { CustomFormatsPage } from "@/features/profiles/CustomFormatsPage";
import { ReleaseRulesPage } from "@/features/profiles/ReleaseRulesPage";
import { ReleaseTesterPage } from "@/features/profiles/ReleaseTesterPage";
import { SystemLayout } from "@/features/system/SystemLayout";
import { StatusPage } from "@/features/system/StatusPage";
import { TasksPage } from "@/features/system/TasksPage";
import { DownloadersPage } from "@/features/downloaders/DownloadersPage";
import { ActivityLayout } from "@/features/activity/ActivityLayout";
import { QueuePage } from "@/features/activity/QueuePage";
import { BlocklistPage } from "@/features/activity/BlocklistPage";
import { IndexersPage } from "@/features/indexers/IndexersPage";
import { SearchPage } from "@/features/search/SearchPage";
import { MoviesPage } from "@/features/movies/MoviesPage";
import { AddMoviePage } from "@/features/movies/AddMoviePage";
import { MovieDetailPage } from "@/features/movies/MovieDetailPage";
import { CollectionPage } from "@/features/movies/CollectionPage";
import { ImportPage } from "@/features/movies/ImportPage";
import { WantedPage } from "@/features/wanted/WantedPage";
import { SeriesPage } from "@/features/series/SeriesPage";
import { AddSeriesPage } from "@/features/series/AddSeriesPage";
import { SeriesImportPage } from "@/features/series/SeriesImportPage";
import { SeriesDetailPage } from "@/features/series/SeriesDetailPage";
import { CalendarPage } from "@/features/calendar/CalendarPage";
import { MediaManagementPage } from "@/features/settings/MediaManagementPage";
import { ImportListsPage, MetadataPage } from "@/features/settings/MetadataPage";

export interface RouterContext {
  queryClient: QueryClient;
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: Outlet,
  // Until an administrator exists, every page is the setup page (and vice versa).
  beforeLoad: async ({ context, location }) => {
    const info = await context.queryClient.ensureQueryData({ queryKey: queryKeys.info, queryFn: fetchSystemInfo });
    const onSetup = location.pathname === "/setup";
    if (info.setupRequired && !onSetup) throw redirect({ to: "/setup" });
    if (!info.setupRequired && onSetup) throw redirect({ to: "/login", search: {} });
  },
});

const setupRoute = createRoute({ getParentRoute: () => rootRoute, path: "/setup", component: SetupPage });

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  validateSearch: (s: Record<string, unknown>): { redirect?: string } =>
    typeof s.redirect === "string" ? { redirect: s.redirect } : {},
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.ensureQueryData({ queryKey: queryKeys.me, queryFn: fetchMe });
    if (me) throw redirect({ to: "/" });
  },
  component: LoginPage,
});

/** Everything behind sign-in. */
const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "app",
  beforeLoad: async ({ context, location }) => {
    const me = await context.queryClient.ensureQueryData({ queryKey: queryKeys.me, queryFn: fetchMe });
    if (!me) throw redirect({ to: "/login", search: location.href === "/" ? {} : { redirect: location.href } });
    return { me };
  },
  component: AppShell,
});

const indexRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/movies" });
  },
});

/** Sections that later phases fill in; each says honestly when it arrives. */
export const UPCOMING = [
  { path: "/music", key: "music", phase: "P6" },
  { path: "/requests", key: "requests", phase: "P8" },
] as const;

const upcomingRoutes = UPCOMING.map((s) =>
  createRoute({
    getParentRoute: () => appRoute,
    path: s.path,
    component: () => <ComingSoonPage section={s.key} phase={s.phase} />,
  }),
);

const moviesRoute = createRoute({ getParentRoute: () => appRoute, path: "/movies", component: MoviesPage });
const addMovieRoute = createRoute({ getParentRoute: () => appRoute, path: "/movies/add", component: AddMoviePage });
const importMoviesRoute = createRoute({ getParentRoute: () => appRoute, path: "/movies/import", component: ImportPage });
const movieRoute = createRoute({ getParentRoute: () => appRoute, path: "/movies/$id", component: MovieDetailPage });
const collectionRoute = createRoute({ getParentRoute: () => appRoute, path: "/movies/collections/$id", component: CollectionPage });
const seriesRoute = createRoute({ getParentRoute: () => appRoute, path: "/series", component: SeriesPage });
const addSeriesRoute = createRoute({ getParentRoute: () => appRoute, path: "/series/add", component: AddSeriesPage });
const importSeriesRoute = createRoute({ getParentRoute: () => appRoute, path: "/series/import", component: SeriesImportPage });
const seriesDetailRoute = createRoute({ getParentRoute: () => appRoute, path: "/series/$id", component: SeriesDetailPage });
const wantedRoute = createRoute({ getParentRoute: () => appRoute, path: "/wanted", component: WantedPage });
const calendarRoute = createRoute({ getParentRoute: () => appRoute, path: "/calendar", component: CalendarPage });

const activityRoute = createRoute({ getParentRoute: () => appRoute, path: "/activity", component: ActivityLayout });
const activityIndexRoute = createRoute({
  getParentRoute: () => activityRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/activity/queue" });
  },
});
const queueRoute = createRoute({ getParentRoute: () => activityRoute, path: "/queue", component: QueuePage });
const blocklistRoute = createRoute({ getParentRoute: () => activityRoute, path: "/blocklist", component: BlocklistPage });
const searchRoute = createRoute({ getParentRoute: () => appRoute, path: "/search", component: SearchPage });

const accountRoute = createRoute({ getParentRoute: () => appRoute, path: "/account", component: AccountPage });

/** Administrator-only pages. */
const adminRoute = createRoute({
  getParentRoute: () => appRoute,
  id: "admin",
  beforeLoad: ({ context }) => {
    if (context.me.role !== "admin") throw redirect({ to: "/" });
  },
  component: Outlet,
});

const downloadersRoute = createRoute({ getParentRoute: () => adminRoute, path: "/downloaders", component: DownloadersPage });

const indexersRoute = createRoute({ getParentRoute: () => adminRoute, path: "/indexers", component: IndexersPage });
const settingsRoute = createRoute({ getParentRoute: () => adminRoute, path: "/settings", component: SettingsLayout });
const settingsIndexRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/", component: GeneralSettingsPage });
const rootFoldersRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/media", component: RootFoldersPage });
const usersRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/users", component: UsersPage });
const profilesRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/profiles", component: QualityProfilesPage });
const qualityRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/quality", component: QualityDefinitionsPage });
const customFormatsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/customformats", component: CustomFormatsPage });
const rulesRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/rules", component: ReleaseRulesPage });
const testerRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/tester", component: ReleaseTesterPage });
const mediaManagementRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/mediamanagement", component: MediaManagementPage });
const metadataRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/metadata", component: MetadataPage });
const importListsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/importlists", component: ImportListsPage });

const systemRoute = createRoute({ getParentRoute: () => adminRoute, path: "/system", component: SystemLayout });
const systemIndexRoute = createRoute({
  getParentRoute: () => systemRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/system/status" });
  },
});
const statusRoute = createRoute({ getParentRoute: () => systemRoute, path: "/status", component: StatusPage });
const tasksRoute = createRoute({ getParentRoute: () => systemRoute, path: "/tasks", component: TasksPage });

export const routeTree = rootRoute.addChildren([
  setupRoute,
  loginRoute,
  appRoute.addChildren([
    indexRoute,
    ...upcomingRoutes,
    moviesRoute,
    addMovieRoute,
    importMoviesRoute,
    movieRoute,
    collectionRoute,
    seriesRoute,
    addSeriesRoute,
    importSeriesRoute,
    seriesDetailRoute,
    wantedRoute,
    calendarRoute,
    activityRoute.addChildren([activityIndexRoute, queueRoute, blocklistRoute]),
    searchRoute,
    accountRoute,
    adminRoute.addChildren([
      downloadersRoute,
      indexersRoute,
      settingsRoute.addChildren([settingsIndexRoute, rootFoldersRoute, usersRoute, profilesRoute, qualityRoute, customFormatsRoute,
        rulesRoute, testerRoute, mediaManagementRoute, metadataRoute, importListsRoute]),
      systemRoute.addChildren([systemIndexRoute, statusRoute, tasksRoute]),
    ]),
  ]),
]);

export function createAppRouter(queryClient: QueryClient, history?: RouterHistory) {
  return createRouter({
    routeTree,
    context: { queryClient },
    history,
    defaultErrorComponent: RouteErrorPage,
    defaultNotFoundComponent: NotFoundPage,
  });
}

export type AppRouter = ReturnType<typeof createAppRouter>;

declare module "@tanstack/react-router" {
  interface Register {
    router: AppRouter;
  }
}
