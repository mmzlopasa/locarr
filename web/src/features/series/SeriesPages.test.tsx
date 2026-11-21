import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Episode, Series, SeriesDetail } from "@/api/client";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { episodeState } from "./common";

const now = new Date().toISOString();
const stats = { seasonCount: 1, episodeCount: 3, episodeFileCount: 1, monitoredEpisodeCount: 3, missingCount: 2, sizeOnDisk: 1 << 30 };

export const series = (over: Partial<Series> = {}): Series => ({
  id: 7, itemRef: "series:7", tmdbId: 800434, tvdbId: 434343, imdbId: "", tvmazeId: 0, metadataSource: "tmdb", title: "Cosmos Laundromat",
  originalTitle: "", originalLanguage: "en", alternateTitles: [], year: 2025, overview: "A sheep.", status: "ended", network: "Blender",
  timeZone: "Europe/Amsterdam", airTime: "", runtime: 24, genres: [], certification: "", rating: 7, votes: 1, firstAired: "2025-01-01",
  lastAired: "2025-01-15", seriesType: "standard", seasonFolder: true, monitorNewSeasons: "all", poster: "", backdrop: "", monitored: true,
  qualityProfileId: 3, path: "/data/media/tv/Cosmos Laundromat (2025)", tags: [], addedAt: now, updatedAt: now, stats, downloading: 0, ...over,
});

export const episode = (n: number, over: Partial<Episode> = {}): Episode => ({
  id: 100 + n, itemRef: `episode:${100 + n}`, seriesId: 7, seasonId: 1, seasonNumber: 1, episodeNumber: n, absoluteNumber: n,
  title: `Cycle ${n}`, overview: "", airDate: `2025-01-${String(n % 28 + 1).padStart(2, "0")}`,
  airDateUtc: `2025-01-${String(n % 28 + 1).padStart(2, "0")}T19:00:00Z`, runtime: 24, monitored: true, aired: true,
  removed: false, wanted: "missing", cutoffMet: false, ...over,
});

const detail = (): SeriesDetail => ({ ...series(), seasons: [
  { id: 1, itemRef: "season:1", number: 1, monitored: true, title: "Season 1", overview: "", poster: "", airDate: "2025-01-01", stats },
  { id: 2, itemRef: "season:2", number: 0, monitored: false, title: "Specials", overview: "", poster: "", airDate: "", stats: { ...stats, missingCount: 0 } },
] });

describe("series", () => {
  it("lists the library and filters continuing/ended and missing episodes", async () => {
    const seen: string[] = [];
    mockApi(({ path, query }) => {
      if (path === "/series") {
        seen.push(`${query.get("status") ?? ""}|${query.get("missing") ?? ""}`);
        return { status: 200, body: { total: 1, items: [series()] } };
      }
      if (path === "/qualityprofiles") return { status: 200, body: [] };
    });
    renderApp("/series", { me: testUser() });
    const card = await screen.findByTestId("series-card");
    expect(card).toHaveTextContent("Cosmos Laundromat");
    expect(card).toHaveTextContent("1/3");
    expect(card).toHaveTextContent("2 missing");
    await userEvent.selectOptions(screen.getByLabelText("Series status"), "ended");
    await userEvent.selectOptions(screen.getByLabelText("Episodes"), "missing");
    await waitFor(() => expect(seen).toContain("ended|true"));
  });

  it("shows seasons with episode rows, monitors in bulk and opens the episode timeline", async () => {
    const calls: { path: string; body: unknown }[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/series/7") return { status: 200, body: detail() };
      if (path === "/series/7/episodes") return { status: 200, body: [
        episode(1, { file: { id: 9, path: "/x/E01.mkv", sizeBytes: 1 << 30, quality: { id: "webdl-720p", name: "WEBDL-720p", source: "webdl", resolution: 720, modifier: "", kind: "video" } as never,
          revision: 1, customFormats: [], score: 0, releaseGroup: "", languages: [], mediaInfo: { source: "name" }, sceneName: "", addedAt: now }, wanted: "upgrade" }),
        episode(2), episode(3, { aired: false, airDateUtc: "2099-01-01T00:00:00Z", wanted: "none" }),
        { ...episode(1, { seasonId: 2, seasonNumber: 0, monitored: false, wanted: "none" }), id: 150, itemRef: "episode:150" },
      ] };
      if (path === "/qualityprofiles") return { status: 200, body: [] };
      if (method === "POST") {
        calls.push({ path, body });
        return { status: 200, body: { count: 1 } };
      }
      if (path === "/timeline") return { status: 200, body: { item: "episode:102", explanation: { code: "nothing_found", tone: "warning", message: "",
        params: { searches: 2 }, reasons: [] }, events: [] } };
    });
    renderApp("/series/7", { me: testUser() });
    expect(await screen.findByTestId("series-title")).toHaveTextContent("Cosmos Laundromat");
    const season = await screen.findByTestId("season-1");
    const rows = await within(season).findAllByTestId("episode-row");
    expect(rows).toHaveLength(3);
    expect(rows.map((r) => r.dataset.code)).toEqual(["S01E03", "S01E02", "S01E01"]);
    expect(rows[0]).toHaveTextContent("Not aired");
    expect(rows[1]).toHaveTextContent("Missing");
    expect(rows[2]).toHaveTextContent("Below cutoff");
    // Specials are collapsed by default.
    expect(within(screen.getByTestId("season-0")).queryAllByTestId("episode-row")).toHaveLength(0);
    await userEvent.click(within(rows[1]!).getByRole("checkbox"));
    await userEvent.click(within(rows[0]!).getByRole("checkbox"));
    const bar = await screen.findByTestId("bulk-bar");
    await userEvent.click(within(bar).getByRole("button", { name: /Stop monitoring/ }));
    await waitFor(() => expect(calls).toContainEqual({ path: "/episodes/monitor", body: { episodeIds: [102, 103], monitored: false } }));
    await userEvent.click(within(rows[1]!).getByTestId("episode-history"));
    const drawer = await screen.findByTestId("drawer-history");
    expect(await within(drawer).findByTestId("why-box")).toBeInTheDocument();
  });

  it("adds a series with a monitor option and series type", async () => {
    const bodies: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/series/lookup") return { status: 200, body: [{ tmdbId: 800434, tvdbId: 434343, source: "tmdb", title: "Cosmos Laundromat",
        originalTitle: "Cosmos Laundromat", year: 2025, firstAired: "", overview: "", poster: "", backdrop: "", network: "Blender", status: "ended", rating: 7 }] };
      if (path === "/rootfolders") return { status: 200, body: [{ id: 6, path: "/data/media/tv", kind: "series", accessible: true, writable: true, createdAt: now }] };
      if (path === "/qualityprofiles") return { status: 200, body: [{ id: 3, name: "HD", kind: "series", items: [], cutoff: "", upgradesAllowed: true,
        minFormatScore: 0, cutoffFormatScore: 0, formatScores: [], languages: [] }] };
      if (path === "/settings/mediamanagement/series") return { status: 403, body: { code: "forbidden", message: "" } };
      if (method === "POST" && path === "/series") {
        bodies.push(body);
        return { status: 201, body: series() };
      }
      if (path === "/series/7") return { status: 200, body: detail() };
      if (path === "/series/7/episodes") return { status: 200, body: [] };
    });
    const { router } = renderApp("/series/add", { me: testUser() });
    await userEvent.type(await screen.findByTestId("add-series-term"), "cosmos");
    await userEvent.click(screen.getByRole("button", { name: /Search/ }));
    await userEvent.click(await screen.findByTestId("pick-800434"));
    await userEvent.selectOptions(await screen.findByTestId("monitor-option"), "future");
    await userEvent.selectOptions(screen.getByLabelText("Series type"), "anime");
    await userEvent.click(screen.getByTestId("add-series-confirm"));
    await waitFor(() => expect(router.state.location.pathname).toBe("/series/7"));
    expect(bodies[0]).toMatchObject({ tmdbId: 800434, rootFolderId: 6, qualityProfileId: 3, monitor: "future", seriesType: "anime", seasonFolder: true });
  });

  it("derives one state per episode", () => {
    expect(episodeState(episode(1))).toBe("missing");
    expect(episodeState(episode(1, { monitored: false }))).toBe("unmonitored");
    expect(episodeState(episode(1, { airDate: "", aired: false }))).toBe("tba");
    expect(episodeState(episode(1, { download: { id: 1, title: "x", progress: 0.4, state: "downloading", status: "active" } }))).toBe("downloading");
  });

  it("renders a series with 30 seasons and 1000 episodes quickly", async () => {
    const seasons = Array.from({ length: 30 }, (_, i) => ({ id: i + 1, itemRef: `season:${i + 1}`, number: i + 1, monitored: true, title: "",
      overview: "", poster: "", airDate: "", stats }));
    const eps: Episode[] = [];
    for (let s = 1; s <= 30; s++) {
      for (let n = 1; n <= 34 && eps.length < 1000; n++) {
        eps.push(episode(n, { id: s * 1000 + n, itemRef: `episode:${s * 1000 + n}`, seasonId: s, seasonNumber: s }));
      }
    }
    mockApi(({ path }) => {
      if (path === "/series/7") return { status: 200, body: { ...series(), seasons } };
      if (path === "/series/7/episodes") return { status: 200, body: eps };
      if (path === "/qualityprofiles") return { status: 200, body: [] };
    });
    const started = performance.now();
    renderApp("/series/7", { me: testUser() });
    await screen.findByTestId("season-30");
    await waitFor(() => expect(screen.getAllByTestId("episode-row").length).toBeGreaterThan(0));
    const took = performance.now() - started;
    expect(screen.getAllByTestId(/^season-\d+$/)).toHaveLength(30);
    // Only the open (latest) season draws its rows.
    expect(screen.getAllByTestId("episode-row").length).toBeLessThanOrEqual(34);
    expect(took).toBeLessThan(3000);
  });
});
