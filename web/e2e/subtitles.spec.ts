import { expect, test, type Page } from "@playwright/test";

// Needs what movies.spec.ts needs (fakes with a seed folder, Transmission, the fake TMDb) plus Locarr started
// with LOCARR_OPENSUBTITLES_URL and LOCARR_SUBDL_URL pointing at the fake providers (see the e2e job in ci.yml).
const setupCode = process.env.E2E_SETUP_CODE ?? "E2E-SETUP";
const adminUser = process.env.E2E_ADMIN_USER ?? "admin";
const adminPassword = process.env.E2E_ADMIN_PASSWORD ?? "e2e-password-123";
const tmHost = process.env.E2E_TRANSMISSION_HOST ?? "transmission";
const tmUser = process.env.E2E_TRANSMISSION_USER ?? "locarr";
const tmPass = process.env.E2E_TRANSMISSION_PASS ?? "locarr-e2e";
const tmdbKey = process.env.E2E_TMDB_KEY ?? "fake0000000000000000000000tmdb01";
const indexerUrl = process.env.E2E_INDEXER_URL ?? "http://fake-indexer:9117/torznab";
const moviesRoot = process.env.E2E_MOVIES_ROOT ?? "/data/media/movies";

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

async function prepare(page: Page) {
  const ok = (status: number) => [200, 201, 409].includes(status);
  const meta = await page.request.put("/api/v1/settings/metadata", { data: { tmdbApiKey: tmdbKey } });
  expect(meta.status(), await meta.text()).toBe(200);
  const ix = await page.request.post("/api/v1/indexers", { data: { name: "Fake movies indexer", kind: "torznab", baseUrl: indexerUrl, apiKey: "fakekey" } });
  expect(ok(ix.status()), await ix.text()).toBeTruthy();
  const cl = await page.request.post("/api/v1/downloadclients", { data: { name: "Transmission movies", implementation: "transmission",
    host: tmHost, port: 9091, username: tmUser, password: tmPass, categories: { movies: "locarr-movies", series: "", music: "" } } });
  expect(ok(cl.status()), await cl.text()).toBeTruthy();
  const rf = await page.request.post("/api/v1/rootfolders", { data: { path: moviesRoot, kind: "movies" } });
  expect(ok(rf.status()), await rf.text()).toBeTruthy();
  const list = await (await page.request.get("/api/v1/movies?q=Big%20Buck")).json();
  for (const m of list.items ?? []) await page.request.delete(`/api/v1/movies/${m.id}?deleteFiles=true`);
}

test("subtitles: imported movie gets its Croatian subtitle, a manual pick replaces it, the shift tool re-times it", async ({ page }) => {
  test.skip(tmHost === "none", "no Transmission instance for this run");
  test.setTimeout(240_000);
  await signInAsAdmin(page);
  await prepare(page);

  // Settings → Subtitles: providers (keys of the fakes) and a Croatian profile as the movie default.
  await page.goto("/settings/subtitles");
  const os = page.locator('[data-testid="subtitle-provider"][data-name="opensubtitles"]');
  await os.getByTestId("provider-key-opensubtitles").fill("fakeoskey");
  await os.getByLabel("Enabled").check();
  await os.getByTestId("provider-save-opensubtitles").click();
  await expect(os.getByText("Working")).toBeVisible();
  const sd = page.locator('[data-testid="subtitle-provider"][data-name="subdl"]');
  await sd.getByTestId("provider-key-subdl").fill("fakesubdlkey");
  await sd.getByLabel("Enabled").check();
  await sd.getByTestId("provider-save-subdl").click();
  await expect(sd.getByText("Working")).toBeVisible();

  const profiles = await (await page.request.get("/api/v1/subtitles/profiles")).json();
  if (!profiles.some((p: { name: string }) => p.name === "Hrvatski")) {
    await page.getByTestId("add-subtitle-profile").click();
    await page.getByTestId("profile-name").fill("Hrvatski");
    await page.getByTestId("profile-language").first().getByLabel("Language").selectOption("hr");
    await page.getByTestId("profile-save").click();
    await expect(page.getByTestId("subtitle-profile").filter({ hasText: "Hrvatski" })).toBeVisible();
  }
  const hrvatski = page.getByTestId("default-movies").locator("option", { hasText: "Hrvatski" });
  await page.getByTestId("default-movies").selectOption({ value: (await hrvatski.getAttribute("value")) ?? "" });
  await page.getByTestId("save-subtitle-settings").click();
  await expect(page.getByText("Saved.")).toBeVisible();

  // Add the movie with a search: grab → Transmission → import → subtitle search.
  await page.goto("/movies/add");
  await page.getByTestId("add-movie-term").fill("Big Buck Bunny");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await page.getByTestId("pick-10378").click();
  await page.getByLabel("Root folder").selectOption({ label: moviesRoot });
  await page.getByLabel("Quality profile").selectOption({ label: "HD-720p/1080p" });
  await page.getByLabel("Search now").check();
  await page.getByTestId("add-movie-confirm").click();
  await expect(page.getByTestId("movie-title")).toHaveText("Big Buck Bunny");

  await page.getByTestId("tab-subtitles").click();
  const hr = page.locator('[data-testid="subtitle-want"][data-key="hr"]');
  await expect(async () => {
    await page.getByTestId("tab-history").click();
    await page.getByTestId("tab-subtitles").click();
    await expect(hr).toHaveAttribute("data-status", "present", { timeout: 2_000 });
  }).toPass({ timeout: 180_000 });
  const row = page.locator('[data-testid="subtitle-row"][data-language="hr"]');
  await expect(row).toContainText("from opensubtitles, score 90");

  // The timeline says what happened.
  await page.getByTestId("tab-history").click();
  await expect(page.getByTestId("timeline").locator('[data-type="subtitle_downloaded"]').first())
    .toContainText("Subtitle Croatian found on opensubtitles, score 90");
  await expect(page.getByTestId("subtitle-why")).toHaveAttribute("data-code", "complete");

  // Manual search: pick SubDL's subtitle instead.
  await page.getByTestId("tab-subtitles").click();
  await page.getByTestId("subtitles-manual").click();
  const subdl = page.locator('[data-testid="subtitle-result"][data-provider="subdl"]').first();
  await expect(subdl).toBeVisible({ timeout: 30_000 });
  await subdl.getByTestId("result-download").click();
  await expect(row).toContainText("from subdl", { timeout: 15_000 });

  // Shift tool: 1.5 s later; the preview shows the new time of the first line (it starts at 2 s).
  await row.getByTestId("subtitle-tool-shift").click();
  await row.getByTestId("shift-ms").fill("1500");
  await row.getByTestId("shift-later").click();
  await expect(row.getByText("Shifted.")).toBeVisible();
  await row.getByTestId("subtitle-tool-preview").click();
  await expect(row.getByTestId("preview-start").first()).toHaveText("00:03.500");

  // Sync is offered against the audio (the image ships ffmpeg).
  await row.getByTestId("subtitle-tool-sync").click();
  await expect(row.getByTestId("sync-reference")).toBeVisible();
});
