import { expect, test, type Page } from "@playwright/test";

// Same environment as movies.spec.ts: the fake indexer and TMDb (with LOCARR_FAKES_SEED_DIR), Locarr pointed at
// the fake TMDb and a Transmission that saves into the seed folder. The fake indexer's season pack is a real
// multi-file torrent whose data the fake writes there, so Transmission completes it without any network.
const setupCode = process.env.E2E_SETUP_CODE ?? "E2E-SETUP";
const adminUser = process.env.E2E_ADMIN_USER ?? "admin";
const adminPassword = process.env.E2E_ADMIN_PASSWORD ?? "e2e-password-123";
const tmHost = process.env.E2E_TRANSMISSION_HOST ?? "transmission";
const tmUser = process.env.E2E_TRANSMISSION_USER ?? "locarr";
const tmPass = process.env.E2E_TRANSMISSION_PASS ?? "locarr-e2e";
const tmdbKey = process.env.E2E_TMDB_KEY ?? "fake0000000000000000000000tmdb01";
const indexerUrl = process.env.E2E_INDEXER_URL ?? "http://fake-indexer:9117/torznab";
const tvRoot = process.env.E2E_TV_ROOT ?? "/data/media/tv";

async function signInAsAdmin(page: Page) {
  await page.addInitScript(() => localStorage.setItem("locarr.lang", "en"));
  await page.goto("/");
  await page.waitForURL(/\/(setup|login|movies)/);
  if (page.url().includes("/setup")) {
    await page.getByLabel("Setup code").fill(setupCode);
    await page.getByLabel("Administrator username").fill(adminUser);
    await page.getByLabel("Password", { exact: true }).fill(adminPassword);
    await page.getByLabel("Repeat password").fill(adminPassword);
    await page.getByRole("button", { name: "Create administrator" }).click();
  } else if (page.url().includes("/login")) {
    await page.getByLabel("Username").fill(adminUser);
    await page.getByLabel("Password").fill(adminPassword);
    await page.getByRole("button", { name: "Sign in" }).click();
  }
  await expect(page.getByTestId("current-username")).toHaveText(adminUser);
}

async function prepare(page: Page) {
  const ok = (status: number) => [200, 201, 409].includes(status);
  const key = await page.request.put("/api/v1/settings/metadata", { data: { tmdbApiKey: tmdbKey } });
  expect(key.ok(), await key.text()).toBeTruthy();
  const ix = await page.request.post("/api/v1/indexers", { data: { name: "Fake series indexer", kind: "torznab", baseUrl: indexerUrl, apiKey: "fakekey" } });
  expect(ok(ix.status()), await ix.text()).toBeTruthy();
  const clients = await (await page.request.get("/api/v1/downloadclients")).json();
  if (!clients.length) {
    const cl = await page.request.post("/api/v1/downloadclients", { data: { name: "Transmission", implementation: "transmission",
      host: tmHost, port: 9091, username: tmUser, password: tmPass, categories: { movies: "locarr-movies", series: "", music: "" } } });
    expect(ok(cl.status()), await cl.text()).toBeTruthy();
  }
  const rf = await page.request.post("/api/v1/rootfolders", { data: { path: tvRoot, kind: "series" } });
  expect(ok(rf.status()), await rf.text()).toBeTruthy();
  // Releases left in the client by an earlier run would be "already queued".
  const queue = await (await page.request.get("/api/v1/queue")).json();
  for (const it of queue.items ?? []) if (/Cosmos|Elephants/.test(it.title)) await page.request.delete(`/api/v1/queue/${it.id}`);
  for (const q of ["Cosmos", "Elephants"]) {
    const list = await (await page.request.get(`/api/v1/series?q=${q}`)).json();
    for (const s of list.items ?? []) await page.request.delete(`/api/v1/series/${s.id}?deleteFiles=true`);
  }
}

test("add a series: season pack grabbed, every episode imported, episode timeline and the calendar", async ({ page }) => {
  test.skip(tmHost === "none", "no Transmission instance for this run");
  test.setTimeout(240_000);
  await signInAsAdmin(page);
  await prepare(page);

  await page.getByRole("link", { name: "Series" }).first().click();
  await page.getByTestId("add-series").click();
  await page.getByTestId("add-series-term").fill("Cosmos Laundromat");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await page.getByTestId("pick-800434").click();
  await page.getByLabel("Root folder").selectOption({ label: tvRoot });
  await page.getByLabel("Quality profile").selectOption({ label: "HD-720p/1080p" });
  await page.getByTestId("monitor-option").selectOption("all");
  await page.getByLabel("Search now").check();
  await page.getByTestId("add-series-confirm").click();
  await expect(page.getByTestId("series-title")).toHaveText("Cosmos Laundromat");

  // The season pack covers all three episodes; each is imported (hardlinked) and renamed.
  const season = page.getByTestId("season-1");
  for (const code of ["S01E01", "S01E02", "S01E03"]) {
    await expect(season.locator(`[data-code="${code}"]`)).toContainText("Below cutoff", { timeout: 200_000 });
  }
  await expect(season.locator('[data-code="S01E02"]')).toContainText("WEBDL-720p");
  await expect(page.getByTestId("season-0")).toContainText("Not monitored");

  // The episode's history: the season search, the pack grab and its import.
  await season.locator('[data-code="S01E02"]').getByTestId("episode-history").click();
  const drawer = page.getByTestId("drawer-history");
  await expect(drawer.getByTestId("why-box")).toHaveAttribute("data-code", "upgrade_wanted");
  for (const type of ["search", "decision", "download_grabbed", "download_completed", "file_imported", "renamed"]) {
    await expect(drawer.locator(`[data-type="${type}"]`).first()).toBeVisible();
  }
  await expect(drawer.getByTestId("timeline")).toContainText("Cosmos.Laundromat.S01.720p.WEB-DL.AAC2.0.x264-LOCARR");
  await page.keyboard.press("Escape");

  // A continuing series puts its coming episodes on the calendar.
  const added = await page.request.post("/api/v1/series", { data: { tmdbId: 800424, rootFolderId: (await (await page.request.get("/api/v1/rootfolders")).json())
    .find((r: { kind: string; path: string }) => r.kind === "series" && r.path === tvRoot).id,
    qualityProfileId: (await (await page.request.get("/api/v1/qualityprofiles")).json()).find((p: { kind: string }) => p.kind === "series").id,
    monitor: "future", searchOnAdd: false } });
  expect(added.status(), await added.text()).toBe(201);
  await page.goto("/calendar");
  await page.getByRole("button", { name: "Agenda" }).click();
  await page.getByRole("button", { name: "Next" }).click();
  await page.getByRole("button", { name: "Today" }).click();
  await expect(page.locator('[data-testid="calendar-entry"][data-kind="episode"]').filter({ hasText: "Elephants Dream" }).first()).toBeVisible();

  await page.goto("/series");
  await expect(page.getByTestId("series-card").filter({ hasText: "Cosmos Laundromat" })).toContainText("3/3");
});
