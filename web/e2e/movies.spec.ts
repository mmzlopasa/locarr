import { expect, test, type Page } from "@playwright/test";

// Needs the fake services (scripts/dev-fakes.sh with LOCARR_FAKES_SEED_DIR), Locarr started with
// LOCARR_TMDB_URL pointing at the fake TMDb, and a Transmission that saves into the seed folder
// under the same /data path (see the e2e job in .github/workflows/ci.yml).
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

/** Creates what the flow needs through the API (each call may find it already there). */
async function prepare(page: Page) {
  const ok = (status: number) => [200, 201, 409].includes(status);
  const ix = await page.request.post("/api/v1/indexers", { data: { name: "Fake movies indexer", kind: "torznab", baseUrl: indexerUrl, apiKey: "fakekey" } });
  expect(ok(ix.status()), await ix.text()).toBeTruthy();
  const cl = await page.request.post("/api/v1/downloadclients", { data: { name: "Transmission movies", implementation: "transmission",
    host: tmHost, port: 9091, username: tmUser, password: tmPass, categories: { movies: "locarr-movies", series: "", music: "" } } });
  expect(ok(cl.status()), await cl.text()).toBeTruthy();
  const rf = await page.request.post("/api/v1/rootfolders", { data: { path: moviesRoot, kind: "movies" } });
  expect(ok(rf.status()), await rf.text()).toBeTruthy();
  // A movie left from an earlier run goes first (to the recycle bin with its files).
  const list = await (await page.request.get("/api/v1/movies?q=Big%20Buck")).json();
  for (const m of list.items ?? []) await page.request.delete(`/api/v1/movies/${m.id}?deleteFiles=true`);
}

test("add a movie: TMDb search, automatic search and grab, download, import and the timeline", async ({ page }) => {
  test.skip(tmHost === "none", "no Transmission instance for this run");
  test.setTimeout(180_000);
  await signInAsAdmin(page);

  await page.goto("/settings/metadata");
  await page.getByTestId("tmdb-key").fill(tmdbKey);
  await page.getByTestId("save-metadata").click();
  await expect(page.getByText("Key set", { exact: true })).toBeVisible();
  await prepare(page);

  await page.getByRole("link", { name: "Movies" }).first().click();
  await page.getByTestId("add-movie").click();
  await page.getByTestId("add-movie-term").fill("Big Buck Bunny");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await page.getByTestId("pick-10378").click();
  await page.getByLabel("Root folder").selectOption({ label: moviesRoot });
  await page.getByLabel("Quality profile").selectOption({ label: "HD-720p/1080p" });
  await page.getByLabel("Search now").check();
  await page.getByTestId("add-movie-confirm").click();
  await expect(page.getByTestId("movie-title")).toHaveText("Big Buck Bunny");

  // The history tab shows each step; the explainer turns to "available" once imported.
  await page.getByTestId("tab-history").click();
  await expect(page.getByTestId("why-box")).toHaveAttribute("data-code", "available", { timeout: 150_000 });
  const timeline = page.getByTestId("timeline");
  for (const type of ["added", "search", "decision", "download_grabbed", "download_completed", "file_imported", "renamed"]) {
    await expect(timeline.locator(`[data-type="${type}"]`).first()).toBeVisible();
  }
  await expect(timeline).toContainText("Imported as Bluray-1080p");

  await page.getByTestId("tab-overview").click();
  await expect(page.getByTestId("movie-file")).toContainText("Big Buck Bunny (2008) [Bluray-1080p][x264]-LOCARR.mkv");

  // Interactive search lists the release with its decision.
  await page.getByTestId("tab-search").click();
  await page.getByTestId("interactive-search").click();
  await expect(page.getByTestId("decision-row").first()).toBeVisible({ timeout: 60_000 });

  await page.goto("/movies");
  const card = page.getByTestId("movie-card").filter({ hasText: "Big Buck Bunny" });
  await expect(card).toContainText("Downloaded");
});
