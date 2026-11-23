import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { emptyNotifications, item, mediaRequest, requester, summary, titleDetail } from "./fixtures";
import { buildRequest } from "./RequestDialog";

describe("discover", () => {
  it("shows rows of posters with their library and request state", async () => {
    mockApi(({ path }) => {
      if (path === "/discover") return { status: 200, body: { metadataReady: true, musicReady: true, requests: [mediaRequest({ status: "processing" })], rows: [
        { key: "trending", items: [item({ status: "available" }), item({ tmdbId: 45745, title: "Sintel", status: "none", status4k: "pending" })] },
        { key: "popular_series", items: [item({ mediaType: "series", tmdbId: 800434, title: "Cosmos Laundromat", status: "partial" })] },
      ] } };
      if (path === "/discover/genres") return { status: 200, body: { movieGenres: [{ id: 16, name: "Animation" }], seriesGenres: [], studios: [], networks: [] } };
      if (path === "/notifications") return emptyNotifications;
    });
    renderApp("/discover", { me: requester() });
    const trending = await screen.findByTestId("discover-row-trending");
    expect(within(trending).getByText("Trending")).toBeInTheDocument();
    const tears = within(trending).getByTestId("poster-movie-133701");
    expect(within(tears).getByTestId("card-status")).toHaveTextContent("Available");
    expect(within(trending).getByTestId("poster-movie-45745")).toHaveTextContent("4K");
    expect(within(screen.getByTestId("discover-row-popular_series")).getByTestId("card-status")).toHaveTextContent("Partly here");
    expect(within(screen.getByTestId("discover-row-my_requests")).getByTestId("request-status")).toHaveTextContent("On its way");
    expect(await screen.findByRole("link", { name: "Animation" })).toHaveAttribute("href", "/discover/list/genre_movies?id=16&name=Animation");
    // Cards are links, so the keyboard opens them.
    expect(tears.tagName).toBe("A");
    expect(tears).toHaveAttribute("href", "/discover/movie/133701");
  });

  it("explains that a TMDb key is needed", async () => {
    mockApi(({ path }) => {
      if (path === "/discover") return { status: 200, body: { metadataReady: false, musicReady: true, requests: [], rows: [] } };
      if (path === "/notifications") return emptyNotifications;
    });
    renderApp("/discover", { me: requester() });
    expect(await screen.findByText(/Discover needs a TMDb API key/)).toBeInTheDocument();
  });

  it("searches by kind", async () => {
    const seen: string[] = [];
    mockApi(({ path, query }) => {
      if (path === "/discover/search") {
        seen.push(`${query.get("q")}/${query.get("type")}`);
        return { status: 200, body: { page: 1, totalPages: 1, items: [item({ mediaType: "person", tmdbId: 5, title: "Derek de Lint", poster: "" })] } };
      }
      if (path === "/notifications") return emptyNotifications;
    });
    renderApp("/discover/search?q=tears&type=all", { me: requester() });
    expect(await screen.findByTestId("poster-person-5")).toHaveAttribute("href", "/discover/person/5");
    await userEvent.click(screen.getByTestId("search-tab-music"));
    await waitFor(() => expect(seen).toContain("tears/music"));
  });

  it("requests a movie from its title page and says when it is waiting for approval", async () => {
    const bodies: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/discover/movies/133701") return { status: 200, body: titleDetail() };
      if (path === "/requests/summary") return { status: 200, body: summary() };
      if (method === "POST" && path === "/requests") {
        bodies.push(body);
        return { status: 201, body: mediaRequest() };
      }
      if (path === "/notifications") return emptyNotifications;
    });
    renderApp("/discover/movie/133701", { me: requester() });
    expect(await screen.findByTestId("title-name")).toHaveTextContent("Tears of Steel");
    expect(screen.getByTestId("trailer-link")).toHaveAttribute("rel", "noopener noreferrer");
    expect(screen.getByRole("link", { name: /Derek de Lint/ })).toHaveAttribute("href", "/discover/person/5");
    await userEvent.click(screen.getByTestId("request-button"));
    const dialog = await screen.findByTestId("request-dialog");
    expect(await within(dialog).findByTestId("quota-line")).toHaveTextContent("3 of 5 requests left (per 7 days).");
    expect(within(dialog).queryByTestId("request-4k")).not.toBeInTheDocument();
    await userEvent.type(within(dialog).getByLabelText("Comment (optional)"), "for the kids");
    await userEvent.click(within(dialog).getByTestId("request-confirm"));
    expect(await within(dialog).findByTestId("request-result")).toHaveTextContent("Requested.");
    expect(bodies).toEqual([{ kind: "movie", tmdbId: 133701, comment: "for the kids" }]);
  });

  it("requests chosen seasons of a series in 4K", async () => {
    const bodies: unknown[] = [];
    const detail = titleDetail({
      item: item({ mediaType: "series", tmdbId: 800434, title: "Cosmos Laundromat", status: "partial" }),
      seasons: [
        { number: 1, title: "Season 1", episodeCount: 3, status: "available", status4k: "none" },
        { number: 2, title: "Season 2", episodeCount: 8, status: "none", status4k: "none" },
        { number: 3, title: "Season 3", episodeCount: 8, status: "pending", status4k: "pending" },
      ],
      ability: { canRequest: true, canRequest4k: true, autoApprove: false, autoApprove4k: true, canSetOptions: false, canReportIssue: true },
    });
    mockApi(({ method, path, body }) => {
      if (path === "/discover/series/800434") return { status: 200, body: detail };
      if (path === "/requests/summary") return { status: 200, body: summary() };
      if (method === "POST" && path === "/requests") {
        bodies.push(body);
        return { status: 201, body: mediaRequest({ kind: "series", is4k: true, status: "approved", autoApproved: true }) };
      }
      if (path === "/notifications") return emptyNotifications;
    });
    renderApp("/discover/series/800434", { me: requester() });
    expect(await screen.findByTestId("season-1-status")).toHaveTextContent("Available");
    await userEvent.click(screen.getByTestId("request-button"));
    const dialog = await screen.findByTestId("request-dialog");
    // Season 1 is here and season 3 already asked for: only season 2 is picked.
    expect(within(dialog).getByTestId("request-season-1")).toBeDisabled();
    expect(within(dialog).getByTestId("request-season-2")).toBeChecked();
    expect(within(dialog).getByTestId("request-season-3")).toBeDisabled();
    await userEvent.click(within(dialog).getByTestId("request-4k"));
    // On the 4K track season 1 is free again.
    expect(within(dialog).getByTestId("request-season-1")).toBeEnabled();
    await userEvent.click(within(dialog).getByTestId("request-season-2"));
    await userEvent.click(within(dialog).getByTestId("request-future-seasons"));
    await userEvent.click(within(dialog).getByTestId("request-confirm"));
    expect(await within(dialog).findByTestId("request-result")).toHaveTextContent("Approved automatically");
    expect(bodies).toEqual([{ kind: "series", tmdbId: 800434, is4k: true, seasons: [1], futureSeasons: true }]);
  });

  it("builds request bodies with options only when set", () => {
    expect(buildRequest({ kind: "album", mbid: "x", is4k: false, seasons: [2], allSeasons: false, futureSeasons: false, comment: " ", tags: "" }))
      .toEqual({ kind: "album", mbid: "x" });
    expect(buildRequest({ kind: "series", tmdbId: 1, is4k: false, seasons: [3, 1], allSeasons: true, futureSeasons: true, comment: "",
      profileId: 4, tags: "kids, hd" })).toEqual({ kind: "series", tmdbId: 1, allSeasons: true, futureSeasons: true,
      options: { qualityProfileId: 4, tags: ["kids", "hd"] } });
  });

  it("lets managers pick library options and reports a problem", async () => {
    const bodies: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/discover/movies/133701") return { status: 200, body: titleDetail({ item: item({ status: "available" }),
        ability: { canRequest: false, canRequest4k: false, autoApprove: true, autoApprove4k: true, canSetOptions: true, canReportIssue: true } }) };
      if (method === "POST" && path === "/issues") {
        bodies.push(body);
        return { status: 201, body: {} };
      }
      if (path === "/notifications") return emptyNotifications;
    });
    renderApp("/discover/movie/133701", { me: testUser() });
    expect(await screen.findByTestId("title-status")).toHaveTextContent("Available");
    expect(screen.queryByTestId("request-button")).not.toBeInTheDocument();
    await userEvent.click(screen.getByTestId("report-issue"));
    const dialog = await screen.findByTestId("issue-dialog");
    await userEvent.selectOptions(within(dialog).getByTestId("issue-type"), "audio");
    await userEvent.type(within(dialog).getByTestId("issue-message"), "out of sync");
    await userEvent.click(within(dialog).getByTestId("issue-send"));
    expect(await within(dialog).findByText(/Thanks/)).toBeInTheDocument();
    expect(bodies).toEqual([{ kind: "movie", tmdbId: 133701, type: "audio", message: "out of sync" }]);
  });
});
