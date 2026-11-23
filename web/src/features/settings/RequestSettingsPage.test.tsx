import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { RequestSettings } from "@/api/client";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { emptyNotifications } from "@/features/discover/fixtures";

const now = new Date().toISOString();
const kd = { qualityProfileId: 0, rootFolderId: 0, qualityProfile4kId: 0, rootFolder4kId: 0, tags: [] };
const settings: RequestSettings = { movie: { ...kd, minimumAvailability: "released" }, series: kd, music: { ...kd, metadataProfileId: 0 },
  quotas: { movie: { limit: 0, days: 7 }, series: { limit: 0, days: 7 }, music: { limit: 0, days: 7 } } };

describe("request settings", () => {
  it("saves defaults and quotas", async () => {
    const saved: RequestSettings[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/settings/requests" && method === "GET") return { status: 200, body: settings };
      if (path === "/settings/requests" && method === "PUT") {
        saved.push(body as RequestSettings);
        return { status: 200, body };
      }
      if (path === "/qualityprofiles") return { status: 200, body: [{ id: 2, name: "HD-720p/1080p", kind: "movies", items: [], cutoff: "",
        upgradesAllowed: true, minFormatScore: 0, cutoffFormatScore: 0, formatScores: [], languages: [] }] };
      if (path === "/rootfolders") return { status: 200, body: [{ id: 5, path: "/data/media/movies", kind: "movies", accessible: true, writable: true, createdAt: now }] };
      if (path === "/music/metadataprofiles") return { status: 200, body: [] };
      if (path === "/notifications") return emptyNotifications;
    });
    renderApp("/settings/requests", { me: testUser() });
    const movie = await screen.findByTestId("defaults-movie");
    await waitFor(() => expect(within(movie).getAllByRole("option", { name: "HD-720p/1080p" })[0]).toBeInTheDocument());
    await userEvent.selectOptions(screen.getByTestId("defaults-movie-profile"), "2");
    await userEvent.selectOptions(screen.getByTestId("defaults-movie-root"), "5");
    const limit = screen.getByTestId("quota-series-limit");
    await userEvent.clear(limit);
    await userEvent.type(limit, "3");
    await userEvent.click(screen.getByTestId("save-request-settings"));
    await waitFor(() => expect(saved).toHaveLength(1));
    expect(saved[0]!.movie).toMatchObject({ qualityProfileId: 2, rootFolderId: 5 });
    expect(saved[0]!.quotas.series).toEqual({ limit: 3, days: 7 });
  });
});
