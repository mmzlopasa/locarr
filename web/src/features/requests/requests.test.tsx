import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { emptyNotifications, mediaRequest, requester, summary } from "@/features/discover/fixtures";

describe("requests", () => {
  it("lets a manager approve several at once and decline one with a reason", async () => {
    const calls: { path: string; body: unknown }[] = [];
    const mine: (string | null)[] = [];
    const items = [mediaRequest({ id: 7 }), mediaRequest({ id: 8, kind: "series", title: "Cosmos Laundromat", tmdbId: 800434,
      seasons: [{ number: 1, status: "pending" }] }), mediaRequest({ id: 9, title: "Sintel", status: "processing",
      progress: { percent: 0.42, downloads: 1 }, explanation: { code: "downloading", tone: "info", message: "x", params: { progress: 0.42, title: "Sintel.2010", state: "downloading" } } })];
    mockApi(({ method, path, body, query }) => {
      if (method === "GET" && path === "/requests") {
        mine.push(query.get("mine"));
        return { status: 200, body: { items, total: items.length } };
      }
      if (path === "/requests/summary") return { status: 200, body: summary({ pending: 2 }) };
      if (method === "POST") {
        calls.push({ path, body });
        if (path === "/requests/approve") return { status: 200, body: { done: 2, errors: [] } };
        return { status: 200, body: mediaRequest({ status: "declined" }) };
      }
      if (path === "/notifications") return emptyNotifications;
    });
    renderApp("/requests?status=all", { me: testUser() });
    await screen.findByTestId("request-9");
    const list = screen.getByTestId("request-list");
    expect(mine[0]).toBe("false");
    expect(within(screen.getByTestId("request-9")).getByTestId("request-progress")).toHaveTextContent("42%");
    expect(within(screen.getByTestId("request-9")).getByTestId("request-explanation")).toHaveTextContent("Downloading Sintel.2010: 42%");
    expect(within(screen.getByTestId("request-8")).getByText("Series · seasons 1")).toBeInTheDocument();
    await userEvent.click(within(list).getByTestId("select-7"));
    await userEvent.click(within(list).getByTestId("select-8"));
    await userEvent.click(screen.getByTestId("bulk-approve"));
    await waitFor(() => expect(calls[0]).toEqual({ path: "/requests/approve", body: { ids: [7, 8] } }));

    await userEvent.click(screen.getByTestId("decline-7"));
    await userEvent.type(screen.getByTestId("decline-reason-7"), "Already on the shelf");
    await userEvent.click(screen.getByTestId("decline-confirm-7"));
    await waitFor(() => expect(calls[1]).toEqual({ path: "/requests/7/decline", body: { reason: "Already on the shelf" } }));
  });

  it("shows a requester only their own requests, with quota and cancel", async () => {
    const calls: string[] = [];
    const mine: (string | null)[] = [];
    mockApi(({ method, path, query }) => {
      if (method === "GET" && path === "/requests") {
        mine.push(query.get("mine"));
        return { status: 200, body: { items: [mediaRequest({ canCancel: true, requestedBy: { id: 2, username: "mia" } })], total: 1 } };
      }
      if (path === "/requests/summary") return { status: 200, body: summary() };
      if (method === "POST") {
        calls.push(path);
        return { status: 200, body: mediaRequest({ status: "cancelled" }) };
      }
      if (path === "/notifications") return emptyNotifications;
    });
    renderApp("/requests", { me: requester() });
    expect(await screen.findByTestId("request-7")).toBeInTheDocument();
    expect(mine[0]).toBe("true");
    expect(screen.queryByTestId("requests-view-all")).not.toBeInTheDocument();
    expect(screen.queryByTestId("approve-7")).not.toBeInTheDocument();
    expect(screen.getAllByTestId("quota-line")[0]).toHaveTextContent("Movie: 3 of 5 requests left");
    await userEvent.click(screen.getByTestId("cancel-7"));
    await waitFor(() => expect(calls).toEqual(["/requests/7/cancel"]));
  });

  it("edits options before approving", async () => {
    const calls: { path: string; body: unknown }[] = [];
    mockApi(({ method, path, body }) => {
      if (method === "GET" && path === "/requests") return { status: 200, body: { items: [mediaRequest()], total: 1 } };
      if (path === "/qualityprofiles") return { status: 200, body: [{ id: 3, name: "UHD", kind: "movies", items: [], cutoff: "", upgradesAllowed: true,
        minFormatScore: 0, cutoffFormatScore: 0, formatScores: [], languages: [] }] };
      if (path === "/rootfolders") return { status: 200, body: [] };
      if (method === "POST") {
        calls.push({ path, body });
        return { status: 200, body: mediaRequest({ status: "approved" }) };
      }
      if (path === "/notifications") return emptyNotifications;
    });
    renderApp("/requests", { me: testUser() });
    await userEvent.click(await screen.findByTestId("edit-7"));
    const dialog = await screen.findByTestId("approve-dialog");
    await userEvent.selectOptions(await within(dialog).findByLabelText("Quality profile"), "3");
    await userEvent.type(within(dialog).getByLabelText("Tags"), "kids");
    await userEvent.click(within(dialog).getByTestId("approve-confirm"));
    await waitFor(() => expect(calls).toEqual([{ path: "/requests/7/approve", body: { qualityProfileId: 3, tags: ["kids"] } }]));
  });
});
