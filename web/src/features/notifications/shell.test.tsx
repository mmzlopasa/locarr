import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { emptyNotifications, requester, summary } from "@/features/discover/fixtures";

const now = new Date().toISOString();

describe("signed-in shell for requesters", () => {
  it("shows only Discover, Requests and Issues and keeps library pages out of reach", async () => {
    mockApi(({ path }) => {
      if (path === "/discover") return { status: 200, body: { metadataReady: true, musicReady: true, requests: [], rows: [] } };
      if (path === "/notifications") return emptyNotifications;
    });
    const { router } = renderApp("/movies", { me: requester() });
    await waitFor(() => expect(router.state.location.pathname).toBe("/discover"));
    const nav = screen.getAllByRole("navigation", { name: "Main navigation" })[0]!;
    for (const name of ["Discover", "Requests", "Issues"]) expect(within(nav).getByRole("link", { name })).toBeInTheDocument();
    for (const name of ["Movies", "Series", "Music", "Activity", "Calendar", "Wanted", "Search", "Indexers", "Downloaders", "Settings", "System"]) {
      expect(within(nav).queryByRole("link", { name })).not.toBeInTheDocument();
    }
    // Nothing on the page links to a download or a library page.
    for (const a of document.querySelectorAll("a")) {
      const href = a.getAttribute("href") ?? "";
      expect(href).not.toMatch(/\/indexers\/\d+\/download|^\/(movies|series|music|activity|search|wanted|calendar)/);
    }
    for (const path of ["/series/1", "/music", "/activity/queue", "/search", "/wanted", "/calendar"]) {
      await router.navigate({ to: path as never });
      await waitFor(() => expect(router.state.location.pathname).toBe("/discover"));
    }
  });

  it("sends people who manage the library to the library", async () => {
    mockApi(({ path }) => {
      if (path === "/movies") return { status: 200, body: { items: [], total: 0 } };
      if (path === "/qualityprofiles") return { status: 200, body: [] };
      if (path === "/notifications") return emptyNotifications;
    });
    const { router } = renderApp("/", { me: testUser({ role: "manager" }) });
    await waitFor(() => expect(router.state.location.pathname).toBe("/movies"));
  });

  it("counts unread notifications, opens one and marks all read", async () => {
    const reads: unknown[] = [];
    let unread = 2;
    mockApi(({ method, path, body }) => {
      if (path === "/notifications" && method === "GET") return { status: 200, body: { unread, latestId: 12, items: [
        { id: 12, type: "request.available", title: "Tears of Steel", requestId: 7, createdAt: now, read: unread === 0 },
        { id: 11, type: "request.declined", title: "Sintel", message: "No space", requestId: 6, createdAt: now, read: unread === 0 },
      ] } };
      if (path === "/notifications/read") {
        reads.push(body);
        unread = 0;
        return { status: 200, body: { unread: 0 } };
      }
      if (path === "/requests/7") return { status: 404, body: { code: "not_found", message: "" } };
      if (path === "/requests/summary") return { status: 200, body: summary() };
    });
    const { router } = renderApp("/issues", { me: requester() });
    const bell = (await screen.findAllByTestId("notif-bell"))[0]!;
    expect(await within(bell).findByTestId("notif-count")).toHaveTextContent("2");
    await userEvent.click(bell);
    const panel = await screen.findByTestId("notif-panel");
    const items = within(panel).getAllByTestId("notif-item");
    expect(items[0]!).toHaveTextContent("Tears of Steel is available.");
    expect(items[1]!).toHaveTextContent("Your request for Sintel was declined. (No space)");
    await userEvent.click(within(panel).getByTestId("notif-read-all"));
    await waitFor(() => expect(reads).toEqual([{ all: true }]));
    await waitFor(() => expect(within(bell).queryByTestId("notif-count")).not.toBeInTheDocument());
    await userEvent.click((await screen.findAllByTestId("notif-item"))[0]!);
    await waitFor(() => expect(router.state.location.pathname).toBe("/requests/7"));
  });
});
