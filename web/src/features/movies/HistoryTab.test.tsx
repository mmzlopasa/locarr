import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Movie, Timeline } from "@/api/client";
import { mockApi, renderApp, testUser, type MockRequest } from "@/test-utils";

const now = new Date().toISOString();
const movie = (over: Partial<Movie> = {}): Movie => ({
  id: 1, itemRef: "movie:1", tmdbId: 10378, imdbId: "tt1254207", title: "Big Buck Bunny", originalTitle: "Big Buck Bunny", originalLanguage: "en",
  alternateTitles: [], year: 2008, overview: "A rabbit.", runtime: 10, status: "Released", studio: "Blender", genres: ["Animation"],
  certification: "", rating: 7, votes: 10, inCinemas: "2008-05-30", digitalRelease: "2008-05-30", physicalRelease: "", collectionId: 9001,
  collectionName: "Open Movies", poster: "poster10378.png", backdrop: "", monitored: true, qualityProfileId: 2, path: "/data/media/movies/Big Buck Bunny (2008)",
  minimumAvailability: "released", tags: [], addedAt: now, updatedAt: now, available: true, cutoffMet: false, libraryStatus: "missing", ...over,
});

const at = (s: number) => new Date(Date.now() - 3_600_000 + s * 1000).toISOString();

function timeline(): Timeline {
  const search = (id: number, s: number, query: string) => ({ id, at: at(s), type: "search", source: "search" as const, itemRef: "movie:1", message: query,
    data: { source: "automatic", query, resultCount: 1, finishedAt: at(s + 1), indexers: [{ name: "Tracker", status: "ok", count: 1 }] } });
  return {
    item: "movie:1", title: "Big Buck Bunny",
    explanation: { code: "all_rejected", tone: "warning", message: "", params: { searches: 1, candidates: 2 },
      reasons: [{ code: "quality_not_allowed", count: 2, example: "Unknown is not allowed in profile HD-1080p", params: { quality: "Unknown", profile: "HD-1080p" }, values: ["Unknown"] }] },
    events: [
      { id: 9, at: at(10), type: "decision", source: "timeline", itemRef: "movie:1", message: "",
        data: { reason: "scheduled", summary: { candidates: 2, accepted: 0, pending: 0, rejected: 2, topRejected: [] } } },
      search(5, 6, "Big Buck Bunny"),
      search(4, 2, "Big Buck Bunny 2008"),
      { id: 1, at: at(0), type: "added", source: "timeline", itemRef: "movie:1", message: "", data: { path: "/m" } },
    ],
  };
}

function api(calls: MockRequest[]) {
  mockApi((req) => {
    calls.push(req);
    if (req.path === "/movies/1") return { status: 200, body: movie() };
    if (req.path === "/timeline") return { status: 200, body: timeline() };
    if (req.path === "/qualityprofiles") return { status: 200, body: [] };
    if (req.path === "/history/clear") return { status: 200, body: { count: (req.body as { dryRun?: boolean }).dryRun ? 4 : 3, deleted: true } };
    if (req.method === "DELETE" && req.path.startsWith("/history/")) return { status: 204 };
  });
}

describe("item timeline", () => {
  it("shows one row per search cycle and names the quality and profile", async () => {
    api([]);
    renderApp("/movies/1?tab=history", { me: testUser() });
    const cycle = await screen.findByTestId("timeline-cycle");
    expect(cycle).toHaveAttribute("data-searches", "2");
    expect(cycle).toHaveTextContent("Automatic search — results: 2");
    expect(within(cycle).getByTestId("timeline-cycle-indexers")).toHaveTextContent("Tracker: 2");
    expect(within(cycle).getByTestId("timeline-cycle-decision")).toHaveTextContent("Decision (scheduled)");
    expect(screen.getAllByTestId("timeline-event")).toHaveLength(1);
    expect(screen.getByTestId("why-box")).toHaveTextContent("Most common reason: Unknown is not allowed in profile HD-1080p.");
    expect(screen.getByTestId("why-reason-quality_not_allowed")).toHaveTextContent("Unknown not allowed in HD-1080p ×2");
  });

  it("deletes a group and a single row through the history API", async () => {
    const calls: MockRequest[] = [];
    api(calls);
    renderApp("/movies/1?tab=history", { me: testUser() });
    await screen.findByTestId("timeline-cycle");
    await userEvent.click(screen.getByTestId("timeline-delete-t:9"));
    await userEvent.click(screen.getByTestId("timeline-delete-t:9-confirm"));
    await waitFor(() => expect(calls.some((c) => c.path === "/history/clear")).toBe(true));
    expect(calls.find((c) => c.path === "/history/clear")?.body).toEqual({ item: "movie:1", ids: ["t:9", "s:5", "s:4"], dryRun: false });
    await userEvent.click(screen.getByTestId("timeline-delete-t:1"));
    await userEvent.click(screen.getByTestId("timeline-delete-t:1-confirm"));
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE")).toBe(true));
    const del = calls.find((c) => c.method === "DELETE")!;
    expect(decodeURIComponent(del.path)).toBe("/history/t:1");
    expect(del.query.get("item")).toBe("movie:1");
  });

  it("clears the item's history after showing how many rows go and what stays", async () => {
    const calls: MockRequest[] = [];
    api(calls);
    renderApp("/movies/1?tab=history", { me: testUser() });
    await userEvent.click(await screen.findByTestId("timeline-clear"));
    const dialog = await screen.findByTestId("timeline-clear-dialog");
    await waitFor(() => expect(within(dialog).getByTestId("timeline-clear-count")).toHaveTextContent("Rows to delete: 4"));
    expect(dialog).toHaveTextContent("Kept: the files, the blocklist");
    await userEvent.click(within(dialog).getByTestId("timeline-clear-confirm"));
    await waitFor(() => expect(calls.some((c) => c.path === "/history/clear" && !(c.body as { dryRun?: boolean }).dryRun)).toBe(true));
    expect(calls.filter((c) => c.path === "/history/clear").map((c) => c.body)).toEqual([{ item: "movie:1", dryRun: true }, { item: "movie:1", dryRun: false }]);
  });

  it("offers no deleting without the library permission", async () => {
    api([]);
    const me = testUser({ role: "manager" });
    me.permissions.manageLibrary = false;
    renderApp("/movies/1?tab=history", { me });
    await screen.findByTestId("timeline-cycle").catch(() => undefined);
    expect(screen.queryByTestId("timeline-clear")).toBeNull();
    expect(screen.queryByTestId("timeline-delete-t:9")).toBeNull();
  });
});
