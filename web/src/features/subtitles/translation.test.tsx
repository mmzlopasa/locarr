import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import i18n from "@/i18n";
import type { Movie, Subtitle, SubtitleItem, SubtitleTranslator, TimelineEvent } from "@/api/client";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { describeSubtitleEvent } from "./common";

const now = new Date().toISOString();

const movie = (): Movie => ({
  id: 1, itemRef: "movie:1", tmdbId: 10378, imdbId: "tt1254207", title: "Big Buck Bunny", originalTitle: "Big Buck Bunny", originalLanguage: "en",
  alternateTitles: [], year: 2008, overview: "", runtime: 10, status: "Released", studio: "", genres: [], certification: "", rating: 7,
  votes: 10, inCinemas: "", digitalRelease: "", physicalRelease: "", collectionId: 0, collectionName: "", poster: "", backdrop: "", monitored: true, qualityProfileId: 2,
  path: "/m", minimumAvailability: "released", tags: [], addedAt: now, updatedAt: now, available: true, cutoffMet: true, libraryStatus: "downloaded",
});

const sub = (over: Partial<Subtitle> = {}): Subtitle => ({
  id: 7, mediaFileId: 1, itemRef: "movie:1", path: "/m/Big Buck Bunny (2008).en.srt", language: "en", forced: false, hearingImpaired: false, format: "srt",
  source: "disk", provider: "", providerId: "", release: "", encoding: "utf-8", syncState: "", syncMessage: "", syncOffsetMs: 0, syncScale: 1,
  translatedFrom: "", translationSource: "", addedAt: now, ...over,
});

const item = (over: Partial<SubtitleItem> = {}): SubtitleItem => ({
  ref: "movie:1", title: "Big Buck Bunny", filePath: "/m/Big Buck Bunny (2008).mkv", hasFile: true,
  profile: { id: 1, name: "Hrvatski", languages: [{ language: "hr", forced: false, hearingImpaired: "include" }], skipEmbedded: true, cutoff: false, translateAfterHours: 0 },
  profileSource: "default", profileOverride: 0,
  wants: [{ key: "hr", language: "hr", forced: false, hearingImpaired: "include", status: "missing" }],
  subtitles: [sub()], tracks: [], probeError: "", searches: 1, lastResult: "", providersAsked: 1, syncAvailable: false,
  explanation: { code: "not_found", tone: "warning", message: "x", params: { missing: ["hr"], searches: 1, providers: 1 } },
  translations: [], translationAvailable: true, ...over,
});

const translator = (over: Partial<SubtitleTranslator> = {}): SubtitleTranslator => ({
  name: "deepl", enabled: true, fields: ["apiKey", "formality", "glossaryId"], needsKey: true, needsUrl: false, local: false, unit: "characters",
  defaultModel: "", models: [], website: "https://www.deepl.com/your-account/keys", baseUrl: "", apiKey: { configured: true }, model: "", formality: "",
  glossaryId: "", monthlyCap: 500000, failures: 0, lastError: "", lastErrorCode: "",
  month: { month: "2026-11", characters: 12345, inputTokens: 0, outputTokens: 0, requests: 3, lines: 742, subtitles: 1 }, ...over,
});

describe("subtitle translation", () => {
  afterEach(async () => { await i18n.changeLanguage("en"); });

  it("translates a missing language, shows progress and the badge", async () => {
    let state = item();
    const posts: unknown[] = [];
    mockApi(({ method, path: raw, body }) => {
      const path = decodeURIComponent(raw);
      if (path === "/movies/1") return { status: 200, body: movie() };
      if (path === "/qualityprofiles") return { status: 200, body: [] };
      if (path === "/subtitles/items/movie:1") return { status: 200, body: state };
      if (method === "POST" && path === "/subtitles/items/movie:1/translate") {
        posts.push(body);
        state = item({ translations: [{ ref: "movie:1", key: "hr", translator: "deepl", state: "running", done: 40, total: 742, startedAt: now }] });
        return { status: 202, body: { count: 1 } };
      }
      if (path === "/subtitles/8/preview") return { status: 200, body: { total: 742, lines: [{ startMs: 1000, endMs: 2000, text: "[hr] Hello", original: "Hello" }] } };
    });
    renderApp("/movies/1", { me: testUser() });
    await userEvent.click(await screen.findByTestId("tab-subtitles"));
    await userEvent.click(await screen.findByTestId("subtitles-translate"));
    await waitFor(() => expect(posts).toEqual([{ keys: ["hr"] }]));
    expect(await screen.findByTestId("translation-progress")).toHaveTextContent("Translating Croatian with DeepL: 40 of 742 lines");

    state = item({ wants: [{ key: "hr", language: "hr", forced: false, hearingImpaired: "include", status: "translated", subtitleId: 8 }],
      subtitles: [sub(), sub({ id: 8, path: "/m/Big Buck Bunny (2008).hr.translated.srt", language: "hr", source: "translated", provider: "deepl",
        translatedFrom: "en", translationSource: "subtitle:Big Buck Bunny (2008).en.srt" })] });
    const row = await screen.findByText("translated from English with DeepL", {}, { timeout: 5000 });
    const li = row.closest("[data-testid=subtitle-row]") as HTMLElement;
    expect(within(li).getByTestId("translated-badge")).toHaveTextContent("Machine translated");
    expect(screen.getAllByTestId("subtitle-want")[0]).toHaveAttribute("data-status", "translated");
    await userEvent.click(within(li).getByTestId("subtitle-tool-preview"));
    expect(await within(li).findByTestId("preview-original")).toHaveTextContent("Hello");
  });

  it("configures a translation service write-only and tests it", async () => {
    const puts: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/subtitles/settings") return { status: 200, body: { defaultProfileMovies: 0, defaultProfileSeries: 0, minScoreMovies: 60,
        minScoreEpisodes: 60, preferredFormat: "", upgradeDays: 7, searchesPerHour: 30, autoSync: false, syncAvailable: true } };
      if (path === "/subtitles/profiles" || path === "/subtitles/providers") return { status: 200, body: [] };
      if (path === "/languages") return { status: 200, body: [{ code: "hr", name: "Croatian", native: "Hrvatski" }, { code: "en", name: "English", native: "English" }] };
      if (path === "/subtitles/translation") return { status: 200, body: { defaultTranslator: "", languageTranslators: {}, suffix: "translated",
        sourceLanguages: ["en"], autoSync: false, perHour: 10 } };
      if (path === "/subtitles/translators") return { status: 200, body: [translator(), translator({ name: "libretranslate", enabled: false, local: true,
        fields: ["baseUrl", "apiKey"], needsKey: false, needsUrl: true, apiKey: { configured: false }, monthlyCap: 0,
        month: { month: "2026-11", characters: 0, inputTokens: 0, outputTokens: 0, requests: 0, lines: 0, subtitles: 0 } })] };
      if (method === "PUT" && path === "/subtitles/translators/libretranslate") {
        puts.push(body);
        return { status: 200, body: translator({ name: "libretranslate" }) };
      }
      if (method === "POST" && path === "/subtitles/translators/deepl/test") return { status: 200, body: { input: "x", output: "Kamo ideš? <i>Čekaj me!</i>",
        from: "en", to: "hr", durationMs: 120, characters: 40, inputTokens: 0, outputTokens: 0, translator: translator() } };
    });
    renderApp("/settings/subtitles", { me: testUser() });
    const cards = await screen.findAllByTestId("subtitle-translator");
    expect(cards[0]).toHaveTextContent("12,345 characters");
    expect(cards[0]).toHaveTextContent("of 500,000");
    expect(cards[1]).toHaveTextContent("On your network");
    expect(within(cards[0]!).getByTestId("translator-key-deepl")).toHaveValue("");
    await userEvent.click(within(cards[0]!).getByTestId("translator-test-deepl"));
    expect(await within(cards[0]!).findByTestId("translator-test-result")).toHaveTextContent("Kamo ideš?");
    await userEvent.type(within(cards[1]!).getByTestId("translator-url-libretranslate"), "http://192.168.1.10:5000");
    await userEvent.click(within(cards[1]!).getByTestId("translator-enabled-libretranslate"));
    await userEvent.click(within(cards[1]!).getByTestId("translator-save-libretranslate"));
    await waitFor(() => expect(puts).toEqual([{ enabled: true, monthlyCap: 0, baseUrl: "http://192.168.1.10:5000" }]));
  });

  it("describes translation events in both languages", async () => {
    const ev: TimelineEvent = { id: 1, at: now, type: "subtitle_translated", source: "timeline", message: "",
      data: { key: "hr", from: "en", translator: "deepl", lines: 742, seconds: 31, path: "/m/Movie.hr.translated.srt" } };
    expect(describeSubtitleEvent(i18n.t.bind(i18n), ev)).toBe("Subtitle Croatian translated from English with DeepL: 742 lines in 31 s, saved as Movie.hr.translated.srt");
    await i18n.changeLanguage("hr");
    expect(describeSubtitleEvent(i18n.t.bind(i18n), ev)).toContain("preveden s jezika engleski pomoću DeepL: 742 redaka");
  });
});
