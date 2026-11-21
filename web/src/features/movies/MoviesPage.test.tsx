import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Movie, Timeline } from "@/api/client";
import { mockApi, renderApp, testUser } from "@/test-utils";

const now = new Date().toISOString();
export const movie = (over: Partial<Movie> = {}): Movie => ({
  id: 1, itemRef: "movie:1", tmdbId: 10378, imdbId: "tt1254207", title: "Big Buck Bunny", originalTitle: "Big Buck Bunny", originalLanguage: "en",
  alternateTitles: [], year: 2008, overview: "A rabbit.", runtime: 10, status: "Released", studio: "Blender", genres: ["Animation"],
  certification: "", rating: 7, votes: 10, inCinemas: "2008-05-30", digitalRelease: "2008-05-30", physicalRelease: "", collectionId: 9001,
  collectionName: "Open Movies", poster: "poster10378.png", backdrop: "", monitored: true, qualityProfileId: 2, path: "/data/media/movies/Big Buck Bunny (2008)",
  minimumAvailability: "released", tags: [], addedAt: now, updatedAt: now, available: true, cutoffMet: false, libraryStatus: "missing", ...over,
});

describe("movies", () => {
  it("lists the library as posters and filters by status", async () => {
    const seen: string[] = [];
    mockApi(({ method, path, query }) => {
      if (method === "GET" && path === "/movies") {
        seen.push(query.get("status") ?? "");
        const items = query.get("status") === "downloaded" ? [movie({ id: 2, title: "Sintel", libraryStatus: "downloaded" })] : [movie()];
        return { status: 200, body: { total: items.length, items } };
      }
      if (path === "/qualityprofiles") return { status: 200, body: [] };
    });
    renderApp("/movies", { me: testUser() });
    expect(await screen.findByText("Big Buck Bunny")).toBeInTheDocument();
    expect(screen.getAllByTestId("movie-card")).toHaveLength(1);
    await userEvent.selectOptions(screen.getByLabelText("Status"), "downloaded");
    expect(await screen.findByText("Sintel")).toBeInTheDocument();
    expect(seen).toContain("downloaded");
  });

  it("shows the why-isn't-it-here box above the timeline", async () => {
    const tl: Timeline = { item: "movie:1", title: "Big Buck Bunny",
      explanation: { code: "not_available_yet", tone: "info", message: "x", params: { searches: 0 }, reasons: [] },
      events: [{ id: 1, at: now, type: "added", source: "timeline", message: "", data: { path: "/data/media/movies/Big Buck Bunny (2008)" } }] };
    mockApi(({ path }) => {
      if (path === "/movies/1") return { status: 200, body: movie({ available: false, libraryStatus: "unavailable" }) };
      if (path === "/timeline") return { status: 200, body: tl };
      if (path === "/qualityprofiles") return { status: 200, body: [] };
    });
    renderApp("/movies/1", { me: testUser() });
    const box = await screen.findByTestId("why-box");
    expect(box).toHaveTextContent("Not available yet");
    expect(screen.getByTestId("timeline-event")).toHaveTextContent("Added to /data/media/movies/Big Buck Bunny (2008).");
  });

  it("adds a movie from a TMDb search", async () => {
    const bodies: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/movies/lookup") return { status: 200, body: [{ tmdbId: 10378, title: "Big Buck Bunny", originalTitle: "Big Buck Bunny", year: 2008,
        releaseDate: "2008-05-30", overview: "", poster: "", backdrop: "", popularity: 1, rating: 7 }] };
      if (path === "/rootfolders") return { status: 200, body: [{ id: 5, path: "/data/media/movies", kind: "movies", accessible: true, writable: true, createdAt: now }] };
      if (path === "/qualityprofiles") return { status: 200, body: [{ id: 2, name: "HD", kind: "movies", items: [], cutoff: "", upgradesAllowed: true,
        minFormatScore: 0, cutoffFormatScore: 0, formatScores: [], languages: [] }] };
      if (path === "/settings/mediamanagement") return { status: 403, body: { code: "forbidden", message: "" } };
      if (method === "POST" && path === "/movies") {
        bodies.push(body);
        return { status: 201, body: movie() };
      }
      if (path === "/movies/1") return { status: 200, body: movie() };
      if (path === "/timeline") return { status: 200, body: { item: "movie:1", explanation: { code: "not_searched_yet", tone: "info", message: "", params: {}, reasons: [] }, events: [] } };
    });
    const { router } = renderApp("/movies/add", { me: testUser() });
    await userEvent.type(await screen.findByTestId("add-movie-term"), "big buck");
    await userEvent.click(screen.getByRole("button", { name: /Search/ }));
    await userEvent.click(await screen.findByTestId("pick-10378"));
    await userEvent.click(await screen.findByTestId("add-movie-confirm"));
    await waitFor(() => expect(router.state.location.pathname).toBe("/movies/1"));
    expect(bodies[0]).toMatchObject({ tmdbId: 10378, rootFolderId: 5, qualityProfileId: 2, minimumAvailability: "released", searchOnAdd: true });
  });
});
