import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { DownloadClientImplementation } from "@/api/client";
import { mockApi, renderApp, testUser, type MockRequest } from "@/test-utils";

const caps = {
  categories: true, createCategories: true, directoryCategories: false, tags: true, seedLimits: true, sequential: true,
  firstLastPiece: true, priority: true, savePath: true, freeSpace: true, deletesData: true,
};
const impls: DownloadClientImplementation[] = [
  { name: "qbittorrent", displayName: "qBittorrent", protocol: "torrent", defaultPort: 8080, defaultUrlBase: "", defaultUsername: "admin",
    fields: ["host", "port", "useTls", "urlBase", "username", "password", "allowSelfSigned"], required: [], capabilities: caps },
  { name: "sabnzbd", displayName: "SABnzbd", protocol: "usenet", defaultPort: 8080, defaultUrlBase: "", defaultUsername: "",
    fields: ["host", "port", "useTls", "urlBase", "apiKey", "allowSelfSigned"], required: ["apiKey"],
    capabilities: { ...caps, sequential: false, firstLastPiece: false, tags: false, seedLimits: false } },
];

describe("downloaders page", () => {
  it("adds a client with only the fields it needs, translates a failed test and saves", async () => {
    const posted: MockRequest[] = [];
    let saved = false;
    mockApi((req) => {
      const { method, path } = req;
      if (method === "GET" && path === "/downloadclients/implementations") return { status: 200, body: impls };
      if (method === "GET" && path === "/downloadclients") {
        return { status: 200, body: saved ? [{
          id: 1, name: "SAB", implementation: "sabnzbd", protocol: "usenet", enabled: true, priority: 1, host: "sab.lan", port: 8080,
          useTls: false, allowSelfSigned: false, urlBase: "", username: "", password: { configured: false }, apiKey: { configured: true },
          directory: "", categories: { movies: "movies", series: "tv", music: "music" }, recentPriority: "normal", olderPriority: "normal",
          initialState: "start", removeCompleted: false, sequential: false, firstLastPiece: false, tags: [],
          status: { ok: true, checkedAt: new Date().toISOString(), version: "4.3.1", freeSpaceBytes: 1024 ** 4 },
          createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }] : [] };
      }
      if (method === "GET" && path === "/remotepathmappings") return { status: 200, body: [] };
      if (method === "GET" && path === "/downloadclients/settings") return { status: 200, body: { pollSeconds: 30, stalledHours: 24 } };
      if (method === "POST" && path === "/downloadclients/test") {
        posted.push(req);
        return { status: 200, body: { ok: false, errorCode: "auth_failed", message: "authentication failed (HTTP 403)", missingCategories: [], canCreateCategories: false } };
      }
      if (method === "POST" && path === "/downloadclients") {
        posted.push(req);
        saved = true;
        return { status: 201, body: {} };
      }
    });
    renderApp("/downloaders", { me: testUser() });
    expect(await screen.findByText(/No download clients yet/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Add client" }));
    await userEvent.click(screen.getByTestId("pick-sabnzbd"));
    const form = screen.getByTestId("client-form");
    // SABnzbd needs an API key, not a username or password.
    expect(within(form).queryByLabelText("Username")).toBeNull();
    expect(within(form).queryByLabelText("Password")).toBeNull();
    expect(within(form).queryByLabelText("Download in order")).toBeNull();
    expect(within(form).getByLabelText("Port")).toHaveValue(8080);
    await userEvent.clear(within(form).getByLabelText("Name"));
    await userEvent.type(within(form).getByLabelText("Name"), "SAB");
    await userEvent.type(within(form).getByLabelText("Host"), "sab.lan");
    await userEvent.type(within(form).getByLabelText("API key or RPC secret"), "sekrit-key");

    await userEvent.click(within(form).getByRole("button", { name: "Test" }));
    expect(await screen.findByTestId("test-failed")).toHaveTextContent("Sign-in refused");

    await userEvent.click(within(form).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(posted).toHaveLength(2));
    const body = posted[1]!.body as Record<string, unknown>;
    expect(body).toMatchObject({ name: "SAB", implementation: "sabnzbd", host: "sab.lan", port: 8080, apiKey: "sekrit-key",
      categories: { movies: "movies", series: "tv", music: "music" } });
    expect(body).not.toHaveProperty("username");
    expect(body).not.toHaveProperty("password");
    const row = await screen.findByTestId("client-SAB");
    expect(row).toHaveTextContent("Connected");
    expect(row).toHaveTextContent("version 4.3.1");
    expect(row).not.toHaveTextContent("sekrit-key");
  });
});
