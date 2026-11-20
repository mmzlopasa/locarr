import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { TaskList } from "@/api/client";
import { mockApi, renderApp, testUser } from "@/test-utils";

const now = Date.now();
const iso = (offsetSec: number) => new Date(now + offsetSec * 1000).toISOString();

const list: TaskList = {
  scheduled: [
    { name: "ping", kind: "ping", intervalSeconds: 900, nextRunAt: iso(600), lastRunAt: iso(-300), lastState: "succeeded" },
    { name: "housekeeping", kind: "housekeeping", intervalSeconds: 86400, nextRunAt: iso(3600) },
  ],
  jobs: [
    { id: 7, kind: "ping", queue: "default", state: "running", attempt: 1, maxAttempts: 5, runAt: iso(-5), createdAt: iso(-5), startedAt: iso(-4) },
    { id: 6, kind: "housekeeping", queue: "default", state: "failed", attempt: 5, maxAttempts: 5, runAt: iso(-90), createdAt: iso(-900),
      finishedAt: iso(-60), error: "disk full" },
  ],
};

describe("tasks page", () => {
  it("lists scheduled tasks and history, runs a task and cancels a running job", async () => {
    const actions: string[] = [];
    mockApi(({ method, path }) => {
      if (method === "GET" && path === "/system/tasks") return { status: 200, body: list };
      if (method === "POST" && path === "/system/tasks/housekeeping/run") {
        actions.push("run housekeeping");
        return { status: 202, body: { ...list.jobs[1], id: 8, state: "queued", attempt: 0 } };
      }
      if (method === "POST" && path === "/system/jobs/7/cancel") {
        actions.push("cancel 7");
        return { status: 204 };
      }
    });
    renderApp("/system/tasks", { me: testUser() });

    const scheduled = await screen.findByTestId("scheduled-tasks");
    expect(within(scheduled).getByText("Self-check")).toBeInTheDocument();
    expect(within(scheduled).getByText("Succeeded")).toBeInTheDocument();
    const history = screen.getByTestId("job-history");
    expect(within(history).getByText("disk full")).toBeInTheDocument();
    expect(within(history).getByText("Running")).toBeInTheDocument();
    expect(within(history).getByText("5/5")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Run Housekeeping now" }));
    // Only the running job can be cancelled; the failed one cannot.
    const cancels = within(history).getAllByRole("button", { name: "Cancel" });
    expect(cancels).toHaveLength(1);
    await userEvent.click(cancels[0]!);
    await waitFor(() => expect(actions).toEqual(["run housekeeping", "cancel 7"]));
  });
});
