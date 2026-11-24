import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Indexer } from "@/api/client";
import { mockApi, renderApp, testUser } from "@/test-utils";

const now = new Date().toISOString();
const indexer = (over: Partial<Indexer>): Indexer => ({
  id: 1, name: "Site", kind: "native", protocol: "torrent", baseUrl: "https://example.invalid", apiPath: "/", apiKey: { configured: false },
  enabled: true, enableRss: true, enableAutomaticSearch: true, enableInteractiveSearch: true, priority: 25, tags: [],
  categories: { movies: [2000], tv: [], music: [], books: [] }, minimumSeeders: 1, timeoutSeconds: 30,
  proxyPassword: { configured: false }, health: { status: "healthy", failures: 0 }, createdAt: now, updatedAt: now, ...over,
});

describe("adult settings: indexers", () => {
  it("offers only indexers that carry adult content and lets a stale opt-in be removed", async () => {
    const list = [
      indexer({ id: 1, name: "General Site", adultCapable: true }),
      indexer({ id: 2, name: "Adult Site", adultCapable: true, categories: { movies: [], tv: [], music: [], books: [], adult: [6000] } }),
      indexer({ id: 3, name: "Movies Only", adultCapable: false }),
      indexer({ id: 4, name: "Old Choice", adultCapable: false, categories: { movies: [], tv: [5000], music: [], books: [], adult: [6000] } }),
    ];
    const sent: { path: string; body: unknown }[] = [];
    mockApi(({ method, path, body }) => {
      if (method === "GET" && path === "/adult/settings") {
        return { status: 200, body: { enabled: true, stashdbKeySet: false, tpdbKeySet: false, defaultSource: "stashdb", defaultProfileId: 0,
          defaultRootFolderId: 0, sceneFolder: "", sceneFile: "", movieFolder: "", movieFile: "", statsAggregate: false,
          notifyStreamero: false, tokens: [] } };
      }
      if (method === "GET" && path === "/indexers") return { status: 200, body: list };
      if (method === "GET" && (path === "/adult/rootfolders" || path === "/adult/blocklist" || path === "/qualityprofiles")) return { status: 200, body: [] };
      if (method === "PUT" && path.startsWith("/indexers/")) {
        sent.push({ path, body });
        return { status: 200, body: list[0] };
      }
    });
    renderApp("/settings/adult", { me: testUser() });
    const offered = await screen.findByTestId("adult-indexers");
    expect(await within(offered).findByRole("checkbox", { name: "General Site" })).not.toBeChecked();
    expect(within(offered).getByRole("checkbox", { name: "Adult Site" })).toBeChecked();
    expect(within(offered).queryByText("Movies Only")).toBeNull();
    const unsupported = screen.getByTestId("adult-indexers-unsupported");
    expect(within(unsupported).getByText("Cannot search adult content")).toBeInTheDocument();
    expect(within(unsupported).getByText("Movies Only")).toBeInTheDocument();
    expect(within(unsupported).queryByRole("checkbox", { name: "Movies Only" })).toBeNull();
    await userEvent.click(within(unsupported).getByRole("checkbox", { name: "Old Choice" }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]).toMatchObject({ path: "/indexers/4", body: { categories: { adult: [] } } });
  });
});
