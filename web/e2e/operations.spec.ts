import { expect, test, type Page } from "@playwright/test";

// Needs the CI compose stack: the fake services (scripts/dev-fakes.sh with LOCARR_FAKES_SEED_DIR), Locarr with the
// fake TMDb, and a Transmission named "transmission" that saves into /data/downloads/fake-seed (see .github/workflows/ci.yml).
const setupCode = process.env.E2E_SETUP_CODE ?? "E2E-SETUP";
const adminUser = process.env.E2E_ADMIN_USER ?? "admin";
const adminPassword = process.env.E2E_ADMIN_PASSWORD ?? "e2e-password-123";
const tmHost = process.env.E2E_TRANSMISSION_HOST ?? "transmission";
const tmUser = process.env.E2E_TRANSMISSION_USER ?? "locarr";
const tmPass = process.env.E2E_TRANSMISSION_PASS ?? "locarr-e2e";
const tmdbKey = process.env.E2E_TMDB_KEY ?? "fake0000000000000000000000tmdb01";
const indexerUrl = process.env.E2E_INDEXER_URL ?? "http://fake-indexer:9117/torznab";
const moviesRoot = process.env.E2E_MOVIES_ROOT ?? "/data/media/movies";
const seedDir = "/data/downloads/fake-seed";
const notMounted = "/data/downloads/not-mounted";
const sintel = 45745;

async function signInAsAdmin(page: Page) {
  await page.addInitScript(() => localStorage.setItem("locarr.lang", "en"));
  await page.goto("/");
  await page.waitForURL(/\/(setup|login)/);
  if (page.url().includes("/setup")) {
    await page.getByLabel("Setup code").fill(setupCode);
    await page.getByLabel("Administrator username").fill(adminUser);
    await page.getByLabel("Password", { exact: true }).fill(adminPassword);
    await page.getByLabel("Repeat password").fill(adminPassword);
    await page.getByRole("button", { name: "Create administrator" }).click();
  } else {
    await page.getByLabel("Username").fill(adminUser);
    await page.getByLabel("Password").fill(adminPassword);
    await page.getByRole("button", { name: "Sign in" }).click();
  }
  await expect(page.getByTestId("current-username")).toHaveText(adminUser);
}

type Mapping = { id: number; host: string; remotePath: string; localPath: string };

async function removeTestMappings(page: Page) {
  const list = (await (await page.request.get("/api/v1/remotepathmappings")).json()) as Mapping[];
  for (const m of list) {
    if (m.host === tmHost && m.localPath === notMounted) await page.request.delete(`/api/v1/remotepathmappings/${m.id}`);
  }
}

test("why didn't it land: a finished download Locarr cannot see, fixed by the path mapping, then imported", async ({ page }) => {
  test.skip(tmHost === "none", "no Transmission instance for this run");
  test.setTimeout(240_000);
  await signInAsAdmin(page);

  // What the flow needs (each call may find it already there).
  const ok = (status: number) => [200, 201, 409].includes(status);
  const key = await page.request.put("/api/v1/settings/metadata", { data: { tmdbApiKey: tmdbKey } });
  expect(key.ok(), await key.text()).toBeTruthy();
  const ix = await page.request.post("/api/v1/indexers", { data: { name: "Fake movies indexer", kind: "torznab", baseUrl: indexerUrl, apiKey: "fakekey" } });
  expect(ok(ix.status()), await ix.text()).toBeTruthy();
  const cl = await page.request.post("/api/v1/downloadclients", { data: { name: "Transmission movies", implementation: "transmission",
    host: tmHost, port: 9091, username: tmUser, password: tmPass, categories: { movies: "locarr-movies", series: "", music: "" } } });
  expect(ok(cl.status()), await cl.text()).toBeTruthy();
  const rf = await page.request.post("/api/v1/rootfolders", { data: { path: moviesRoot, kind: "movies" } });
  expect(ok(rf.status()), await rf.text()).toBeTruthy();
  const leftovers = await (await page.request.get("/api/v1/movies?q=Sintel")).json();
  for (const m of leftovers.items ?? []) await page.request.delete(`/api/v1/movies/${m.id}?deleteFiles=true`);
  await removeTestMappings(page);

  // The client's download folder maps to a folder this container does not have: the files are "not visible".
  const mapping = await page.request.post("/api/v1/remotepathmappings", { data: { host: tmHost, remotePath: seedDir, localPath: notMounted } });
  expect(mapping.status(), await mapping.text()).toBe(201);

  try {
    const roots = await (await page.request.get("/api/v1/rootfolders")).json();
    const root = roots.find((r: { path: string }) => r.path === moviesRoot);
    const profiles = await (await page.request.get("/api/v1/qualityprofiles")).json();
    const profile = profiles.find((p: { name: string; kind: string }) => p.kind === "movies" && p.name === "Any");
    const added = await page.request.post("/api/v1/movies", { data: { tmdbId: sintel, rootFolderId: root.id, qualityProfileId: profile.id,
      minimumAvailability: "released", searchOnAdd: false } });
    expect(added.status(), await added.text()).toBe(201);
    const movie = await added.json();

    // Grab the fake indexer's Sintel release by hand (the decision does not matter for this flow).
    const search = await (await page.request.get(`/api/v1/movies/${movie.id}/releases`)).json();
    const pick = (search.decisions ?? []).find((d: { decision: { candidate: { title: string } } }) => d.decision.candidate.title.startsWith("Sintel.2010"));
    expect(pick, JSON.stringify(search.decisions?.map((d: { decision: { candidate: { title: string } } }) => d.decision.candidate.title))).toBeTruthy();
    const c = pick.decision.candidate;
    const grab = await page.request.post(`/api/v1/movies/${movie.id}/releases`, { data: { indexerId: c.indexerId, downloadToken: pick.downloadToken,
      magnetUrl: pick.magnetUrl, title: c.title, protocol: c.protocol ?? "torrent", guid: c.guid } });
    expect(grab.status(), await grab.text()).toBe(201);

    // The board explains it: finished, but not visible, with a link to where mappings are fixed.
    await page.goto("/activity/board");
    const group = page.getByTestId("board-group-path_not_visible");
    await expect(async () => {
      await page.reload();
      await expect(group).toBeVisible({ timeout: 3_000 });
    }).toPass({ timeout: 150_000, intervals: [3_000] });
    await group.getByRole("button").first().click();
    const item = page.getByTestId(`board-item-movie:${movie.id}`);
    await expect(item).toContainText("Locarr cannot see");
    await expect(group.getByTestId("board-fix-path_not_visible")).toHaveAttribute("href", "/downloaders");

    // Fix it: remove the wrong mapping on the download clients page.
    await group.getByTestId("board-fix-path_not_visible").click();
    await expect(page).toHaveURL(/\/downloaders$/);
    const row = page.locator("li").filter({ hasText: `${tmHost}:${seedDir} → ${notMounted}` });
    await row.getByRole("button", { name: "Remove" }).click();
    await row.getByRole("button", { name: "Remove it" }).click();
    await expect(row).toHaveCount(0);

    // Retry the import from the board (the tracker may already have picked it up by itself).
    await page.goto("/activity/board");
    await page.waitForLoadState("networkidle");
    if (await group.isVisible()) {
      await group.getByRole("button").first().click();
      if (await item.isVisible({ timeout: 5_000 }).catch(() => false)) {
        await item.getByRole("checkbox").check();
        await page.getByTestId("board-action-retry_import").click();
        await expect(page.getByTestId("board-action-result")).toBeVisible();
      }
    }

    await expect.poll(async () => {
      const m = await (await page.request.get(`/api/v1/movies/${movie.id}`)).json();
      return Boolean(m.file);
    }, { timeout: 120_000, intervals: [2_000] }).toBe(true);
    await page.goto(`/movies/${movie.id}`);
    await page.getByTestId("tab-overview").click();
    await expect(page.getByTestId("movie-file")).toContainText("Sintel");

    // The import shows up in the history.
    await page.goto("/activity/history");
    await page.getByTestId("history-category").selectOption("imported");
    await expect(page.getByTestId("history-list")).toContainText("Sintel");
  } finally {
    await removeTestMappings(page);
  }
});

test("backup and restore: a change made after the backup is gone and the administrator stays signed in", async ({ page }) => {
  test.setTimeout(180_000);
  await signInAsAdmin(page);
  const before = await (await page.request.get("/api/v1/settings")).json();

  await page.goto("/system/backups");
  await expect(page.getByText(/contains the secret key/)).toBeVisible();
  const made = page.waitForResponse((r) => r.url().endsWith("/api/v1/system/backups") && r.request().method() === "POST");
  await page.getByTestId("backup-now").click();
  const backup = await (await made).json();
  expect(backup.name).toMatch(/^locarr_backup_.*\.zip$/);
  await expect(page.getByTestId(`backup-${backup.name}`)).toBeVisible();

  // A change after the backup.
  const changed = `Restore e2e ${Date.now().toString(36)}`;
  const patch = await page.request.patch("/api/v1/settings", { data: { instanceName: changed } });
  expect(patch.ok(), await patch.text()).toBeTruthy();
  expect((await (await page.request.get("/api/v1/settings")).json()).instanceName).toBe(changed);

  await page.reload();
  await page.getByTestId(`backup-restore-${backup.name}`).click();
  await expect(page.getByTestId("restore-submit")).toBeDisabled();
  await page.getByTestId("restore-typed").fill("RESTORE");
  const reloaded = page.waitForEvent("load", { timeout: 150_000 });
  await page.getByTestId("restore-submit").click();
  await expect(page.getByTestId("restore-restarting")).toBeVisible();
  await reloaded;

  // The page reloads by itself when Locarr is back; the restorer's session survives the restore.
  await expect(page.getByTestId("current-username")).toHaveText(adminUser, { timeout: 120_000 });
  await expect.poll(async () => {
    const res = await page.request.get("/api/v1/settings");
    return res.ok() ? (await res.json()).instanceName : "";
  }, { timeout: 60_000, intervals: [1_000] }).toBe(before.instanceName);
  // The backup made just before the restore is listed too.
  await page.goto("/system/backups");
  await expect(page.getByTestId("backup-list")).toContainText("Before restore");
});
