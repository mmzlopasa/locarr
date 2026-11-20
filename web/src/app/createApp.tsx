import type { RouterHistory } from "@tanstack/react-router";
import { queryKeys } from "@/api/client";
import { createQueryClient } from "./queryClient";
import { createAppRouter, type AppRouter } from "./router";

/** Builds the query client and router, wired so a lost session leads to sign-in. */
export function createApp(history?: RouterHistory) {
  let router: AppRouter | undefined;
  const queryClient = createQueryClient(() => {
    queryClient.setQueryData(queryKeys.me, null);
    if (!router) return;
    const { pathname, href } = router.state.location;
    if (pathname === "/login" || pathname === "/setup") return;
    void router.navigate({ to: "/login", search: { redirect: href } });
  });
  router = createAppRouter(queryClient, history);
  return { router, queryClient };
}
