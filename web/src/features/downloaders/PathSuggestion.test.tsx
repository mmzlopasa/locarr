import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { PathMappingSuggestion, Queue, QueueItem } from "@/api/client";
import { mockApi, renderApp, testUser, type MockRequest } from "@/test-utils";

const now = new Date().toISOString();
const suggestion: PathMappingSuggestion = {
  clientId: 1, clientName: "qBit", host: "10.1.5.16", remotePath: "/downloads", localPath: "/data/downloads",
  clientPath: "/downloads/Some.Movie.2024.1080p", foundAt: "/data/downloads/Some.Movie.2024.1080p", source: "download", downloadIds: [1],
};
const item: QueueItem = {
  id: 1, unknown: false, clientId: 1, clientName: "qBit", clientItemId: "abc", protocol: "torrent", title: "Some.Movie.2024.1080p",
  status: "active", state: "seeding", rawState: "uploading", progress: 1, sizeBytes: 1000, downloadedBytes: 1000, downloadSpeed: 0,
  uploadSpeed: 0, ratio: 0, seedingSeconds: 0, category: "movies",
  message: "finished, but /downloads/Some.Movie.2024.1080p is not visible to Locarr", addedAt: now, updatedAt: now, pathSuggestion: suggestion,
};
const queue: Queue = { items: [item], clients: [{ id: 1, name: "qBit", protocol: "torrent", enabled: true, status: { ok: true, checkedAt: now } }] };

describe("path mapping suggestions", () => {
  it("offers the found mapping in the queue and adds it in one click", async () => {
    const posted: MockRequest[] = [];
    mockApi((req) => {
      if (req.method === "GET" && req.path === "/queue") return { status: 200, body: queue };
      if (req.method === "POST" && req.path === "/remotepathmappings") {
        posted.push(req);
        return { status: 201, body: { id: 5, host: "10.1.5.16", remotePath: "/downloads", localPath: "/data/downloads" } };
      }
      if (req.method === "POST" && req.path === "/queue/refresh") {
        posted.push(req);
        return { status: 204 };
      }
    });
    renderApp("/activity", { me: testUser() });
    const box = await screen.findByTestId("path-suggestion-1");
    expect(box).toHaveTextContent(
      "Locarr found this download at /data/downloads/Some.Movie.2024.1080p. Add mapping /downloads → /data/downloads for 10.1.5.16?");
    await userEvent.click(within(box).getByRole("button", { name: "Add mapping" }));
    await waitFor(() => expect(posted.map((r) => r.path)).toEqual(["/remotepathmappings", "/queue/refresh"]));
    expect(posted[0]!.body).toEqual({ host: "10.1.5.16", remotePath: "/downloads", localPath: "/data/downloads" });
    expect(await within(box).findByTestId("add-suggested-mapping-done")).toBeInTheDocument();
  });

  it("shows no button to a library manager who is not an administrator", async () => {
    mockApi((req) => {
      if (req.method === "GET" && req.path === "/queue") return { status: 200, body: queue };
    });
    renderApp("/activity", { me: testUser({ role: "manager" }) });
    const box = await screen.findByTestId("path-suggestion-1");
    expect(within(box).queryByRole("button")).toBeNull();
    expect(box).toHaveTextContent("An administrator can add it");
  });

  it("lists suggestions above the mappings, explains when mappings are needed, and warns about client folders", async () => {
    const puts: MockRequest[] = [];
    mockApi((req) => {
      const { method, path } = req;
      if (method === "GET" && path === "/downloadclients/implementations") return { status: 200, body: [] };
      if (method === "GET" && path === "/downloadclients") {
        return { status: 200, body: [{
          id: 1, name: "qBit", implementation: "qbittorrent", protocol: "torrent", enabled: true, priority: 1, host: "10.1.5.16", port: 8080,
          useTls: false, allowSelfSigned: false, urlBase: "", username: "admin", password: { configured: true }, apiKey: { configured: false },
          directory: "", categories: { movies: "movies", series: "", music: "" }, recentPriority: "normal", olderPriority: "normal",
          initialState: "start", removeCompleted: false, sequential: false, firstLastPiece: false, tags: [],
          status: { ok: true, checkedAt: now, pathChecks: [
            { clientPath: "/downloads", visible: false, suggestion: { ...suggestion, source: "folder", foundAt: "/data/downloads" } },
            { category: "movies", clientPath: "/data/movies", visible: true, localPath: "/data/movies" },
          ] },
          createdAt: now, updatedAt: now }] };
      }
      if (method === "GET" && path === "/remotepathmappings") return { status: 200, body: [] };
      if (method === "GET" && path === "/remotepathmappings/suggestions") return { status: 200, body: [{ ...suggestion, replacesMappingId: 3 }] };
      if (method === "GET" && path === "/downloadclients/settings") return { status: 200, body: { pollSeconds: 30, stalledHours: 24 } };
      if (method === "PUT" && path === "/remotepathmappings/3") {
        puts.push(req);
        return { status: 200, body: { id: 3, host: "10.1.5.16", remotePath: "/downloads", localPath: "/data/downloads" } };
      }
      if (method === "POST" && path === "/queue/refresh") return { status: 204 };
    });
    renderApp("/downloaders", { me: testUser() });
    expect(await screen.findByText(/sees the same files under a different path/)).toBeInTheDocument();
    expect(screen.queryByText(/none are needed/)).toBeNull();
    const checks = await screen.findByTestId("path-checks");
    expect(checks).toHaveTextContent("The client saves to /downloads, which Locarr cannot see.");
    expect(checks).toHaveTextContent("Likely mapping: /downloads → /data/downloads for 10.1.5.16.");
    expect(checks).not.toHaveTextContent("/data/movies");
    const sug = await screen.findByTestId("mapping-suggestion");
    await userEvent.click(within(sug).getByRole("button", { name: "Replace mapping" }));
    await waitFor(() => expect(puts).toHaveLength(1));
  });
});
