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
import { SystemLayout } from "@/features/system/SystemLayout";
import { StatusPage } from "@/features/system/StatusPage";
import { TasksPage } from "@/features/system/TasksPage";
import { DownloadersPage } from "@/features/downloaders/DownloadersPage";
import { ActivityLayout } from "@/features/activity/ActivityLayout";
import { QueuePage } from "@/features/activity/QueuePage";
import { BlocklistPage } from "@/features/activity/BlocklistPage";

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
  { path: "/movies", key: "movies", phase: "P4" },
  { path: "/series", key: "series", phase: "P5" },
  { path: "/music", key: "music", phase: "P6" },
  { path: "/requests", key: "requests", phase: "P8" },
  { path: "/calendar", key: "calendar", phase: "P5" },
  { path: "/wanted", key: "wanted", phase: "P4" },
  { path: "/indexers", key: "indexers", phase: "P1" },
] as const;

const upcomingRoutes = UPCOMING.map((s) =>
  createRoute({
    getParentRoute: () => appRoute,
    path: s.path,
    component: () => <ComingSoonPage section={s.key} phase={s.phase} />,
  }),
);

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

const settingsRoute = createRoute({ getParentRoute: () => adminRoute, path: "/settings", component: SettingsLayout });
const settingsIndexRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/", component: GeneralSettingsPage });
const rootFoldersRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/media", component: RootFoldersPage });
const usersRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/users", component: UsersPage });

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
    activityRoute.addChildren([activityIndexRoute, queueRoute, blocklistRoute]),
    accountRoute,
    adminRoute.addChildren([
      downloadersRoute,
      settingsRoute.addChildren([settingsIndexRoute, rootFoldersRoute, usersRoute]),
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
