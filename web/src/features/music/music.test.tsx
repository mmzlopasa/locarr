import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { albumGroup, formatLength, type Album, type AlbumDetail, type Artist } from "./common";
import { releaseLabel } from "./AlbumDetailPage";

const now = new Date().toISOString();
const artist = (over: Partial<Artist> = {}): Artist => ({
  id: 1, itemRef: "artist:1", mbid: "5a8e07d5-d932-4484-a7f7-e700793a9c94", name: "Open Source Artist", sortName: "Open Source Artist",
  disambiguation: "electronic duo", type: "Group", country: "XW", aliases: ["OSA"], genres: ["electronic"], monitored: true, monitorNewAlbums: "all",
  qualityProfileId: 5, metadataProfileId: 1, path: "/data/media/music/Open Source Artist", tags: [], addedAt: now, updatedAt: now, albums: 3,
  monitoredAlbums: 3, albumsWithFiles: 1, files: 5, sizeBytes: 50_000_000, cover: "rg-8c1e5a3e-0f4d-4a9c-8c43-0000000000a1.jpg", ...over,
});
const album = (over: Partial<Album> = {}): Album => ({
  id: 10, itemRef: "album:10", artistId: 1, artistName: "Open Source Artist", mbid: "8c1e5a3e-0f4d-4a9c-8c43-0000000000a1", title: "Free Album",
  disambiguation: "", primaryType: "album", secondaryTypes: [], releaseDate: "2020-03-14", monitored: true, artistMonitored: true, releaseLocked: false,
  filtered: false, path: "", trackCount: 5, fileCount: 0, sizeBytes: 0, cutoffMet: false, available: true, libraryStatus: "missing",
  cover: "rg-8c1e5a3e-0f4d-4a9c-8c43-0000000000a1.jpg", ...over,
});
const release = { id: 3, mbid: "r1", title: "Free Album", status: "official", date: "2020-03-14", country: "XW", disambiguation: "", label: "Commons",
  catalogNumber: "CR-1", formats: ["Digital Media"], format: "Digital Media", trackCount: 5, mediaCount: 1, tracksLoaded: true };

describe("music", () => {
  it("lists artists as covers and switches to the table", async () => {
    mockApi(({ path }) => {
      if (path === "/music/artists") return { status: 200, body: { total: 1, items: [artist()] } };
    });
    renderApp("/music", { me: testUser() });
    expect(await screen.findByTestId("artist-card")).toHaveTextContent("Open Source Artist");
    expect(screen.getByTestId("artist-card")).toHaveTextContent("1 of 3 albums");
    await userEvent.click(screen.getByRole("button", { name: /Table/ }));
    expect(await screen.findByRole("table")).toHaveTextContent("electronic duo");
  });

  it("adds an artist from a MusicBrainz search that tells namesakes apart", async () => {
    const bodies: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/music/artists/lookup") {
        return { status: 200, body: [
          { mbid: "a1", name: "Open Source Artist", sortName: "", disambiguation: "electronic duo", type: "Group", country: "XW", score: 100 },
          { mbid: "a2", name: "Open Source Artist", sortName: "", disambiguation: "jazz trio from Zagreb", type: "Group", country: "HR", score: 90 }] };
      }
      if (path === "/rootfolders") return { status: 200, body: [{ id: 7, path: "/data/media/music", kind: "music", freeBytes: 1, totalBytes: 2, accessible: true, writable: true }] };
      if (path === "/qualityprofiles") return { status: 200, body: [{ id: 5, name: "Lossless", kind: "music", items: [], cutoff: "flac", upgradesAllowed: true }] };
      if (path === "/music/metadataprofiles") return { status: 200, body: [{ id: 1, name: "Standard", primaryTypes: ["album"], secondaryTypes: ["studio"], releaseStatuses: ["official"], artists: 0 }] };
      if (method === "POST" && path === "/music/artists") {
        bodies.push(body);
        return { status: 201, body: artist() };
      }
      if (path === "/music/artists/1") return { status: 200, body: artist() };
      if (path === "/music/artists/1/albums") return { status: 200, body: [album()] };
    });
    renderApp("/music/add", { me: testUser() });
    await userEvent.type(await screen.findByTestId("add-artist-term"), "Open Source Artist");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    const results = await screen.findAllByTestId("artist-result");
    expect(results).toHaveLength(2);
    expect(results[1]).toHaveTextContent("jazz trio from Zagreb");
    await userEvent.click(screen.getByTestId("pick-a1"));
    await userEvent.selectOptions(screen.getByLabelText("Monitor now"), "latest");
    await waitFor(() => expect(screen.getByTestId("add-artist-confirm")).toBeEnabled());
    await userEvent.click(screen.getByTestId("add-artist-confirm"));
    expect(await screen.findByTestId("artist-name")).toHaveTextContent("Open Source Artist");
    expect(bodies[0]).toMatchObject({ mbid: "a1", rootFolderId: 7, qualityProfileId: 5, metadataProfileId: 1, monitor: "latest", searchOnAdd: true });
    expect(screen.getByTestId("album-group-album")).toHaveTextContent("Free Album");
  });

  it("monitors an album from the artist page", async () => {
    const monitorCalls: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/music/artists/1") return { status: 200, body: artist() };
      if (path === "/music/artists/1/albums") return { status: 200, body: [album({ monitored: false, libraryStatus: "unmonitored" })] };
      if (method === "PUT" && path === "/music/albums/monitor") {
        monitorCalls.push(body);
        return { status: 200, body: { count: 1 } };
      }
    });
    renderApp("/music/artists/1", { me: testUser() });
    await userEvent.click(await screen.findByLabelText("Monitor Free Album"));
    await waitFor(() => expect(monitorCalls).toEqual([{ albumIds: [10], monitored: true }]));
  });

  it("shows an album's tracks with files, picks an edition and opens the history drawer", async () => {
    const detail: AlbumDetail = {
      album: album({ release, fileCount: 1, libraryStatus: "partial" }),
      releases: [release, { ...release, id: 4, mbid: "r2", country: "GB", format: "2×CD", trackCount: 8, mediaCount: 2, disambiguation: "deluxe edition" }],
      tracks: [
        { id: 1, mbid: "t1", recordingMbid: "rec1", disc: 1, position: 1, number: "1", title: "Opening", lengthMs: 167000, artist: "Open Source Artist",
          file: { id: 9, path: "/data/media/music/Open Source Artist/Free Album (2020)/01 - Opening.flac", sizeBytes: 1000, format: "flac", bitDepth: 24,
            sampleRate: 96000, bitrate: 0, durationMs: 167000, addedAt: now,
            quality: { id: "flac-24", name: "FLAC 24bit", source: "flac", resolution: 0, modifier: "", music: true, rank: 1 } } },
        { id: 2, mbid: "t2", recordingMbid: "rec2", disc: 1, position: 2, number: "2", title: "Second Light", lengthMs: 184000, artist: "Open Source Artist" },
      ],
      extraFiles: [],
    };
    const patches: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (method === "GET" && path === "/music/albums/10") return { status: 200, body: detail };
      if (method === "PATCH" && path === "/music/albums/10") {
        patches.push(body);
        return { status: 200, body: { ...detail, album: { ...detail.album, releaseLocked: true } } };
      }
      if (path === "/timeline") {
        return { status: 200, body: { item: "album:10", explanation: { code: "upgrade_wanted", tone: "info", message: "1 of 5 tracks", params: { searches: 1 }, reasons: [] },
          events: [{ id: 1, at: now, type: "release_matched", source: "timeline", message: "", data: { score: 96, releases: [{ release: "r1", title: "Free Album", format: "Digital Media", country: "XW", score: 96 }] } }] } };
      }
    });
    renderApp("/music/albums/10", { me: testUser() });
    const rows = await screen.findAllByTestId("track-row");
    expect(rows[0]).toHaveTextContent("01 - Opening.flac");
    expect(rows[0]).toHaveTextContent("FLAC 24bit");
    expect(rows[1]).toHaveTextContent("missing");
    await userEvent.selectOptions(screen.getByTestId("release-picker"), "4");
    await waitFor(() => expect(patches).toEqual([{ releaseId: 4 }]));
    await userEvent.click(screen.getByTestId("open-history"));
    const drawer = await screen.findByTestId("history-drawer");
    expect(await within(drawer).findByTestId("why-box")).toBeInTheDocument();
    expect(within(drawer).getByTestId("timeline-event")).toHaveTextContent("Matched the files to Free Album (Digital Media, XW): score 96");
  });

  it("creates a metadata profile", async () => {
    const created: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (method === "GET" && path === "/music/metadataprofiles") return { status: 200, body: [] };
      if (method === "POST" && path === "/music/metadataprofiles") {
        created.push(body);
        return { status: 201, body: { id: 2, artists: 0, ...(body as object) } };
      }
    });
    renderApp("/settings/metadataprofiles", { me: testUser() });
    await userEvent.click(await screen.findByTestId("new-metadata-profile"));
    await userEvent.type(screen.getByTestId("profile-name"), "Live too");
    await userEvent.click(screen.getByLabelText("Live"));
    await userEvent.click(screen.getByLabelText("EP"));
    await userEvent.click(screen.getByTestId("save-metadata-profile"));
    await waitFor(() => expect(created).toEqual([{ name: "Live too", primaryTypes: ["album", "ep"], secondaryTypes: ["studio", "live"], releaseStatuses: ["official"] }]));
  });

  it("groups albums, formats lengths and describes editions", () => {
    expect(albumGroup({ primaryType: "album", secondaryTypes: [] })).toBe("album");
    expect(albumGroup({ primaryType: "album", secondaryTypes: ["live"] })).toBe("live");
    expect(albumGroup({ primaryType: "single", secondaryTypes: [] })).toBe("single");
    expect(albumGroup({ primaryType: "", secondaryTypes: [] })).toBe("other");
    expect(formatLength(167_000)).toBe("2:47");
    expect(formatLength(0)).toBe("–");
    const t = (k: string, o?: Record<string, unknown>) => (k === "music.tracks" ? `tracks: ${String(o?.count)}` : k);
    expect(releaseLabel({ ...release, country: "GB", format: "2×CD", trackCount: 8, disambiguation: "deluxe edition" }, t))
      .toBe("2020-03-14 · GB · 2×CD · tracks: 8 · deluxe edition");
  });
});
