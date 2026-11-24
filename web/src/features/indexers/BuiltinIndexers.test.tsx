import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Indexer, IndexerCatalog, IndexerDefinition, IndexerSettings } from "@/api/client";
import { mockApi, renderApp, testUser } from "@/test-utils";

const now = new Date().toISOString();
const settings: IndexerSettings = { rssSyncIntervalMinutes: 15, releaseRetentionDays: 14, searchTimeoutSeconds: 45, searchCacheSeconds: 120,
  allowedHosts: [], blockPrivateNetworks: false, publicTrackers: [], userAgent: "",
  browserUrl: "", browserDefaultUrl: "http://locarr-browser:8490", browserToken: { configured: false }, browserTokenFromEnv: false };
const def = (over: Partial<IndexerDefinition>): IndexerDefinition => ({
  id: "nyaa", name: "Nyaa", description: "Anime and more.", version: 1, builtin: true, overrides: false, protocol: "torrent",
  links: ["https://nyaa.si"], verified: "Observed the RSS feed.", challenge: "never", flaresolverr: "never", rss: true, language: "en",
  searching: [{ type: "search", available: true, params: ["q"] }, { type: "tv", available: true, params: ["q", "season", "ep"] }],
  categories: [{ id: 5000, name: "TV", subcategories: [{ id: 5070, name: "TV/Anime" }] }],
  settings: [{ name: "filter", type: "select", label: "Filter", default: "0", options: [{ value: "0", label: "No filter" }, { value: "2", label: "Trusted only" }] }],
  aliases: [], indexerIds: [], rateLimit: { requests: 12, periodSeconds: 60 }, ...over,
});
const catalog = (over: Partial<IndexerCatalog> = {}): IndexerCatalog => ({
  definitions: [def({}), def({ id: "1337x", name: "1337x", challenge: "always", flaresolverr: "always", rss: false, settings: [] })],
  presets: [{ id: "nzbgeek", name: "NZBgeek", baseUrl: "https://api.nzbgeek.info", apiPath: "/api", siteUrl: "https://nzbgeek.info" },
    { id: "tabularasa", name: "Tabula Rasa", baseUrl: "https://www.tabula-rasa.pw", apiPath: "/api/v1/api", siteUrl: "https://www.tabula-rasa.pw" }],
  errors: [], customDir: "/config/definitions", ...over,
});
const native = (over: Partial<Indexer>): Indexer => ({
  id: 7, name: "Nyaa", kind: "native", protocol: "torrent", baseUrl: "https://nyaa.si", apiPath: "/", apiKey: { configured: false },
  enabled: true, enableRss: true, enableAutomaticSearch: true, enableInteractiveSearch: true, priority: 25, tags: [],
  categories: { movies: [], tv: [5000, 5070], music: [], books: [] }, minimumSeeders: 1, timeoutSeconds: 30, requestLimit: 12,
  requestPeriodSeconds: 60, proxyPassword: { configured: false }, health: { status: "healthy", failures: 0 },
  definition: { id: "nyaa", name: "Nyaa", version: 1, builtin: true, overrides: false, missing: false }, options: { filter: "0" },
  createdAt: now, updatedAt: now, ...over,
});

function serve(state: { list: Indexer[]; catalog: IndexerCatalog; sent: { method: string; path: string; body: unknown }[] }) {
  mockApi(({ method, path, body }) => {
    if (method !== "GET") state.sent.push({ method, path, body });
    if (method === "GET" && path === "/indexers") return { status: 200, body: state.list };
    if (method === "GET" && path === "/indexers/sources") return { status: 200, body: [] };
    if (method === "GET" && path === "/indexers/settings") return { status: 200, body: settings };
    if (method === "GET" && path === "/indexers/definitions") return { status: 200, body: state.catalog };
    if (method === "POST" && path === "/indexers/definitions/reload") return { status: 200, body: state.catalog };
    if (method === "POST" && path === "/indexers") {
      const created = native({ id: 9, name: (body as { name: string }).name });
      state.list = [...state.list, created];
      return { status: 201, body: created };
    }
    if (method === "PUT" && path.startsWith("/indexers/")) return { status: 200, body: state.list[0] };
    if (method === "POST" && path === "/indexers/test") return { status: 200, body: { ok: true, durationMs: 5, probeResults: 3 } };
  });
}

describe("built-in indexers", () => {
  it("adds a built-in site with one click and switches an existing one off", async () => {
    const state = { list: [] as Indexer[], catalog: catalog(), sent: [] as { method: string; path: string; body: unknown }[] };
    serve(state);
    renderApp("/indexers", { me: testUser() });
    await userEvent.click(await screen.findByRole("button", { name: "Add indexer" }));
    expect(screen.getByRole("tab", { name: "Built-in sites" })).toHaveAttribute("aria-selected", "true");
    const row = await screen.findByTestId("builtin-1337x");
    expect(within(row).getByText("Behind a browser check")).toBeInTheDocument();
    expect(within(row).getByText(/No RSS/)).toBeInTheDocument();
    await userEvent.click(within(screen.getByTestId("builtin-nyaa")).getByRole("switch", { name: "Use Nyaa" }));
    await waitFor(() => expect(state.sent).toHaveLength(1));
    expect(state.sent[0]).toMatchObject({ method: "POST", path: "/indexers", body: { name: "Nyaa", kind: "native", definition: "nyaa" } });
    expect(await screen.findByTestId("indexer-Nyaa")).toBeInTheDocument();
  });

  it("switching off a site that is in use keeps it and disables it", async () => {
    const state = { list: [native({})], catalog: catalog(), sent: [] as { method: string; path: string; body: unknown }[] };
    serve(state);
    renderApp("/indexers", { me: testUser() });
    await userEvent.click(await screen.findByRole("button", { name: "Add indexer" }));
    const sw = within(await screen.findByTestId("builtin-nyaa")).getByRole("switch", { name: "Use Nyaa" });
    await waitFor(() => expect(sw).toBeChecked());
    await userEvent.click(sw);
    await waitFor(() => expect(state.sent).toHaveLength(1));
    expect(state.sent[0]).toMatchObject({ method: "PUT", path: "/indexers/7",
      body: { enabled: false, kind: "native", definition: "nyaa", options: { filter: "0" }, baseUrl: "https://nyaa.si" } });
  });

  it("adds a usenet indexer from a preset", async () => {
    const state = { list: [] as Indexer[], catalog: catalog(), sent: [] as { method: string; path: string; body: unknown }[] };
    serve(state);
    renderApp("/indexers", { me: testUser() });
    await userEvent.click(await screen.findByRole("button", { name: "Add indexer" }));
    await userEvent.click(screen.getByRole("tab", { name: "Usenet" }));
    const form = await screen.findByTestId("usenet-form");
    await userEvent.selectOptions(within(form).getByLabelText("Indexer"), "tabularasa");
    expect(within(form).getByText("https://www.tabula-rasa.pw/api/v1/api")).toBeInTheDocument();
    await userEvent.type(within(form).getByLabelText("API key"), "secret");
    await userEvent.click(within(form).getByRole("button", { name: "Test" }));
    expect(await within(form).findByText(/Works: 3 results/)).toBeInTheDocument();
    await userEvent.click(within(form).getByRole("button", { name: "Add" }));
    await waitFor(() => expect(state.sent.some((s) => s.path === "/indexers")).toBe(true));
    expect(state.sent.find((s) => s.path === "/indexers")?.body).toMatchObject({ name: "Tabula Rasa", kind: "newznab",
      baseUrl: "https://www.tabula-rasa.pw", apiPath: "/api/v1/api", apiKey: "secret" });
  });

  it("shows health reasons, quality and custom definition errors, and reloads", async () => {
    const state = {
      list: [native({ health: { status: "failing", failures: 1, lastErrorCode: "challenge", lastError: "cloudflare", parseFailures: 3,
        selfTestAt: now, selfTestOk: false }, definition: { id: "nyaa", name: "Nyaa", version: 2, builtin: false, overrides: true, missing: false } })],
      catalog: catalog({ definitions: [def({ builtin: false, overrides: true, file: "nyaa.yml", version: 2 })],
        errors: [{ file: "broken.yml", message: "not a valid definition" }] }),
      sent: [] as { method: string; path: string; body: unknown }[],
    };
    serve(state);
    renderApp("/indexers", { me: testUser() });
    const row = await screen.findByTestId("indexer-Nyaa");
    expect(within(row).getByTestId("indexer-error")).toHaveTextContent("built-in browser");
    expect(within(row).getByTestId("indexer-quality")).toHaveTextContent("Unreadable results: 3");
    expect(within(row).getByTestId("indexer-quality")).toHaveTextContent("self-test failed");
    expect(within(row).getByText("Overrides built-in")).toBeInTheDocument();
    const panel = await screen.findByTestId("definitions-panel");
    expect(within(panel).getByText("broken.yml")).toBeInTheDocument();
    expect(within(panel).getByText("nyaa.yml")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Reload" }));
    expect(await screen.findByText("Definitions reloaded.")).toBeInTheDocument();

    await userEvent.click(within(row).getByRole("button", { name: "Edit" }));
    const form = await screen.findByTestId("native-form");
    expect(within(form).queryByLabelText("API key")).not.toBeInTheDocument();
    await userEvent.selectOptions(within(form).getByLabelText("Filter"), "2");
    await userEvent.selectOptions(within(form).getByLabelText("Address"), "");
    const addr = within(form).getByLabelText("Custom address");
    await userEvent.clear(addr);
    await userEvent.type(addr, "https://nyaa.mirror.example");
    await userEvent.click(within(form).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(state.sent.some((s) => s.method === "PUT")).toBe(true));
    expect(state.sent.find((s) => s.method === "PUT")?.body).toMatchObject({ options: { filter: "2" }, baseUrl: "https://nyaa.mirror.example" });
  });
});
