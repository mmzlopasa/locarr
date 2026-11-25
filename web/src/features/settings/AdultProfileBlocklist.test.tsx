import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi, renderApp, testUser, type MockRequest } from "@/test-utils";

const settings = (over: Record<string, unknown> = {}) => ({ enabled: true, stashdbKeySet: false, tpdbKeySet: false, defaultSource: "stashdb",
  defaultProfileId: 12, defaultRootFolderId: 0, sceneFolder: "", sceneFile: "", movieFolder: "", movieFile: "", statsAggregate: false,
  notifyStreamero: false, tokens: [], protectedTerms: ["child", "underage"], openProfileId: 12,
  profileUsage: { items: 3, studios: 1, performers: 0 }, ...over });

function api(calls: MockRequest[], over: Record<string, unknown> = {}) {
  mockApi((req) => {
    calls.push(req);
    if (req.method === "GET" && req.path === "/adult/settings") return { status: 200, body: settings(over) };
    if (req.method === "GET" && req.path === "/qualityprofiles") {
      return { status: 200, body: [{ id: 12, name: "Any (Unknown allowed)", kind: "movies", items: [], cutoff: "unknown", upgradesAllowed: true,
        minFormatScore: 0, cutoffFormatScore: 0, formatScores: [], languages: [] }] };
    }
    if (req.method === "GET" && ["/adult/rootfolders", "/adult/blocklist", "/indexers"].includes(req.path)) return { status: 200, body: [] };
    if (req.method === "POST" && req.path === "/adult/items/quality-profile") return { status: 200, body: { updated: 3 } };
  });
}

describe("adult settings: quality profile and blocklist", () => {
  it("offers to move items still on another profile to the open one", async () => {
    const calls: MockRequest[] = [];
    api(calls);
    renderApp("/settings/adult", { me: testUser({ permissions: { ...testUser().permissions, viewAdult: true } }) });
    expect(await screen.findByTestId("adult-profile-usage")).toHaveTextContent("On another profile: 3 items, 1 followed studios, 0 followed performers.");
    await userEvent.click(await screen.findByTestId("adult-profile-apply"));
    await userEvent.click(screen.getByTestId("adult-profile-apply-confirm"));
    await waitFor(() => expect(calls.some((c) => c.path === "/adult/items/quality-profile")).toBe(true));
    expect(calls.find((c) => c.path === "/adult/items/quality-profile")?.body).toEqual({ profileId: 12 });
    expect(await screen.findByText("Items changed: 3")).toBeInTheDocument();
  });

  it("shows the protected terms as fixed chips and the owner's empty list", async () => {
    api([], { profileUsage: undefined });
    renderApp("/settings/adult", { me: testUser() });
    const fixed = await screen.findByTestId("adult-blocklist-protected");
    expect(fixed).toHaveTextContent("Always blocked: content involving minors");
    expect(within(fixed).getByText("child")).toBeInTheDocument();
    expect(within(fixed).queryByRole("button")).toBeNull();
    expect(await screen.findByTestId("adult-blocklist-empty")).toHaveTextContent("No terms yet.");
    expect(screen.queryByTestId("adult-profile-usage")).toBeNull();
  });
});
