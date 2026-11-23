import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { emptyNotifications } from "@/features/discover/fixtures";
import { compatUrls } from "./StreameroCompatPanel";

describe("Streamero integration card", () => {
  it("builds the compatible addresses from the base", () => {
    expect(compatUrls("http://nas:8484/")).toEqual({
      seerr: "http://nas:8484/compat/seerr",
      radarr: "http://nas:8484/compat/radarr",
      sonarr: "http://nas:8484/compat/sonarr",
    });
  });

  it("shows the values for Streamero and creates a key", async () => {
    const created: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/mediaserver/streamero" && method === "GET") {
        return { status: 200, body: { enabled: false, url: "", publicUrl: "", apiKey: { configured: false }, pathMappings: [], libraryMap: [],
          signIn: { enabled: false, autoCreate: false }, status: { indexedItems: 0 } } };
      }
      if (path === "/rootfolders") return { status: 200, body: [] };
      if (path === "/auth/apikeys" && method === "POST") {
        created.push(body);
        return { status: 201, body: { key: "lk_streamerokey123", apiKey: { id: 4, name: "Streamero", prefix: "lk_strea", readOnly: false,
          createdAt: new Date().toISOString() } } };
      }
      if (path === "/notifications") return emptyNotifications;
    });
    renderApp("/settings/mediaserver", { me: testUser() });
    const origin = window.location.origin;
    expect(await screen.findByTestId("compat-url-seerr")).toHaveTextContent(`${origin}/compat/seerr`);
    expect(screen.getByTestId("compat-url-radarr")).toHaveTextContent(`${origin}/compat/radarr`);
    expect(screen.getByTestId("compat-url-sonarr")).toHaveTextContent(`${origin}/compat/sonarr`);
    await userEvent.click(screen.getByTestId("compat-create-key"));
    await waitFor(() => expect(created).toEqual([{ name: "Streamero", readOnly: false }]));
    expect(await screen.findByTestId("compat-created-key")).toHaveTextContent("lk_streamerokey123");
  });
});
