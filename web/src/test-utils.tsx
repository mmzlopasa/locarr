import { render, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { afterEach, expect, vi } from "vitest";
import type { SystemInfo, User } from "@/api/client";
import { queryKeys } from "@/api/client";
import { createApp } from "@/app/createApp";

// A test that ends mid-navigation would let the router update React after the DOM
// is gone; let it settle first.
const routers = new Set<{ state: { status: string } }>();
afterEach(async () => {
  for (const r of routers) await waitFor(() => expect(r.state.status).toBe("idle")).catch(() => undefined);
  routers.clear();
});

export const testInfo = (over: Partial<SystemInfo> = {}): SystemInfo => ({
  product: "Locarr",
  instanceName: "Locarr",
  version: "0.1.0",
  commit: "abc1234",
  startedAt: new Date().toISOString(),
  setupRequired: false,
  ...over,
});

export const testUser = (over: Partial<User> = {}): User => ({
  id: 1,
  username: "ana",
  role: "admin",
  language: "en",
  disabled: false,
  totpEnabled: false,
  permissions: {
    requestMovies: true,
    requestSeries: true,
    requestMusic: true,
    autoApprove: true,
    request4k: true,
    manageLibrary: true,
    manageRequests: true,
  },
  createdAt: new Date().toISOString(),
  ...over,
});

export type MockRequest = { method: string; path: string; body: unknown; query: URLSearchParams };
type Handler = (req: MockRequest) => { status: number; body?: unknown } | undefined;

/**
 * Replaces fetch with a small router over API paths (without the /api/v1 prefix).
 * Requests the handler does not answer get a 404 JSON error.
 */
export function mockApi(handler: Handler) {
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const req = input instanceof Request ? input : new Request(String(input), init);
    const url = new URL(req.url, "http://localhost");
    const text = await req.clone().text();
    const res = handler({
      method: req.method,
      path: url.pathname.replace(/^\/api\/v1/, ""),
      body: text ? JSON.parse(text) : undefined,
      query: url.searchParams,
    }) ?? { status: 404, body: { code: "not_found", message: "not found" } };
    const empty = res.body === undefined || res.status === 204;
    return new Response(empty ? null : JSON.stringify(res.body), {
      status: res.status,
      headers: { "Content-Type": "application/json" },
    });
  });
  vi.stubGlobal("fetch", fn);
  return fn;
}

/** Renders the real app at `path` with the given server info and signed-in user. */
export function renderApp(path: string, { info = testInfo(), me = null as User | null } = {}) {
  const { router, queryClient } = createApp(createMemoryHistory({ initialEntries: [path] }));
  queryClient.setDefaultOptions({ queries: { retry: false, staleTime: Infinity, refetchInterval: false } });
  queryClient.setQueryData(queryKeys.info, info);
  queryClient.setQueryData(queryKeys.me, me);
  routers.add(router);
  const utils = render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { ...utils, router, queryClient };
}
