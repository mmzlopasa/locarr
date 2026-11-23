import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import i18n from "@/i18n";
import type { Movie, Subtitle, SubtitleItem, SubtitleProvider, SubtitleResult, TimelineEvent } from "@/api/client";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { cueTime, describeSubtitleEvent, wantLabel } from "./common";

const now = new Date().toISOString();

const movie = (over: Partial<Movie> = {}): Movie => ({
  id: 1, itemRef: "movie:1", tmdbId: 10378, imdbId: "tt1254207", title: "Big Buck Bunny", originalTitle: "Big Buck Bunny", originalLanguage: "en",
  alternateTitles: [], year: 2008, overview: "A rabbit.", runtime: 10, status: "Released", studio: "Blender", genres: [], certification: "", rating: 7,
  votes: 10, inCinemas: "", digitalRelease: "", physicalRelease: "", collectionId: 0, collectionName: "", poster: "", backdrop: "", monitored: true, qualityProfileId: 2,
  path: "/data/media/movies/Big Buck Bunny (2008)", minimumAvailability: "released", tags: [], addedAt: now, updatedAt: now, available: true,
  cutoffMet: true, libraryStatus: "downloaded", ...over,
});

const sub = (over: Partial<Subtitle> = {}): Subtitle => ({
  id: 5, mediaFileId: 1, itemRef: "movie:1", path: "/data/media/movies/BBB/Big Buck Bunny (2008).hr.srt", language: "hr", forced: false,
  hearingImpaired: false, format: "srt", source: "downloaded", provider: "opensubtitles", providerId: "9100001", release: "Big.Buck.Bunny.2008.1080p.BluRay.x264-LOCARR",
  score: 90, encoding: "windows-1250", syncState: "", syncMessage: "", syncOffsetMs: 0, syncScale: 1, addedAt: now, ...over,
});

const item = (over: Partial<SubtitleItem> = {}): SubtitleItem => ({
  ref: "movie:1", title: "Big Buck Bunny", filePath: "/data/media/movies/BBB/Big Buck Bunny (2008).mkv", hasFile: true,
  profile: { id: 1, name: "Hrvatski", languages: [{ language: "hr", forced: false, hearingImpaired: "include" }, { language: "en", forced: false, hearingImpaired: "include" }],
    skipEmbedded: true, cutoff: false },
  profileSource: "default", profileOverride: 0,
  wants: [{ key: "hr", language: "hr", forced: false, hearingImpaired: "include", status: "present", subtitleId: 5 },
    { key: "en", language: "en", forced: false, hearingImpaired: "include", status: "missing" }],
  subtitles: [sub()],
  tracks: [{ index: 0, kind: "audio", codec: "ac3", language: "en", name: "", default: true, forced: false, hearingImpaired: false, text: false },
    { index: 0, kind: "subtitle", codec: "srt", language: "en", name: "English", default: false, forced: false, hearingImpaired: false, text: true }],
  probeError: "", searches: 2, lastResult: "", providersAsked: 2, syncAvailable: false,
  explanation: { code: "not_found", tone: "warning", message: "x", params: { missing: ["en"], searches: 2, providers: 2 } },
  ...over,
});

const result = (over: Partial<SubtitleResult> = {}): SubtitleResult => ({
  key: "1:subdl:3197651:hr", provider: "subdl", id: "3197651", language: "hr", releases: ["Big.Buck.Bunny.2008.BDRip.XviD-OLD"], fileName: "x.zip",
  hearingImpaired: false, forced: false, hashMatch: false, downloads: 0, trusted: false, uploader: "", machineTranslated: false, score: 45,
  parts: [{ code: "ids", points: 20 }, { code: "year", points: 10 }, { code: "source", points: 15 }], want: "hr", downloaded: false, blacklisted: false, minScore: 60, ...over,
});

describe("subtitles", () => {
  it("describes keys, times and timeline events in both languages", async () => {
    const t = i18n.t.bind(i18n);
    expect(wantLabel(t, "hr:forced")).toBe("Croatian (forced)");
    expect(cueTime(3_723_004)).toBe("1:02:03.004");
    const ev: TimelineEvent = { id: 1, at: now, type: "subtitle_downloaded", source: "timeline", message: "",
      data: { key: "hr", provider: "opensubtitles", score: 92, path: "/m/Movie.hr.srt" } };
    expect(describeSubtitleEvent(t, ev)).toBe("Subtitle Croatian found on opensubtitles, score 92, saved as Movie.hr.srt");
    const nf: TimelineEvent = { ...ev, type: "subtitle_not_found", data: { key: "hr", providers: 4 } };
    expect(describeSubtitleEvent(t, nf)).toBe("No Croatian subtitle yet — searched 4 providers");
    await i18n.changeLanguage("hr");
    expect(describeSubtitleEvent(i18n.t.bind(i18n), nf)).toBe("Još nema titla hrvatski — pretraženo providera: 4");
  });

  it("shows the subtitles of a movie, picks a manual result and shifts a file", async () => {
    const calls: { method: string; path: string; body: unknown }[] = [];
    mockApi(({ method, path: raw, body }) => {
      const path = decodeURIComponent(raw);
      calls.push({ method, path, body });
      if (path === "/movies/1") return { status: 200, body: movie({ libraryStatus: "downloaded" }) };
      if (path === "/qualityprofiles") return { status: 200, body: [] };
      if (path === "/subtitles/items/movie:1") return { status: 200, body: item() };
      if (path === "/subtitles/items/movie:1/results") return { status: 200, body: { results: [result(), result({ key: "0:opensubtitles:9100001:hr",
        provider: "opensubtitles", id: "9100001", score: 90, downloaded: true, hashMatch: false })], providers: [{ provider: "opensubtitles", results: 4 },
        { provider: "subdl", results: 1 }] } };
      if (path === "/subtitles/items/movie:1/download") return { status: 200, body: sub({ provider: "subdl", providerId: "3197651", score: 45 }) };
      if (path === "/subtitles/5/shift") return { status: 200, body: sub() };
      if (path === "/subtitles/5/preview") return { status: 200, body: { total: 6, lines: [{ startMs: 3500, endMs: 6500, text: "Dobro jutro, šumo!" }] } };
    });
    renderApp("/movies/1", { me: testUser() });
    await userEvent.click(await screen.findByTestId("tab-subtitles"));
    const section = await screen.findByTestId("subtitles-section");
    expect(within(section).getByTestId("subtitle-why")).toHaveTextContent("No English subtitle yet — searched 2 times");
    const wants = screen.getAllByTestId("subtitle-want");
    expect(wants[0]).toHaveAttribute("data-status", "present");
    expect(wants[1]).toHaveTextContent("Missing");
    expect(screen.getByTestId("subtitle-row")).toHaveTextContent("from opensubtitles, score 90");
    expect(screen.getAllByTestId("media-track")).toHaveLength(2);

    await userEvent.click(screen.getByTestId("subtitles-manual"));
    const rows = await screen.findAllByTestId("subtitle-result");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent("Big.Buck.Bunny.2008.BDRip.XviD-OLD");
    expect(within(rows[0]!).getByTestId("result-score")).toHaveTextContent("45");
    expect(rows[1]).toHaveTextContent("Downloaded before");
    expect(screen.getByTestId("provider-reports")).toHaveTextContent("opensubtitles: 4 results");
    await userEvent.click(within(rows[0]!).getByTestId("result-download"));
    await waitFor(() => expect(calls.some((c) => c.path === "/subtitles/items/movie:1/download" && (c.body as { key: string }).key === "1:subdl:3197651:hr")).toBe(true));

    await userEvent.click(screen.getByTestId("subtitle-tool-shift"));
    await userEvent.clear(screen.getByTestId("shift-ms"));
    await userEvent.type(screen.getByTestId("shift-ms"), "1500");
    await userEvent.click(screen.getByTestId("shift-later"));
    await waitFor(() => expect(calls.some((c) => c.path === "/subtitles/5/shift" && (c.body as { offsetMs: number }).offsetMs === 1500)).toBe(true));
    await userEvent.click(screen.getByTestId("subtitle-tool-preview"));
    expect(await screen.findByTestId("subtitle-preview")).toHaveTextContent("00:03.500");

    // Without ffmpeg the sync tool says so and offers the other subtitle only.
    await userEvent.click(screen.getByTestId("subtitle-tool-sync"));
    expect(screen.getByTestId("sync-tool")).toHaveTextContent("ffmpeg is missing");
  });

  it("saves provider credentials write-only and shows health and quota", async () => {
    const provider = (over: Partial<SubtitleProvider> = {}): SubtitleProvider => ({
      name: "opensubtitles", enabled: true, priority: 10, username: "locarr", apiKey: { configured: true }, password: { configured: true },
      fields: ["apiKey", "username", "password"], languages: [], needsKey: true, website: "https://www.opensubtitles.com/consumers", failures: 0,
      lastError: "You have downloaded your allowed 5 subtitles", lastErrorCode: "quota", account: "locarr (Sub leecher)", downloadsToday: 5,
      quotaRemaining: 0, quotaLimit: 5, quotaResetAt: new Date(Date.now() + 3_600_000).toISOString(), ...over,
    });
    const puts: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/subtitles/settings") return { status: 200, body: { defaultProfileMovies: 0, defaultProfileSeries: 0, minScoreMovies: 60,
        minScoreEpisodes: 60, preferredFormat: "", upgradeDays: 7, searchesPerHour: 30, autoSync: false, syncAvailable: true } };
      if (path === "/subtitles/profiles") return { status: 200, body: [] };
      if (path === "/languages") return { status: 200, body: [{ code: "hr", name: "Croatian", native: "Hrvatski" }, { code: "en", name: "English", native: "English" }] };
      if (path === "/subtitles/providers") return { status: 200, body: [provider(), provider({ name: "subdl", enabled: false, fields: ["apiKey"], apiKey: { configured: false },
        password: { configured: false }, lastError: "", lastErrorCode: "", quotaRemaining: undefined, quotaResetAt: undefined, account: "", username: "" })] };
      if (method === "PUT" && path === "/subtitles/providers/subdl") {
        puts.push(body);
        return { status: 200, body: provider({ name: "subdl" }) };
      }
    });
    renderApp("/settings/subtitles", { me: testUser() });
    const cards = await screen.findAllByTestId("subtitle-provider");
    expect(cards[0]).toHaveTextContent("Quota used up");
    expect(cards[0]).toHaveTextContent("0 of 5");
    expect(cards[0]).toHaveTextContent("The download quota is used up.");
    expect(within(cards[0]!).getByTestId("provider-key-opensubtitles")).toHaveValue("");
    await userEvent.type(within(cards[1]!).getByTestId("provider-key-subdl"), "secret-key");
    await userEvent.click(within(cards[1]!).getByTestId("provider-enabled-subdl"));
    await userEvent.click(within(cards[1]!).getByTestId("provider-save-subdl"));
    await waitFor(() => expect(puts).toEqual([{ enabled: true, priority: 10, apiKey: "secret-key" }]));
  });

  it("lists missing subtitles and searches the selected", async () => {
    const searched: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/subtitles/wanted") return { status: 200, body: { total: 1, items: [{ ref: "episode:7", kind: "episode", itemId: 3, title: "Elephants Dream",
        season: 1, episode: 2, episodeTitle: "Emo", missing: ["hr", "en:hi"], searches: 0, lastResult: "" }] } };
      if (path === "/subtitles/profiles") return { status: 200, body: [] };
      if (method === "POST" && path === "/subtitles/wanted/search") {
        searched.push(body);
        return { status: 200, body: { count: 1 } };
      }
    });
    renderApp("/wanted", { me: testUser() });
    await userEvent.click(await screen.findByTestId("wanted-subtitles"));
    const row = await screen.findByTestId("wanted-subtitle");
    expect(row).toHaveTextContent("S01E02");
    expect(row).toHaveTextContent("Croatian, English (hearing impaired)");
    await userEvent.click(within(row).getByRole("checkbox"));
    await userEvent.click(screen.getByTestId("subtitles-search-selected"));
    await waitFor(() => expect(searched).toEqual([{ refs: ["episode:7"] }]));
  });
});
