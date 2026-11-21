import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Queue, QueueItem } from "@/api/client";
import { mockApi, renderApp, testUser } from "@/test-utils";

const now = new Date().toISOString();
const item = (over: Partial<QueueItem>): QueueItem => ({
  id: 1, unknown: false, clientId: 1, clientName: "qBit", clientItemId: "abc", protocol: "torrent", title: "Some.Movie.2024.1080p",
  status: "active", state: "downloading", rawState: "downloading", progress: 0.425, sizeBytes: 4 * 1024 ** 3,
  downloadedBytes: 1024 ** 3, downloadSpeed: 2 * 1024 ** 2, uploadSpeed: 0, etaSeconds: 3600, ratio: 0.1, seedingSeconds: 0,
  category: "movies", message: "", addedAt: now, updatedAt: now, ...over,
});
const queue: Queue = {
  items: [
    item({}),
    item({ id: 2, title: "Done.Movie", status: "completed", state: "seeding", progress: 1, etaSeconds: undefined }),
    item({ id: 0, unknown: true, title: "Linux.ISO", clientItemId: "fff", state: "seeding", progress: 1 }),
  ],
  clients: [{ id: 1, name: "qBit", protocol: "torrent", enabled: true, canPrioritise: true, status: { ok: true, checkedAt: now } }],
};

describe("queue page", () => {
  it("shows live progress, marks unknown items and calls the actions", async () => {
    const calls: string[] = [];
    mockApi(({ method, path, query }) => {
      if (method === "GET" && path === "/queue") return { status: 200, body: queue };
      if (method === "POST" && path === "/queue/1/pause") {
        calls.push("pause 1");
        return { status: 204 };
      }
      if (method === "DELETE" && path === "/queue/1") {
        calls.push(`remove 1 blocklist=${query.get("blocklist")}`);
        return { status: 204 };
      }
    });
    renderApp("/activity", { me: testUser() });
    const row = await screen.findByTestId("queue-row-1");
    expect(within(row).getByRole("progressbar")).toHaveAttribute("aria-valuenow", "42.5");
    expect(within(row).getByText("Downloading")).toBeInTheDocument();
    expect(within(row).getByText("2 MiB/s")).toBeInTheDocument();
    expect(within(screen.getByTestId("queue-row-2")).getByText("Ready to import")).toBeInTheDocument();
    const unknown = screen.getByTestId("queue-unknown");
    expect(within(unknown).getAllByText("Not added by Locarr").length).toBeGreaterThan(0);
    expect(within(unknown).queryByRole("button")).toBeNull();

    await userEvent.click(within(row).getByRole("button", { name: "Pause" }));
    await userEvent.click(within(row).getByTestId("blocklist-1"));
    await userEvent.click(within(row).getByTestId("blocklist-1-confirm"));
    await waitFor(() => expect(calls).toEqual(["pause 1", "remove 1 blocklist=true"]));
  });

  it("says so when nothing is downloading, and hides the queue from users without permission", async () => {
    mockApi(({ method, path }) => {
      if (method === "GET" && path === "/queue") return { status: 200, body: { items: [], clients: queue.clients } };
    });
    const { unmount } = renderApp("/activity/queue", { me: testUser() });
    expect(await screen.findByText("Nothing is downloading right now.")).toBeInTheDocument();
    unmount();
    renderApp("/activity/queue", {
      me: testUser({ role: "requester", permissions: { ...testUser().permissions, manageLibrary: false } }),
    });
    expect(await screen.findByTestId("activity-forbidden")).toBeInTheDocument();
  });
});
