import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { whyLine, type ArtistSuggestion, type ArtistSuggestionList } from "./discovery";

const choirId = "5a8e07d5-d932-4484-a7f7-e700793a9c96";
const trioId = "5a8e07d5-d932-4484-a7f7-e700793a9c95";

const suggestion = (over: Partial<ArtistSuggestion> = {}): ArtistSuggestion => ({
  mbid: choirId, name: "Creative Commons Choir", disambiguation: "vocal ensemble", score: 97,
  seeds: [{ mbid: "s1", name: "Open Source Artist", artistId: 1 }, { mbid: "s2", name: "Free Trio", artistId: 2 }], seedCount: 5,
  providers: ["lastfm", "listenbrainz"], tags: ["shoegaze", "dream pop"], cover: "rg-8c1e5a3e-0f4d-4a9c-8c43-0000000000c1.jpg",
  matchedByName: false, links: { musicbrainz: `https://musicbrainz.org/artist/${choirId}`, lastfm: "https://www.last.fm/music/Creative+Commons+Choir" },
  status: "none", ...over,
});

const listOf = (items: ArtistSuggestion[], over: Partial<ArtistSuggestionList> = {}): ArtistSuggestionList => ({
  items, total: items.length, tags: [{ name: "shoegaze", count: 1 }, { name: "jazz", count: 1 }],
  providers: [{ id: "lastfm", enabled: true, configured: true, artists: 3, failed: 0 },
    { id: "listenbrainz", enabled: true, configured: true, artists: 3, failed: 0 },
    { id: "musicbrainz", enabled: true, configured: true, artists: 3, failed: 0 },
    { id: "spotify", enabled: true, configured: true, artists: 0, failed: 3, relatedArtists: "restricted" }],
  refreshing: false, pending: 0, canAdd: true, canRequest: true, ...over,
});

const setup = { rootfolders: [{ id: 7, path: "/data/media/music", kind: "music", freeBytes: 1, totalBytes: 2, accessible: true, writable: true }],
  profiles: [{ id: 5, name: "Lossless", kind: "music", items: [], cutoff: "flac", upgradesAllowed: true }],
  meta: [{ id: 1, name: "Standard", primaryTypes: ["album"], secondaryTypes: ["studio"], releaseStatuses: ["official"], artists: 0 }] };

describe("music discovery", () => {
  it("explains a suggestion", () => {
    const t = (k: string, o?: Record<string, unknown>) => `${k}:${JSON.stringify(o)}`;
    expect(whyLine(t, suggestion())).toBe('discovery.similarToMore:{"names":"Open Source Artist, Free Trio","count":3}');
    expect(whyLine(t, suggestion({ seeds: [{ mbid: "s1", name: "A" }], seedCount: 1 }))).toBe('discovery.similarTo:{"a":"A"}');
  });

  it("shows suggestions with why, tags, listen links and attribution; filters; adds several; not interested", async () => {
    const queries: URLSearchParams[] = [];
    const added: unknown[] = [];
    const excluded: unknown[] = [];
    mockApi(({ method, path, body, query }) => {
      if (path === "/music/discover/artists") {
        queries.push(query);
        if (query.get("tag") === "jazz") return { status: 200, body: listOf([suggestion({ mbid: trioId, name: "Trio", tags: ["jazz"] })]) };
        return { status: 200, body: listOf([suggestion(), suggestion({ mbid: trioId, name: "Trio", tags: ["jazz"], matchedByName: true, seedCount: 1,
          seeds: [{ mbid: "s1", name: "Open Source Artist" }] })]) };
      }
      if (path === "/music/artists" && method === "GET") return { status: 200, body: { total: 0, items: [] } };
      if (path === "/rootfolders") return { status: 200, body: setup.rootfolders };
      if (path === "/qualityprofiles") return { status: 200, body: setup.profiles };
      if (path === "/music/metadataprofiles") return { status: 200, body: setup.meta };
      if (method === "POST" && path === "/music/artists") {
        added.push(body);
        return { status: 201, body: { id: 9 } };
      }
      if (method === "POST" && path === "/music/discover/exclusions") {
        excluded.push(body);
        return { status: 200, body: { mbid: choirId, name: "x", createdAt: new Date().toISOString() } };
      }
    });
    renderApp("/music/discover", { me: testUser() });
    const cards = await screen.findAllByTestId("artist-suggestion");
    expect(cards).toHaveLength(2);
    expect(cards[0]).toHaveTextContent("Similar to Open Source Artist, Free Trio and 3 more");
    expect(cards[0]).toHaveTextContent("Tags: shoegaze, dream pop");
    expect(cards[0]).toHaveTextContent("97%");
    expect(within(cards[0]!).getByRole("link", { name: /Creative Commons Choir at Last.fm/ })).toHaveAttribute("href",
      "https://www.last.fm/music/Creative+Commons+Choir");
    expect(cards[1]).toHaveTextContent("Matched by name");
    expect(screen.getByTestId("discovery-attribution")).toHaveTextContent("Data from Last.fm · ListenBrainz");
    expect(screen.getByText(/Spotify no longer gives related artists/)).toBeInTheDocument();

    await userEvent.selectOptions(screen.getByTestId("tag-filter"), "jazz");
    await waitFor(() => expect(screen.getAllByTestId("artist-suggestion")).toHaveLength(1));
    expect(queries.some((q) => q.get("tag") === "jazz")).toBe(true);
    await userEvent.selectOptions(screen.getByTestId("tag-filter"), "");
    await waitFor(() => expect(screen.getAllByTestId("artist-suggestion")).toHaveLength(2));

    // Bulk add with the add-artist defaults.
    await userEvent.click(screen.getByRole("checkbox", { name: "Select Creative Commons Choir" }));
    await userEvent.click(screen.getByRole("checkbox", { name: "Select Trio" }));
    await userEvent.click(screen.getByTestId("bulk-add"));
    const dialog = await screen.findByTestId("add-suggestions-dialog");
    await waitFor(() => expect(within(dialog).getByTestId("add-suggestions-confirm")).toBeEnabled());
    await userEvent.click(within(dialog).getByTestId("add-suggestions-confirm"));
    await waitFor(() => expect(added).toHaveLength(2));
    expect(added[0]).toMatchObject({ mbid: choirId, rootFolderId: 7, qualityProfileId: 5, metadataProfileId: 1, searchOnAdd: true, monitor: "all" });

    await userEvent.click(screen.getByTestId(`exclude-${choirId}`));
    await waitFor(() => expect(excluded).toEqual([{ mbid: choirId, name: "Creative Commons Choir" }]));
  });

  it("says when nothing is set up and when it is still looking", async () => {
    mockApi(({ path }) => {
      if (path === "/music/discover/artists") {
        return { status: 200, body: listOf([], { pending: 4, providers: [{ id: "musicbrainz", enabled: true, configured: true, artists: 0, failed: 0 }] }) };
      }
      if (path === "/music/artists") return { status: 200, body: { total: 0, items: [] } };
    });
    renderApp("/music/discover", { me: testUser() });
    expect(await screen.findByTestId("discovery-working")).toHaveTextContent("waiting: 4");
  });

  it("lets people who request ask for a suggested artist on Discover", async () => {
    const requested: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/discover") return { status: 200, body: { rows: [], requests: [], metadataReady: true, musicReady: true } };
      if (path === "/music/discover/artists") {
        return { status: 200, body: listOf([suggestion({ seeds: [{ mbid: "s1", name: "Open Source Artist" }] }),
          suggestion({ mbid: trioId, name: "Trio", status: "pending", requestId: 3 })], { canAdd: false }) };
      }
      if (method === "POST" && path === "/requests") {
        requested.push(body);
        return { status: 201, body: { id: 4 } };
      }
    });
    const fan = testUser({ role: "requester" });
    fan.permissions = { ...fan.permissions, manageLibrary: false, requestMusic: true };
    renderApp("/discover", { me: fan });
    const row = await screen.findByTestId("discover-row-artist_suggestions");
    expect(within(row).getByTestId(`status-${trioId}`)).toHaveTextContent("Requested");
    await userEvent.click(within(row).getByTestId(`request-${choirId}`));
    await waitFor(() => expect(requested).toEqual([{ kind: "artist", mbid: choirId }]));
  });

  it("stores discovery keys write-only and tests a provider", async () => {
    const puts: unknown[] = [];
    const settings = { lastfm: true, listenbrainz: true, musicbrainz: true, spotify: true, lastfmApiKey: { configured: false },
      spotifyClientId: { configured: false }, spotifyClientSecret: { configured: false }, refreshDays: 7,
      providers: listOf([]).providers };
    mockApi(({ method, path, body }) => {
      if (path === "/settings/metadata") return { status: 200, body: { tmdbApiKey: { configured: true }, language: "en-US", region: "US",
        tvdbApiKey: { configured: false }, tvdbPin: { configured: false }, seriesProvider: "tmdb" } };
      if (path === "/settings/musicdiscovery" && method === "GET") return { status: 200, body: settings };
      if (path === "/settings/musicdiscovery" && method === "PUT") {
        puts.push(body);
        return { status: 200, body: { ...settings, lastfmApiKey: { configured: true } } };
      }
      if (path === "/settings/musicdiscovery/test") return { status: 200, body: { ok: false, code: "metadata_key_invalid", message: "refused" } };
    });
    renderApp("/settings/metadata", { me: testUser() });
    await userEvent.type(await screen.findByTestId("lastfm-key"), "abc123");
    await userEvent.click(screen.getByTestId("save-discovery"));
    await waitFor(() => expect(puts).toEqual([{ lastfmApiKey: "abc123" }]));
    expect(screen.getByTestId("spotify-restricted")).toBeInTheDocument();
    await userEvent.click(screen.getByTestId("discovery-test-lastfm"));
    expect(await screen.findByText("The key was refused.")).toBeInTheDocument();
  });
});
