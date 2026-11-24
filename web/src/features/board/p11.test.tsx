import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { BoardItem, BoardSummary, HealthReport } from "@/api/client";
import { mockApi, renderApp, testUser, type MockRequest } from "@/test-utils";
import { describeBoardItem, itemHref } from "./board";

const now = new Date().toISOString();

const summary: BoardSummary = {
  total: 3,
  updatedAt: now,
  groups: [
    { code: "path_not_visible", tone: "danger", fix: "path_mapping", count: 1, searches: 1,
      kinds: { movie: 1, episode: 0, album: 0, subtitle: 0 }, reasons: [] },
    { code: "all_rejected", tone: "warning", fix: "profile", count: 2, searches: 9,
      kinds: { movie: 1, episode: 1, album: 0, subtitle: 0 },
      reasons: [{ code: "size_too_large", count: 14, example: "too large" }, { code: "quality_not_allowed", count: 3, example: "q" }] },
  ],
};

const item = (over: Partial<BoardItem> = {}): BoardItem => ({
  key: "movie:7", kind: "movie", itemRef: "movie:7", parentRef: "", title: "Arrival (2016)", code: "path_not_visible", tone: "danger",
  fix: "path_mapping", message: "finished, but not visible", params: { title: "Arrival.2016.1080p", progress: 1,
    detail: "finished, but /downloads/Arrival is not visible to Locarr (add a remote path mapping or fix the /data mount)" },
  reasons: [], searches: 1, updatedAt: now, ...over,
});

describe("why didn't it land? board", () => {
  it("groups items by reason, adds up rejections and runs bulk actions on the selection", async () => {
    const actions: MockRequest[] = [];
    mockApi((req) => {
      if (req.path === "/board") return { status: 200, body: summary };
      if (req.path === "/board/items") {
        expect(req.query.get("code")).toBe("path_not_visible");
        expect(req.query.get("fix")).toBe("path_mapping");
        return { status: 200, body: { total: 1, items: [item()] } };
      }
      if (req.path === "/qualityprofiles") return { status: 200, body: [] };
      if (req.method === "POST" && req.path === "/board/actions") {
        actions.push(req);
        return { status: 200, body: { done: 1, failed: [] } };
      }
    });
    renderApp("/activity/board", { me: testUser() });
    const group = await screen.findByTestId("board-group-path_not_visible");
    expect(within(group).getByText("Finished, but the files are not visible")).toBeInTheDocument();
    expect(within(group).getByTestId("board-fix-path_not_visible")).toHaveAttribute("href", "/downloaders");
    const rejected = screen.getByTestId("board-group-all_rejected");
    expect(within(rejected).getByText(/too large · 14/)).toBeInTheDocument();

    await userEvent.click(within(group).getByRole("button", { name: /Finished, but the files are not visible/ }));
    const row = await screen.findByTestId("board-item-movie:7");
    expect(within(row).getByRole("link", { name: "Arrival (2016)" })).toHaveAttribute("href", "/movies/7");
    expect(row).toHaveTextContent("Locarr cannot see its files: finished, but /downloads/Arrival is not visible");
    expect(screen.getByTestId("board-action-retry_import")).toBeDisabled();
    await userEvent.click(within(row).getByRole("checkbox"));
    await userEvent.click(screen.getByTestId("board-action-retry_import"));
    await waitFor(() => expect(actions).toHaveLength(1));
    expect(actions[0]!.body).toEqual({ action: "retry_import", keys: ["movie:7"] });
    expect(await screen.findByTestId("board-action-result")).toHaveTextContent("Done: 1, failed: 0");
  });

  it("links items to their pages", () => {
    expect(itemHref({ kind: "episode", itemRef: "episode:5", parentRef: "series:2" })).toBe("/series/2");
    expect(itemHref({ kind: "album", itemRef: "album:3", parentRef: "artist:1" })).toBe("/music/albums/3");
    expect(itemHref({ kind: "subtitle", itemRef: "movie:4", parentRef: "" })).toBe("/movies/4");
  });

  it("describes subtitle items and falls back to the explainer's texts", () => {
    const t = ((k: string, o?: Record<string, unknown>) => (k === "board.itemCodes.subtitle_no_providers" ? `missing ${o?.languages}` : String(o?.defaultValue ?? k))) as never;
    expect(describeBoardItem(t, item({ code: "subtitle_no_providers", params: { languages: ["hr", "en"] } }), "en")).toBe("missing hr, en");
  });
});

describe("history", () => {
  it("filters by category, kind and text", async () => {
    const seen: URLSearchParams[] = [];
    mockApi((req) => {
      if (req.path === "/history") {
        seen.push(req.query);
        return { status: 200, body: { hasMore: false, retentionDays: 365, items: [
          { id: "d:1", at: now, category: "grabbed", type: "download_grabbed", itemRef: "movie:1", title: "Dune (2021)", itemTitle: "Dune (2021)",
            release: "Dune.2021.2160p", indexer: "Fake", message: "sent to qBittorrent" },
        ] } };
      }
    });
    renderApp("/activity/history", { me: testUser() });
    expect(await screen.findByRole("link", { name: "Dune (2021)" })).toHaveAttribute("href", "/movies/1");
    expect(screen.getByTestId("history-retention")).toHaveTextContent("older than 365 days");
    await userEvent.click(screen.getByTestId("history-toggle"));
    expect(screen.getByTestId("history-detail")).toHaveTextContent("Fake");
    await userEvent.selectOptions(screen.getByTestId("history-category"), "failed");
    await userEvent.selectOptions(screen.getByTestId("history-kind"), "series");
    await waitFor(() => expect(seen.at(-1)?.get("kind")).toBe("series"));
    expect(seen.at(-1)?.get("category")).toBe("failed");
    expect(seen.at(-1)?.get("offset")).toBe("0");
  });

  it("groups repeated failures, shows the old title, shortens URLs and clears with a confirmation", async () => {
    const long = "Q".repeat(400);
    const fail = (id: string, n: number) => ({
      id, at: new Date(Date.now() - n * 1000).toISOString(), category: "failed", type: "grab_failed", itemRef: "movie:2", title: "Iron Man (2008)",
      itemTitle: "Iron Man (2008)", itemGone: true, release: `Iron.Man.2008.720p-G${n}`, indexer: "X", message: `could not grab: request https://pro.example.net/8/download?link=${long}: refused`,
      data: { code: "indexer_limited", indexer: "X", title: `Iron.Man.2008.720p-G${n}` },
    });
    const clears: unknown[] = [];
    mockApi((req) => {
      if (req.path === "/history") {
        return { status: 200, body: { hasMore: false, retentionDays: 0, items: [fail("t:3", 1), fail("t:2", 2), fail("t:1", 3),
          { id: "t:0", at: now, category: "failed", type: "grab_failed", itemRef: "movie:9#removed", title: "Iron.Man.2008.1080p", itemTitle: "",
            itemGone: true, release: "Iron.Man.2008.1080p", message: "could not grab: request https://h.test/a?apikey=[redacted]: refused", data: {} }] } };
      }
      if (req.method === "POST" && req.path === "/history/clear") {
        clears.push(req.body);
        return { status: 200, body: { count: 4, deleted: !(req.body as { dryRun?: boolean }).dryRun } };
      }
    });
    renderApp("/activity/history", { me: testUser() });
    const group = await screen.findByTestId("history-group");
    expect(within(group).getByText("Iron Man (2008)")).toBeInTheDocument();
    expect(within(group).queryByRole("link")).toBeNull();
    expect(within(group).getByTestId("history-reason")).toHaveTextContent("Could not grab 3 releases");
    const single = screen.getByTestId("history-row");
    expect(single).toHaveTextContent("(removed item)");
    expect(within(single).getByTestId("history-reason")).toHaveTextContent("h.test/a?…");
    expect(within(single).getByTestId("history-reason").textContent).not.toContain("apikey");
    await userEvent.click(within(group).getByTestId("history-toggle"));
    expect(within(group).getByTestId("history-group-list").querySelectorAll("li")).toHaveLength(3);
    expect(screen.getByTestId("history-retention")).toHaveTextContent("kept until you delete them");

    await userEvent.click(screen.getByTestId("history-clear"));
    const dialog = await screen.findByTestId("history-clear-dialog");
    await userEvent.click(within(dialog).getByTestId("history-clear-failed"));
    await waitFor(() => expect(within(dialog).getByTestId("history-clear-count")).toHaveTextContent("Entries to delete: 4"));
    expect(dialog).toHaveTextContent("timeline");
    await userEvent.click(within(dialog).getByTestId("history-clear-confirm"));
    await waitFor(() => expect(clears.at(-1)).toEqual({ dryRun: false, category: "failed" }));
    expect(clears).toContainEqual({ dryRun: true, category: "failed" });
  });
});

describe("health", () => {
  it("translates issues and links to the fix", async () => {
    const report: HealthReport = { checkedAt: now, issues: [
      { key: "rootfolder:1", check: "rootfolders", severity: "warning", code: "rootfolder_low_space", message: "low",
        params: { path: "/data/media/movies", freeBytes: 1024 ** 3, minBytes: 10 * 1024 ** 3 }, docs: "disk-space", fixRoute: "/settings/media", since: now },
      { key: "update", check: "update", severity: "info", code: "update_available", message: "new", params: { latest: "1.2.0", current: "1.1.0" },
        docs: "update-available", since: now },
      { key: "x", check: "x", severity: "error", code: "something_new", message: "Server text", params: {}, docs: "", since: now },
    ] };
    mockApi((req) => (req.path === "/system/health" ? { status: 200, body: report } : undefined));
    renderApp("/system/health", { me: testUser() });
    const low = await screen.findByTestId("health-issue-rootfolder_low_space");
    expect(low).toHaveTextContent("Media folder /data/media/movies has only 1 GiB free (warning below 10 GiB).");
    expect(within(low).getByRole("link", { name: "Fix it" })).toHaveAttribute("href", "/settings/media");
    expect(within(low).getByRole("link", { name: /How to fix/ })).toHaveAttribute("href", expect.stringContaining("troubleshooting.md#disk-space"));
    expect(screen.getByTestId("health-issue-update_available")).toHaveTextContent("Locarr 1.2.0 is available (this is 1.1.0).");
    expect(screen.getByTestId("health-issue-something_new")).toHaveTextContent("Server text");
    // errors come first
    expect(screen.getAllByTestId(/health-issue-/)[0]).toHaveAttribute("data-testid", "health-issue-something_new");
  });
});

describe("backups", () => {
  it("restores only after RESTORE is typed", async () => {
    const restores: MockRequest[] = [];
    mockApi((req) => {
      if (req.path === "/system/backups" && req.method === "GET") {
        return { status: 200, body: [{ name: "locarr_backup_20261123_manual.zip", kind: "manual", size: 2048, createdAt: now, version: "0.1.0" }] };
      }
      if (req.method === "POST" && req.path === "/system/backups/locarr_backup_20261123_manual.zip/restore") {
        restores.push(req);
        return { status: 202, body: { restarting: true, preRestoreBackup: "locarr_backup_x_pre_restore.zip" } };
      }
    });
    renderApp("/system/backups", { me: testUser() });
    expect(await screen.findByText(/contains the secret key/)).toBeInTheDocument();
    await userEvent.click(await screen.findByTestId("backup-restore-locarr_backup_20261123_manual.zip"));
    const submit = screen.getByTestId("restore-submit");
    expect(submit).toBeDisabled();
    await userEvent.type(screen.getByTestId("restore-typed"), "restore");
    expect(submit).toBeDisabled();
    await userEvent.clear(screen.getByTestId("restore-typed"));
    await userEvent.type(screen.getByTestId("restore-typed"), "RESTORE");
    expect(submit).toBeEnabled();
    await userEvent.click(submit);
    await waitFor(() => expect(restores).toHaveLength(1));
    expect(restores[0]!.query.get("confirm")).toBe("RESTORE");
    expect(await screen.findByTestId("restore-restarting")).toBeInTheDocument();
  });
});

describe("first run", () => {
  it("creates media folders and offers the import wizard only when it exists", async () => {
    const created: MockRequest[] = [];
    let folders: unknown[] = [];
    mockApi((req) => {
      if (req.path === "/rootfolders" && req.method === "GET") return { status: 200, body: folders };
      if (req.path === "/rootfolders" && req.method === "POST") {
        created.push(req);
        folders = [{ id: 1, path: "/data/media/movies", kind: "movies", accessible: true, writable: true, freeSpaceBytes: 10, totalSpaceBytes: 20, createdAt: now }];
        return { status: 201, body: folders[0] };
      }
    });
    renderApp("/welcome", { me: testUser() });
    await userEvent.click(await screen.findByTestId("welcome-add-movies"));
    await waitFor(() => expect(created).toHaveLength(1));
    expect(created[0]!.body).toEqual({ path: "/data/media/movies", kind: "movies", create: true });
    expect(await within(screen.getByTestId("welcome-movies")).findByText("writable")).toBeInTheDocument();
    await userEvent.click(screen.getByTestId("welcome-next"));
    // The import wizard (P10) is offered as the next step.
    expect(await screen.findByTestId("welcome-migrate")).toHaveAttribute("href", "/migrate");
  });
});

describe("overview", () => {
  it("is the administrator's home and points to the welcome steps without media folders", async () => {
    mockApi((req) => {
      switch (req.path) {
        case "/rootfolders": return { status: 200, body: [] };
        case "/queue": return { status: 200, body: { items: [], clients: [] } };
        case "/board": return { status: 200, body: summary };
        case "/system/health": return { status: 200, body: { checkedAt: now, issues: [] } };
        case "/calendar": return { status: 200, body: [] };
        case "/history": return { status: 200, body: { items: [], hasMore: false } };
      }
    });
    const { router } = renderApp("/", { me: testUser() });
    expect(await screen.findByTestId("overview")).toBeInTheDocument();
    expect(await screen.findByTestId("overview-welcome-hint")).toBeInTheDocument();
    expect(await within(screen.getByTestId("overview-board")).findByText("Found, but every release was rejected")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/");
  });

  it("sends library managers to their movies", async () => {
    mockApi((req) => (req.path === "/movies" ? { status: 200, body: { items: [], total: 0 } } : undefined));
    const { router } = renderApp("/", { me: testUser({ role: "manager" }) });
    await waitFor(() => expect(router.state.location.pathname).toBe("/movies"));
  });
});
