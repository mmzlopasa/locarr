import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { MigrateItem, MigrateRun, MigrateSource } from "@/api/client";
import { mockApi, renderApp, testUser } from "@/test-utils";

const now = new Date().toISOString();
const source = (over: Partial<MigrateSource>): MigrateSource => ({
  id: 1, kind: "radarr", name: "Radarr", url: "http://radarr:7878", username: "", secretSet: true, appName: "Radarr", version: "5.14.0",
  lastError: "", createdAt: now, supports: { settings: true, library: true, users: false, history: true }, ...over,
});
const run = (over: Partial<MigrateRun>): MigrateRun => ({
  id: 7, status: "planned", error: "", createdAt: now, updatedAt: now,
  options: { sources: [{ id: 1, settings: true, library: true }], pathMappings: [{ sourceId: 1, from: "/movies", to: "/data/media/movies" }],
    resolutions: {}, historyDays: 90 },
  progress: { phase: "planned", done: 3, total: 3, message: "" },
  summary: { sources: [{ id: 1, kind: "radarr", name: "Radarr", version: "5.14.0", read: { movies: 2 }, roots: ["/movies"] }],
    pathChecks: [{ sourceId: 1, sourcePath: "/movies", localPath: "/data/media/movies", inside: true, exists: true, files: 2, found: 1,
      missing: ["/movies/B (2001)/b.mkv"] }],
    suggested: [], counts: { movie: { create: 2 }, qualityprofile: { update: 1 } }, statuses: { planned: 3 }, attention: { file_not_found: 1 },
    conflicts: 1, warnings: [] },
  ...over,
});
const items: MigrateItem[] = [
  { seq: 1, key: "1:qualityprofile:1", sourceId: 1, category: "qualityprofile", name: "HD-1080p", action: "update", reason: "name_exists",
    message: "", attention: [], status: "planned", localRef: "", error: "",
    conflict: { existing: "HD-1080p", options: ["merge", "rename", "skip"], chosen: "merge" } },
  { seq: 2, key: "1:movie:1", sourceId: 1, category: "movie", name: "A (2000)", action: "create", reason: "", message: "",
    attention: [], status: "planned", localRef: "", error: "" },
  { seq: 3, key: "1:movie:2", sourceId: 1, category: "movie", name: "B (2001)", action: "create", reason: "",
    message: "file not found at /data/media/movies/B (2001)/b.mkv", attention: ["file_not_found"], status: "planned", localRef: "", error: "" },
];

describe("import wizard", () => {
  it("adds a source after a test that shows the version", async () => {
    let sources: MigrateSource[] = [];
    const posted: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (method === "GET" && path === "/migrate/runs") return { status: 200, body: [] };
      if (method === "GET" && path === "/migrate/sources") return { status: 200, body: sources };
      if (method === "POST" && path === "/migrate/sources/test") return { status: 200, body: { ok: true, appName: "Sonarr", version: "4.0.11" } };
      if (method === "POST" && path === "/migrate/sources") {
        posted.push(body);
        sources = [source({ kind: "sonarr", name: "Sonarr", version: "4.0.11" })];
        return { status: 201, body: sources[0] };
      }
    });
    renderApp("/migrate", { me: testUser() });
    expect(await screen.findByText(/Nothing is changed on them/)).toBeInTheDocument();
    const form = screen.getByTestId("migrate-add-source");
    await userEvent.type(within(form).getByLabelText("API key"), "secret-key");
    await userEvent.click(within(form).getByRole("button", { name: "Test" }));
    expect(await within(form).findByTestId("migrate-test-ok")).toHaveTextContent("Connected to Sonarr 4.0.11");
    await userEvent.click(within(form).getByRole("button", { name: "Test and add" }));
    await waitFor(() => expect(posted).toHaveLength(1));
    expect(posted[0]).toMatchObject({ kind: "sonarr", url: "http://sonarr:8989", secret: "secret-key" });
    expect(await screen.findByTestId("source-sonarr")).toHaveTextContent("v4.0.11");
    // The key is never shown back.
    expect(screen.queryByText("secret-key")).not.toBeInTheDocument();
  });

  it("previews with counts and conflicts, re-plans a choice and imports", async () => {
    let current = run({});
    const plans: unknown[] = [];
    let imported = false;
    mockApi(({ method, path, body, query }) => {
      if (method === "GET" && path === "/migrate/runs") return { status: 200, body: [current] };
      if (method === "GET" && path === "/migrate/sources") return { status: 200, body: [source({})] };
      if (method === "GET" && path === "/migrate/runs/7") return { status: 200, body: current };
      if (method === "GET" && path === "/migrate/runs/7/items") {
        const list = query.get("attention") ? items.filter((i) => i.attention.length) : items;
        return { status: 200, body: { items: list, total: list.length } };
      }
      if (method === "POST" && path === "/migrate/runs/7/plan") {
        plans.push(body);
        return { status: 200, body: current };
      }
      if (method === "POST" && path === "/migrate/runs/7/import") {
        imported = true;
        current = run({ status: "done", summary: { ...run({}).summary, statuses: { done: 3 }, counts: { movie: { create: 2 } },
          refresh: { total: 2, done: 2, failed: 0 } } });
        return { status: 202, body: current };
      }
    });
    renderApp("/migrate", { me: testUser() });
    const counts = await screen.findByTestId("migrate-counts");
    expect(within(counts).getByTestId("count-movie")).toHaveTextContent("Movies");
    expect(screen.getByTestId("migrate-attention")).toHaveTextContent("1 with files not found");
    expect(await screen.findByText("B (2001)")).toBeInTheDocument();
    await userEvent.selectOptions(screen.getByTestId("migrate-resolution"), "rename");
    await userEvent.click(screen.getByTestId("migrate-replan"));
    await waitFor(() => expect(plans).toHaveLength(1));
    expect(plans[0]).toMatchObject({ resolutions: { "1:qualityprofile:1": "rename" } });
    await userEvent.click(screen.getByLabelText("Needs attention only"));
    await waitFor(() => expect(screen.queryByText("A (2000)")).not.toBeInTheDocument());
    await userEvent.click(screen.getByTestId("migrate-import"));
    await waitFor(() => expect(imported).toBe(true));
    expect(await screen.findByTestId("migrate-report")).toHaveTextContent("3 done, 0 skipped, 0 failed");
    expect(screen.getByTestId("migrate-refresh")).toHaveTextContent("Metadata loaded for 2 titles");
  });

  it("shows the switch-over checklist with manual steps", async () => {
    mockApi(({ method, path }) => {
      if (method === "GET" && path === "/migrate/runs") return { status: 200, body: [] };
      if (method === "GET" && path === "/migrate/checklist") {
        return { status: 200, body: [
          { key: "import_done", done: true, manual: false, params: {} },
          { key: "stop_sonarr", done: false, manual: true, params: { names: "Sonarr" } },
          { key: "rss", done: false, manual: false, params: { indexers: "0", minutes: "15" } },
        ] };
      }
    });
    renderApp("/migrate/checklist", { me: testUser() });
    const list = await screen.findByTestId("migrate-checklist");
    expect(await within(list).findByTestId("check-stop_sonarr")).toHaveTextContent("Stop automatic searches in Sonarr");
    expect(within(list).getByTestId("check-rss")).toHaveTextContent("To do");
    await userEvent.click(within(within(list).getByTestId("check-stop_sonarr")).getByLabelText("I did this"));
    expect(within(list).getByTestId("check-stop_sonarr")).toHaveTextContent("Done");
  });

  it("is for administrators only", async () => {
    mockApi(() => undefined);
    renderApp("/migrate", { me: testUser({ role: "manager" }) });
    await waitFor(() => expect(screen.queryByText("Import setup", { selector: "h1" })).not.toBeInTheDocument());
  });
});
