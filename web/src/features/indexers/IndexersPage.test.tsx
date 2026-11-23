import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Indexer, IndexerSettings } from "@/api/client";
import { mockApi, renderApp, testUser } from "@/test-utils";

const now = new Date().toISOString();
const caps = {
  searching: [{ type: "search" as const, available: true, params: ["q"] }],
  categories: [{ id: 2000, name: "Movies", subcategories: [{ id: 2040, name: "Movies/HD" }] }, { id: 5000, name: "TV" }],
};
const indexer = (over: Partial<Indexer>): Indexer => ({
  id: 1, name: "Alpha", kind: "torznab", protocol: "torrent", baseUrl: "http://alpha", apiPath: "/api", apiKey: { configured: true },
  enabled: true, enableRss: true, enableAutomaticSearch: true, enableInteractiveSearch: true, priority: 25, tags: [],
  categories: { movies: [2000], tv: [], music: [], books: [] }, minimumSeeders: 1, timeoutSeconds: 30,
  proxyPassword: { configured: false }, health: { status: "healthy", failures: 0 }, createdAt: now, updatedAt: now, ...over,
});
const settings: IndexerSettings = { rssSyncIntervalMinutes: 15, releaseRetentionDays: 14, searchTimeoutSeconds: 45, searchCacheSeconds: 120,
  allowedHosts: [], blockPrivateNetworks: false, publicTrackers: ["udp://tracker.example:1337/announce"], userAgent: "" };

describe("indexers page", () => {
  it("lists indexers with health and adds one after a test that loads the categories", async () => {
    const created: unknown[] = [];
    let list = [indexer({}), indexer({ id: 2, name: "Beta", health: { status: "failing", failures: 3, lastError: "HTTP 503",
      lastErrorCode: "unreachable", retryAt: new Date(Date.now() + 600_000).toISOString() } })];
    mockApi(({ method, path, body }) => {
      if (method === "GET" && path === "/indexers") return { status: 200, body: list };
      if (method === "GET" && path === "/indexers/sources") return { status: 200, body: [] };
      if (method === "GET" && path === "/indexers/settings") return { status: 200, body: settings };
      if (method === "POST" && path === "/indexers/test") {
        return { status: 200, body: { ok: true, durationMs: 12, probeResults: 7, caps,
          defaultCategories: { movies: [2000, 2040], tv: [5000], music: [], books: [] } } };
      }
      if (method === "POST" && path === "/indexers") {
        created.push(body);
        list = [...list, indexer({ id: 3, name: "Gamma" })];
        return { status: 201, body: list[2] };
      }
    });
    renderApp("/indexers", { me: testUser() });
    const beta = await screen.findByTestId("indexer-Beta");
    expect(within(beta).getByText("Failing")).toBeInTheDocument();
    expect(within(beta).getByTestId("indexer-error")).toHaveTextContent("cannot be reached");
    expect(within(screen.getByTestId("indexer-Alpha")).getByText("Healthy")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Add indexer" }));
    await userEvent.click(screen.getByRole("tab", { name: "Torznab / Newznab" }));
    const form = screen.getByTestId("indexer-form");
    await userEvent.type(within(form).getByLabelText("Name"), "Gamma");
    await userEvent.type(within(form).getByLabelText("URL"), "http://gamma:9117");
    await userEvent.type(within(form).getByLabelText("API key"), "k3y");
    await userEvent.click(within(form).getByRole("button", { name: "Test" }));
    expect(await within(form).findByText(/Works: 7 results/)).toBeInTheDocument();
    const picker = within(form).getByTestId("category-picker");
    expect(within(picker).getByLabelText("TV: TV")).toBeChecked();
    await userEvent.click(within(picker).getByLabelText("Movies: Movies/HD"));
    await userEvent.click(within(form).getByRole("button", { name: "Add" }));
    await waitFor(() => expect(created).toHaveLength(1));
    expect(created[0]).toMatchObject({ name: "Gamma", kind: "torznab", baseUrl: "http://gamma:9117", apiKey: "k3y",
      categories: { movies: [2000], tv: [5000] } });
    expect(await screen.findByTestId("indexer-Gamma")).toBeInTheDocument();
  });

  it("imports from Prowlarr after a preview", async () => {
    const sent: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (method === "GET" && path === "/indexers") return { status: 200, body: [] };
      if (method === "GET" && path === "/indexers/sources") return { status: 200, body: [] };
      if (method === "GET" && path === "/indexers/settings") return { status: 200, body: settings };
      if (method === "POST" && path === "/indexers/sources/preview") {
        return { status: 200, body: { items: [
          { ref: "1", name: "One", kind: "torznab", protocol: "torrent", feedUrl: "http://p/1/api", action: "add" },
          { ref: "2", name: "Two", kind: "newznab", protocol: "usenet", feedUrl: "http://p/2/api", action: "add" },
        ] } };
      }
      if (method === "POST" && path === "/indexers/sources") {
        sent.push(body);
        return { status: 201, body: { added: 1, updated: 0, disabled: 0, unchanged: 0,
          source: { id: 1, kind: "prowlarr", name: "Prowlarr", url: "http://p", apiKey: { configured: true }, exclude: ["2"], indexerCount: 1, createdAt: now } } };
      }
    });
    renderApp("/indexers", { me: testUser() });
    expect(screen.queryByRole("button", { name: "Import from Prowlarr or Jackett" })).not.toBeInTheDocument();
    await userEvent.click(await screen.findByRole("button", { name: "Show Prowlarr and Jackett import" }));
    await userEvent.click(await screen.findByRole("button", { name: "Import from Prowlarr or Jackett" }));
    const wiz = screen.getByTestId("import-wizard");
    await userEvent.type(within(wiz).getByLabelText("URL"), "http://p");
    await userEvent.type(within(wiz).getByLabelText("API key"), "key");
    await userEvent.click(within(wiz).getByRole("button", { name: "Show indexers" }));
    const preview = await within(wiz).findByTestId("import-preview");
    await userEvent.click(within(preview).getByLabelText("Two"));
    await userEvent.click(within(wiz).getByRole("button", { name: "Import 1" }));
    expect(await within(wiz).findByText("Imported 1 indexers.")).toBeInTheDocument();
    expect(sent[0]).toEqual({ kind: "prowlarr", url: "http://p", apiKey: "key", exclude: ["2"] });
  });
});
