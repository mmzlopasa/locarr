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
import { ArtistsPage } from "@/features/music/ArtistsPage";
import { AddArtistPage } from "@/features/music/AddArtistPage";
import { DiscoverArtistsPage, validateDiscoverArtistsSearch } from "@/features/music/DiscoverArtistsPage";
import { ArtistDetailPage } from "@/features/music/ArtistDetailPage";
import { AlbumDetailPage } from "@/features/music/AlbumDetailPage";
import { MusicImportPage } from "@/features/music/MusicImportPage";
import { MetadataProfilesPage } from "@/features/music/MetadataProfilesPage";
import { SubtitleSettingsPage } from "@/features/subtitles/SubtitleSettingsPage";
import { AdultSettingsPage } from "@/features/settings/AdultSettingsPage";
import {
  AdultAddPage, AdultCalendarPage, AdultItemPage, AdultLayout, AdultMoviesPage, AdultPerformerPage, AdultPerformersPage, AdultScenesPage,
  AdultStudioPage, AdultStudiosPage, AdultWantedPage,
} from "@/features/adult/AdultPages";
import { MigratePage } from "@/features/migrate/MigratePage";
import { ChecklistPage } from "@/features/migrate/ChecklistPage";
import { JoinPage } from "@/routes/JoinPage";
import { DiscoverPage } from "@/features/discover/DiscoverPage";
import { DiscoverListPage, validateListSearch } from "@/features/discover/DiscoverListPage";
import { DiscoverSearchPage, validateDiscoverSearch } from "@/features/discover/DiscoverSearchPage";
import { ArtistTitlePage, MovieTitlePage, PersonPage, SeriesTitlePage } from "@/features/discover/TitlePage";
import { RequestsPage, validateRequestsSearch } from "@/features/requests/RequestsPage";
import { RequestDetailPage } from "@/features/requests/RequestDetailPage";
import { IssueDetailPage, IssuesPage, validateIssuesSearch } from "@/features/issues/IssuesPage";
import { RequestSettingsPage } from "@/features/settings/RequestSettingsPage";
import { MediaServerPage } from "@/features/settings/MediaServerPage";
import { NotificationSettingsPage } from "@/features/settings/NotificationSettingsPage";
import { canManageLibrary } from "@/features/users/permissions";
import { BoardPage } from "@/features/board/BoardPage";
import { HistoryPage } from "@/features/activity/HistoryPage";
import { StatsPage } from "@/features/activity/StatsPage";
import { HealthPage } from "@/features/system/HealthPage";
import { LogsPage } from "@/features/system/LogsPage";
import { BackupsPage } from "@/features/system/BackupsPage";
import { MonitoringSettingsPage } from "@/features/settings/MonitoringSettingsPage";
import { OverviewPage } from "@/features/overview/OverviewPage";
import { WelcomePage } from "@/features/welcome/WelcomePage";
import type { User } from "@/api/client";

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

/** Administrators get the overview; library managers their movies; requesters Discover. */
const indexRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/",
  beforeLoad: ({ context }) => {
    if (context.me.role === "admin") return;
    throw redirect({ to: canManageLibrary(context.me) ? "/movies" : "/discover" });
  },
  component: OverviewPage,
});

/** Library pages (titles, paths, downloads) are for people who manage the library. */
const libraryOnly = ({ context }: { context: { me: User } }) => {
  if (!canManageLibrary(context.me)) throw redirect({ to: "/discover" });
};

const joinRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/join",
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.ensureQueryData({ queryKey: queryKeys.me, queryFn: fetchMe });
    if (me) throw redirect({ to: "/" });
  },
  component: JoinPage,
});

const discoverRoute = createRoute({ getParentRoute: () => appRoute, path: "/discover", component: DiscoverPage });
const discoverSearchRoute = createRoute({ getParentRoute: () => appRoute, path: "/discover/search", validateSearch: validateDiscoverSearch,
  component: DiscoverSearchPage });
const discoverListRoute = createRoute({ getParentRoute: () => appRoute, path: "/discover/list/$key", validateSearch: validateListSearch,
  component: DiscoverListPage });
const discoverMovieRoute = createRoute({ getParentRoute: () => appRoute, path: "/discover/movie/$tmdbId", component: MovieTitlePage });
const discoverSeriesRoute = createRoute({ getParentRoute: () => appRoute, path: "/discover/series/$tmdbId", component: SeriesTitlePage });
const discoverPersonRoute = createRoute({ getParentRoute: () => appRoute, path: "/discover/person/$tmdbId", component: PersonPage });
const discoverArtistRoute = createRoute({ getParentRoute: () => appRoute, path: "/discover/artist/$mbid", component: ArtistTitlePage });
const requestsRoute = createRoute({ getParentRoute: () => appRoute, path: "/requests", validateSearch: validateRequestsSearch, component: RequestsPage });
const requestRoute = createRoute({ getParentRoute: () => appRoute, path: "/requests/$id", component: RequestDetailPage });
const issuesRoute = createRoute({ getParentRoute: () => appRoute, path: "/issues", validateSearch: validateIssuesSearch, component: IssuesPage });
const issueRoute = createRoute({ getParentRoute: () => appRoute, path: "/issues/$id", component: IssueDetailPage });

const moviesRoute = createRoute({ getParentRoute: () => appRoute, beforeLoad: libraryOnly, path: "/movies", component: MoviesPage });
const addMovieRoute = createRoute({ getParentRoute: () => appRoute, beforeLoad: libraryOnly, path: "/movies/add", component: AddMoviePage });
const importMoviesRoute = createRoute({ getParentRoute: () => appRoute, beforeLoad: libraryOnly, path: "/movies/import", component: ImportPage });
const movieRoute = createRoute({ getParentRoute: () => appRoute, beforeLoad: libraryOnly, path: "/movies/$id", component: MovieDetailPage });
const collectionRoute = createRoute({ getParentRoute: () => appRoute, beforeLoad: libraryOnly, path: "/movies/collections/$id", component: CollectionPage });
const seriesRoute = createRoute({ getParentRoute: () => appRoute, beforeLoad: libraryOnly, path: "/series", component: SeriesPage });
const addSeriesRoute = createRoute({ getParentRoute: () => appRoute, beforeLoad: libraryOnly, path: "/series/add", component: AddSeriesPage });
const importSeriesRoute = createRoute({ getParentRoute: () => appRoute, beforeLoad: libraryOnly, path: "/series/import", component: SeriesImportPage });
const seriesDetailRoute = createRoute({ getParentRoute: () => appRoute, beforeLoad: libraryOnly, path: "/series/$id", component: SeriesDetailPage });
const musicRoute = createRoute({ getParentRoute: () => appRoute, beforeLoad: libraryOnly, path: "/music", component: ArtistsPage });
const discoverArtistsRoute = createRoute({ getParentRoute: () => appRoute, beforeLoad: libraryOnly, path: "/music/discover",
  validateSearch: validateDiscoverArtistsSearch, component: DiscoverArtistsPage });
const addArtistRoute = createRoute({ getParentRoute: () => appRoute, beforeLoad: libraryOnly, path: "/music/add", component: AddArtistPage });
const importMusicRoute = createRoute({ getParentRoute: () => appRoute, beforeLoad: libraryOnly, path: "/music/import", component: MusicImportPage });
const artistRoute = createRoute({ getParentRoute: () => appRoute, beforeLoad: libraryOnly, path: "/music/artists/$id", component: ArtistDetailPage });
const albumRoute = createRoute({ getParentRoute: () => appRoute, beforeLoad: libraryOnly, path: "/music/albums/$id", component: AlbumDetailPage });
const wantedRoute = createRoute({ getParentRoute: () => appRoute, beforeLoad: libraryOnly, path: "/wanted", component: WantedPage });
const calendarRoute = createRoute({ getParentRoute: () => appRoute, beforeLoad: libraryOnly, path: "/calendar", component: CalendarPage });

const activityRoute = createRoute({ getParentRoute: () => appRoute, beforeLoad: libraryOnly, path: "/activity", component: ActivityLayout });
const activityIndexRoute = createRoute({
  getParentRoute: () => activityRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/activity/queue" });
  },
});
const queueRoute = createRoute({ getParentRoute: () => activityRoute, path: "/queue", component: QueuePage });
const blocklistRoute = createRoute({ getParentRoute: () => activityRoute, path: "/blocklist", component: BlocklistPage });
const boardRoute = createRoute({ getParentRoute: () => activityRoute, path: "/board", component: BoardPage });
const historyRoute = createRoute({ getParentRoute: () => activityRoute, path: "/history", component: HistoryPage });
const activityStatsRoute = createRoute({ getParentRoute: () => activityRoute, path: "/stats", component: StatsPage });
const searchRoute = createRoute({ getParentRoute: () => appRoute, beforeLoad: libraryOnly, path: "/search", component: SearchPage });

const accountRoute = createRoute({ getParentRoute: () => appRoute, path: "/account", component: AccountPage });

/** Administrator-only pages. */
const adminRoute = createRoute({
  getParentRoute: () => appRoute,
  id: "admin",
  beforeLoad: ({ context }) => {
    if (context.me.role !== "admin") throw redirect({ to: canManageLibrary(context.me) ? "/movies" : "/discover" });
  },
  component: Outlet,
});

const downloadersRoute = createRoute({ getParentRoute: () => adminRoute, path: "/downloaders", component: DownloadersPage });

const indexersRoute = createRoute({ getParentRoute: () => adminRoute, path: "/indexers", component: IndexersPage });
const migrateRoute = createRoute({ getParentRoute: () => adminRoute, path: "/migrate", component: MigratePage });
const migrateChecklistRoute = createRoute({ getParentRoute: () => adminRoute, path: "/migrate/checklist", component: ChecklistPage });
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
const requestSettingsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/requests", component: RequestSettingsPage });
const metadataProfilesRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/metadataprofiles", component: MetadataProfilesPage });
const importListsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/importlists", component: ImportListsPage });
const mediaServerRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/mediaserver", component: MediaServerPage });
const notificationSettingsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/notifications", component: NotificationSettingsPage });
const subtitleSettingsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/subtitles", component: SubtitleSettingsPage });
const monitoringSettingsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/system", component: MonitoringSettingsPage });
const adultSettingsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/adult", component: AdultSettingsPage });

/** The adult section exists only for accounts the server lets see it (enabled and view_adult). */
const adultRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/adult",
  beforeLoad: ({ context }) => {
    if (!context.me.adultVisible) throw redirect({ to: "/" });
  },
  component: AdultLayout,
});
const adultScenesRoute = createRoute({ getParentRoute: () => adultRoute, path: "/", component: AdultScenesPage });
const adultMoviesRoute = createRoute({ getParentRoute: () => adultRoute, path: "/movies", component: AdultMoviesPage });
const adultStudiosRoute = createRoute({ getParentRoute: () => adultRoute, path: "/studios", component: AdultStudiosPage });
const adultStudioRoute = createRoute({ getParentRoute: () => adultRoute, path: "/studios/$id", component: AdultStudioPage });
const adultPerformersRoute = createRoute({ getParentRoute: () => adultRoute, path: "/performers", component: AdultPerformersPage });
const adultPerformerRoute = createRoute({ getParentRoute: () => adultRoute, path: "/performers/$id", component: AdultPerformerPage });
const adultItemRoute = createRoute({ getParentRoute: () => adultRoute, path: "/items/$id", component: AdultItemPage });
const adultWantedRoute = createRoute({ getParentRoute: () => adultRoute, path: "/wanted", component: AdultWantedPage });
const adultCalendarRoute = createRoute({ getParentRoute: () => adultRoute, path: "/calendar", component: AdultCalendarPage });
const adultAddRoute = createRoute({ getParentRoute: () => adultRoute, path: "/add", component: AdultAddPage });
const welcomeRoute = createRoute({ getParentRoute: () => adminRoute, path: "/welcome", component: WelcomePage });

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
const healthRoute = createRoute({ getParentRoute: () => systemRoute, path: "/health", component: HealthPage });
const logsRoute = createRoute({ getParentRoute: () => systemRoute, path: "/logs", component: LogsPage });
const backupsRoute = createRoute({ getParentRoute: () => systemRoute, path: "/backups", component: () => <BackupsPage /> });

export const routeTree = rootRoute.addChildren([
  setupRoute,
  loginRoute,
  joinRoute,
  appRoute.addChildren([
    indexRoute,
    discoverRoute,
    discoverSearchRoute,
    discoverListRoute,
    discoverMovieRoute,
    discoverSeriesRoute,
    discoverPersonRoute,
    discoverArtistRoute,
    requestsRoute,
    requestRoute,
    issuesRoute,
    issueRoute,
    moviesRoute,
    addMovieRoute,
    importMoviesRoute,
    movieRoute,
    collectionRoute,
    seriesRoute,
    addSeriesRoute,
    importSeriesRoute,
    seriesDetailRoute,
    musicRoute,
    discoverArtistsRoute,
    addArtistRoute,
    importMusicRoute,
    artistRoute,
    albumRoute,
    wantedRoute,
    calendarRoute,
    activityRoute.addChildren([activityIndexRoute, queueRoute, boardRoute, historyRoute, blocklistRoute, activityStatsRoute]),
    searchRoute,
    accountRoute,
    adultRoute.addChildren([adultScenesRoute, adultMoviesRoute, adultStudiosRoute, adultStudioRoute, adultPerformersRoute, adultPerformerRoute,
      adultItemRoute, adultWantedRoute, adultCalendarRoute, adultAddRoute]),
    adminRoute.addChildren([
      downloadersRoute,
      indexersRoute,
      migrateRoute,
      migrateChecklistRoute,
      settingsRoute.addChildren([settingsIndexRoute, rootFoldersRoute, usersRoute, profilesRoute, qualityRoute, customFormatsRoute,
        rulesRoute, testerRoute, mediaManagementRoute, metadataRoute, importListsRoute, metadataProfilesRoute, subtitleSettingsRoute,
        requestSettingsRoute, mediaServerRoute, notificationSettingsRoute, monitoringSettingsRoute, adultSettingsRoute]),
      systemRoute.addChildren([systemIndexRoute, statusRoute, healthRoute, tasksRoute, logsRoute, backupsRoute]),
      welcomeRoute,
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
