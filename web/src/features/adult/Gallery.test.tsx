import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi, renderApp, testUser, type MockRequest } from "@/test-utils";

const adultUser = () =>
  testUser({ role: "admin", adultVisible: true, permissions: { ...testUser().permissions, manageLibrary: true, viewAdult: true } });

const base = {
  studio: "Studio A", studioId: 1, releaseDate: "2024-02-01", monitored: true, qualityProfileId: 12, addedAt: "2024-02-01T00:00:00Z",
  performers: [{ id: 3, name: "Performer One" }], sourceTags: [],
};
const scene = { ...base, id: 5, kind: "scene", ref: "scene:5", title: "Second Scene", path: "/data/media/adult/Studio A", status: "downloaded" };
const gallery = {
  ...base, id: 8, kind: "gallery", ref: "adult_gallery:8", title: "Second Scene", sceneId: 5, status: "downloaded",
  path: "/data/media/adult/Studio A - 2024-02-01 - Second Scene [Performer One]",
  file: { id: 2, path: "/data/media/adult/Studio A - 2024-02-01 - Second Scene [Performer One]", size: 4096, quality: "images", imageCount: 3 },
};

function api(calls: MockRequest[], extra?: (req: MockRequest) => { status: number; body?: unknown } | undefined) {
  return mockApi((req) => {
    calls.push(req);
    const hit = extra?.(req);
    if (hit) return hit;
    if (req.path === "/adult/preferences") return { status: 200, body: { blur: false } };
    if (req.path === "/adult/rootfolders") return { status: 200, body: [{ id: 1, path: "/data/media/adult" }] };
    if (req.path === "/adult/studios") return { status: 200, body: [{ id: 1, name: "Studio A", monitored: true, qualityProfileId: 12, path: "/x", addedAt: base.addedAt, scenes: 1, files: 1 }] };
    if (req.path === "/adult/performers") return { status: 200, body: [{ id: 3, name: "Performer One", monitored: false, addedAt: base.addedAt, items: 1 }] };
    if (req.path === "/qualityprofiles") return { status: 200, body: [] };
    if (req.path.startsWith("/adult/images/")) return { status: 404, body: { code: "not_found", message: "no" } };
    if (/^\/adult\/items\/8\/images\/\d+$/.test(req.path)) return { status: 404, body: { code: "not_found", message: "no" } };
    if (req.path === "/adult/items/8/images") {
      return { status: 200, body: [1, 2, 3].map((i) => ({ index: i, name: `000${i}.png`, size: 1000 })) };
    }
    if (req.path === "/adult/items/8/timeline" || req.path === "/adult/items/5/timeline") {
      return { status: 200, body: { events: [], explanation: { code: "downloaded", tone: "success", message: "", params: {}, reasons: [] } } };
    }
    return undefined;
  });
}

describe("adult galleries", () => {
  it("lists galleries in their own tab with the image count", async () => {
    const calls: MockRequest[] = [];
    api(calls, (req) => (req.method === "GET" && req.path === "/adult/items" ? { status: 200, body: { total: 1, items: [gallery] } } : undefined));
    renderApp("/adult/galleries", { me: adultUser() });
    const cards = await screen.findAllByTestId("adult-card");
    expect(cards).toHaveLength(1);
    expect(within(cards[0]!).getByText("Images: 3")).toBeInTheDocument();
    expect(calls.find((c) => c.path === "/adult/items")!.query.get("kind")).toBe("gallery");
    expect(screen.getByRole("link", { name: "Galleries" })).toBeInTheDocument();
  });

  it("shows a gallery's thumbnails, opens the viewer and moves through it", async () => {
    const calls: MockRequest[] = [];
    const fetchMock = api(calls, (req) => (req.path === "/adult/items/8" ? { status: 200, body: gallery } : undefined));
    renderApp("/adult/items/8", { me: adultUser() });
    const grid = await screen.findByTestId("adult-gallery-grid");
    const thumbs = await within(grid).findAllByTestId("adult-image");
    expect(thumbs).toHaveLength(3);
    // Thumbnails are asked for with ?thumb=true, through the session.
    await waitFor(() => expect(fetchMock.mock.calls.some(([u]) => String(u).endsWith("/api/v1/adult/items/8/images/1?thumb=true"))).toBe(true));
    expect(screen.getByTestId("adult-gallery-scene")).toHaveAttribute("href", "/adult/items/5");
    await waitFor(() => expect(thumbs[0]).toHaveAttribute("data-state", "placeholder"));
    await userEvent.click(thumbs[0]!);
    const box = await screen.findByTestId("adult-lightbox");
    expect(within(box).getByTestId("adult-lightbox-pos")).toHaveTextContent("1 / 3");
    await userEvent.click(within(box).getByTestId("adult-lightbox-next"));
    expect(within(box).getByTestId("adult-lightbox-pos")).toHaveTextContent("2 / 3");
    await userEvent.keyboard("{ArrowLeft}{ArrowLeft}");
    expect(within(box).getByTestId("adult-lightbox-pos")).toHaveTextContent("3 / 3");
    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByTestId("adult-lightbox")).not.toBeInTheDocument());
    // A gallery has no rename.
    expect(screen.queryByRole("button", { name: "Rename" })).not.toBeInTheDocument();
  });

  it("looks for a scene's photo set from the scene page", async () => {
    const calls: MockRequest[] = [];
    api(calls, (req) => {
      if (req.path === "/adult/items/5") return { status: 200, body: scene };
      if (req.method === "GET" && req.path === "/adult/items") return { status: 200, body: { total: 0, items: [] } };
      if (req.method === "POST" && req.path === "/adult/galleries") return { status: 201, body: gallery };
      return undefined;
    });
    renderApp("/adult/items/5", { me: adultUser() });
    await userEvent.click(await screen.findByTestId("adult-find-photo-set"));
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/adult/galleries")).toBe(true));
    expect(calls.find((c) => c.path === "/adult/galleries")!.body).toMatchObject({ sceneId: 5, monitored: true, search: true });
    expect(calls.find((c) => c.method === "GET" && c.path === "/adult/items")!.query.get("sceneId")).toBe("5");
  });

  it("adds a gallery by hand from Add (Type: Gallery)", async () => {
    const calls: MockRequest[] = [];
    api(calls, (req) => {
      if (req.method === "POST" && req.path === "/adult/galleries") return { status: 201, body: gallery };
      if (req.path === "/adult/items/8") return { status: 200, body: gallery };
      return undefined;
    });
    renderApp("/adult/add", { me: adultUser() });
    await userEvent.selectOptions(await screen.findByTestId("adult-add-type"), "gallery");
    expect(screen.queryByRole("button", { name: "Search" })).not.toBeInTheDocument();
    await userEvent.click(screen.getByTestId("adult-gallery-new-open"));
    const dialog = await screen.findByTestId("adult-gallery-new");
    expect(within(dialog).getByTestId("adult-gallery-create")).toBeDisabled();
    await userEvent.type(within(dialog).getByTestId("adult-gallery-title"), "Garden Set");
    await userEvent.selectOptions(within(dialog).getByLabelText("Studio in the library"), "1");
    await userEvent.click(within(dialog).getByTestId("adult-gallery-create"));
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/adult/galleries")).toBe(true));
    expect(calls.find((c) => c.path === "/adult/galleries")!.body).toMatchObject({ title: "Garden Set", studioId: 1, monitored: true, search: true });
    expect(await screen.findByTestId("adult-item")).toBeInTheDocument();
  });

  it("asks for the photo set when adding a scene with the option on", async () => {
    const calls: MockRequest[] = [];
    api(calls, (req) => {
      if (req.path === "/adult/lookup") {
        return { status: 200, body: { page: 1, more: false, results: [{ source: "stashdb", id: "sc-2", type: "scene", name: "Second Scene", studio: "Studio A" }] } };
      }
      if (req.method === "POST" && req.path === "/adult/items") return { status: 201, body: scene };
      if (req.path === "/adult/items/5") return { status: 200, body: scene };
      if (req.method === "GET" && req.path === "/adult/items") return { status: 200, body: { total: 0, items: [] } };
      return undefined;
    });
    renderApp("/adult/add", { me: adultUser() });
    await userEvent.selectOptions(await screen.findByTestId("adult-add-type"), "scene");
    await userEvent.click(screen.getByTestId("adult-also-set"));
    await userEvent.type(screen.getByLabelText("Search", { exact: true }), "second");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    const row = (await screen.findAllByTestId("adult-lookup-row"))[0]!;
    await userEvent.click(within(row).getByRole("button", { name: "Add" }));
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/adult/items")).toBe(true));
    expect(calls.find((c) => c.method === "POST" && c.path === "/adult/items")!.body).toMatchObject({ id: "sc-2", gallery: true });
  });
});
