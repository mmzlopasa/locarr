import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi, renderApp, testUser, type MockRequest } from "@/test-utils";
import { initial, loadPref, PREF } from "./logic";

const adultUser = (manage = true) =>
  testUser({ role: manage ? "admin" : "manager", adultVisible: true,
    permissions: { ...testUser().permissions, manageLibrary: manage, viewAdult: true } });

const item = (id: number, over: Record<string, unknown> = {}) => ({
  id, kind: "scene", ref: `scene:${id}`, studio: "Studio A", studioId: 1, title: `Scene ${id}`, releaseDate: `2024-01-${String(10 + id).padStart(2, "0")}`,
  monitored: false, qualityProfileId: 12, path: "/data/media/adult/Studio A", addedAt: "2024-02-01T00:00:00Z", performers: [{ id: 3, name: "Performer One" }],
  sourceTags: [], status: "missing", image: "0123456789abcdef0123456789abcdef", ...over,
});

function api(calls: MockRequest[], extra?: (req: MockRequest) => { status: number; body?: unknown } | undefined) {
  mockApi((req) => {
    calls.push(req);
    const hit = extra?.(req);
    if (hit) return hit;
    if (req.path === "/adult/preferences") return { status: 200, body: { blur: true } };
    if (req.path === "/adult/rootfolders") return { status: 200, body: [{ id: 1, path: "/data/media/adult" }] };
    if (req.path === "/qualityprofiles") return { status: 200, body: [] };
    if (req.path.startsWith("/adult/images/")) return { status: 404, body: { code: "not_found", message: "no" } };
    if (req.method === "GET" && req.path === "/adult/items") return { status: 200, body: { total: 2, items: [item(1), item(2, { status: "downloaded", file: { id: 9, path: "/x.mkv", size: 1, quality: "WEBDL-1080p" } })] } };
    return undefined;
  });
}

beforeEach(() => {
  try {
    localStorage.clear();
  } catch {
    // no storage in this environment
  }
});

describe("adult search (Add)", () => {
  it("asks for newest first by default, keeps the chosen order and shows thumbnails", async () => {
    const calls: MockRequest[] = [];
    api(calls, (req) => {
      if (req.path !== "/adult/lookup") return undefined;
      const sort = req.query.get("sort");
      const results = [
        { source: "stashdb", id: "a", type: "scene", name: "Newer", date: "2023-05-01", studio: "Studio A", performers: ["Performer One"], image: "0123456789abcdef0123456789abcdef" },
        { source: "stashdb", id: "b", type: "scene", name: "Older", date: "2015-02-01", studio: "Studio A", inLibrary: true },
      ];
      return { status: 200, body: { page: Number(req.query.get("page")), more: req.query.get("page") === "1", results: sort === "oldest" ? results.reverse() : results } };
    });
    renderApp("/adult/add", { me: adultUser() });
    await userEvent.selectOptions(await screen.findByLabelText("Type"), "scene");
    expect(screen.getByTestId("adult-lookup-sort")).toHaveValue("newest");
    await userEvent.type(screen.getByLabelText("Search", { exact: true }), "performer");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    const rows = await screen.findAllByTestId("adult-lookup-row");
    expect(rows[0]!).toHaveTextContent("Newer");
    expect(within(rows[1]!).getByText("In library")).toBeInTheDocument();
    const first = calls.find((c) => c.path === "/adult/lookup")!;
    expect(first.query.get("sort")).toBe("newest");
    expect(first.query.get("page")).toBe("1");
    // A thumbnail that could not be loaded is a neutral placeholder with the studio initial.
    await waitFor(() => expect(within(rows[0]!).getByTestId("adult-image")).toHaveAttribute("data-state", "placeholder"));
    expect(within(rows[0]!).getByTestId("adult-image")).toHaveTextContent("S");

    // Load more asks for the next page.
    await userEvent.click(screen.getByTestId("adult-lookup-more"));
    await waitFor(() => expect(calls.some((c) => c.path === "/adult/lookup" && c.query.get("page") === "2")).toBe(true));

    // Another order searches again with it and is remembered.
    await userEvent.selectOptions(screen.getByTestId("adult-lookup-sort"), "oldest");
    await waitFor(() => expect(calls.some((c) => c.path === "/adult/lookup" && c.query.get("sort") === "oldest")).toBe(true));
    await waitFor(() => expect(screen.getAllByTestId("adult-lookup-row")[0]).toHaveTextContent("Older"));
    expect(loadPref(PREF.lookupSort, ["newest", "oldest", "relevance", "title"] as const, "newest")).toBe("oldest");
  });

  it("adds the selected results with the chosen profile and root folder", async () => {
    const calls: MockRequest[] = [];
    api(calls, (req) => {
      if (req.path === "/qualityprofiles") return { status: 200, body: [{ id: 7, name: "Any", kind: "movies", items: [], cutoff: "unknown", upgradesAllowed: true, minFormatScore: 0, cutoffFormatScore: 0, formatScores: [], languages: [] }] };
      if (req.path === "/adult/lookup") {
        return { status: 200, body: { page: 1, more: false, results: [
          { source: "tpdb", id: "x1", type: "scene", name: "First", date: "2024-01-01" },
          { source: "tpdb", id: "x2", type: "scene", name: "Second", date: "2023-01-01" },
        ] } };
      }
      if (req.method === "POST" && req.path === "/adult/items") return { status: 201, body: item(5) };
      return undefined;
    });
    renderApp("/adult/add", { me: adultUser() });
    await userEvent.selectOptions(await screen.findByLabelText("Source"), "tpdb");
    await userEvent.selectOptions(screen.getByLabelText("Type"), "scene");
    await userEvent.selectOptions(screen.getByLabelText("Quality profile"), "7");
    await userEvent.type(screen.getByLabelText("Search", { exact: true }), "x");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    await screen.findAllByTestId("adult-lookup-row");
    await userEvent.click(screen.getByLabelText("Select all"));
    await userEvent.click(screen.getByTestId("adult-add-selected"));
    await waitFor(() => expect(calls.filter((c) => c.method === "POST" && c.path === "/adult/items")).toHaveLength(2));
    expect(calls.find((c) => c.method === "POST" && c.path === "/adult/items")?.body).toMatchObject({ source: "tpdb", id: "x1", qualityProfileId: 7, kind: "scene" });
    await waitFor(() => expect(screen.getAllByText("In library")).toHaveLength(2));
  });
});

describe("adult scenes", () => {
  it("shows status and quality, and bulk-monitors the selection", async () => {
    const calls: MockRequest[] = [];
    api(calls, (req) => (req.method === "POST" && req.path === "/adult/items/bulk" ? { status: 200, body: { changed: 2 } } : undefined));
    renderApp("/adult", { me: adultUser() });
    const cards = await screen.findAllByTestId("adult-card");
    expect(within(cards[0]!).getByTestId("adult-status")).toHaveAttribute("data-status", "missing");
    expect(within(cards[1]!).getByText("WEBDL-1080p")).toBeInTheDocument();
    // No image: the placeholder, never the eye-slash.
    await waitFor(() => expect(within(cards[0]!).getByTestId("adult-image")).toHaveAttribute("data-state", "placeholder"));
    expect(within(cards[0]!).queryByText("Click to show")).toBeNull();

    await userEvent.click(screen.getByTestId("adult-select"));
    await userEvent.click(screen.getAllByTestId("adult-card")[0]!);
    await userEvent.click(screen.getAllByTestId("adult-card")[1]!);
    expect(screen.getByTestId("adult-selected-count")).toHaveTextContent("Selected: 2");
    await userEvent.click(screen.getByTestId("adult-bulk-monitor"));
    await waitFor(() => expect(calls.some((c) => c.path === "/adult/items/bulk")).toBe(true));
    expect(calls.find((c) => c.path === "/adult/items/bulk")?.body).toEqual({ ids: [1, 2], action: "monitor", deleteFiles: false, exclude: false });
  });

  it("sends the sort, status and monitored filters", async () => {
    const calls: MockRequest[] = [];
    api(calls);
    renderApp("/adult", { me: adultUser() });
    await screen.findAllByTestId("adult-card");
    await userEvent.selectOptions(screen.getByTestId("adult-item-sort"), "added");
    await userEvent.selectOptions(screen.getByTestId("adult-status-filter"), "wanted");
    await waitFor(() => {
      const last = calls.filter((c) => c.path === "/adult/items").at(-1)!;
      expect(last.query.get("sort")).toBe("added");
      expect(last.query.get("status")).toBe("wanted");
      expect(last.query.get("kind")).toBe("scene");
    });
  });

  it("offers no selection or removal to a viewer who cannot manage the library", async () => {
    api([]);
    renderApp("/adult", { me: adultUser(false) });
    await screen.findAllByTestId("adult-card");
    expect(screen.queryByTestId("adult-select")).toBeNull();
  });
});

describe("removing a scene", () => {
  it("defaults to not interested, keeps the file unless asked", async () => {
    const calls: MockRequest[] = [];
    api(calls, (req) => {
      if (req.method === "GET" && req.path === "/adult/items/2") return { status: 200, body: item(2, { status: "downloaded", file: { id: 9, path: "/x.mkv", size: 1, quality: "WEBDL-1080p" } }) };
      if (req.method === "GET" && req.path === "/adult/items/2/timeline") return { status: 200, body: { events: [], explanation: { code: "downloaded", tone: "success", message: "", params: {}, reasons: [] } } };
      if (req.method === "DELETE" && req.path === "/adult/items/2") return { status: 204 };
      return undefined;
    });
    renderApp("/adult/items/2", { me: adultUser() });
    await userEvent.click(await screen.findByTestId("adult-remove"));
    const dialog = await screen.findByTestId("adult-remove-dialog");
    expect(within(dialog).getByTestId("adult-remove-exclude")).toBeChecked();
    expect(within(dialog).getByTestId("adult-remove-files")).not.toBeChecked();
    await userEvent.click(within(dialog).getByTestId("adult-remove-confirm"));
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE")).toBe(true));
    const del = calls.find((c) => c.method === "DELETE")!;
    expect(del.query.get("exclude")).toBe("true");
    expect(del.query.get("deleteFiles")).toBe("false");
  });
});

describe("helpers", () => {
  it("takes the first letter or digit as the initial", () => {
    expect(initial("  studio a")).toBe("S");
    expect(initial("21 Naturals")).toBe("2");
    expect(initial("")).toBe("?");
  });
});
