import { expect, test, type Page } from "@playwright/test";

// Needs the CI compose stack (fake TMDb and indexer, see .github/workflows/ci.yml). Two automatic searches of a
// movie whose profile refuses every release: the item timeline shows one row per search cycle with its decision,
// a cycle is deleted as a group, and the rest is cleared after a confirmation that shows the count (D-109).
const setupCode = process.env.E2E_SETUP_CODE ?? "E2E-SETUP";
const adminUser = process.env.E2E_ADMIN_USER ?? "admin";
const adminPassword = process.env.E2E_ADMIN_PASSWORD ?? "e2e-password-123";
const tmdbKey = process.env.E2E_TMDB_KEY ?? "fake0000000000000000000000tmdb01";
const indexerUrl = process.env.E2E_INDEXER_URL ?? "http://fake-indexer:9117/torznab";
const moviesRoot = process.env.E2E_MOVIES_ROOT ?? "/data/media/movies";
const openHorizon = 900001;

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

test("item timeline: one row per search cycle, delete a cycle, clear the item's history", async ({ page }) => {
  test.setTimeout(120_000);
  await signInAsAdmin(page);
  const ok = (status: number) => [200, 201, 409].includes(status);
  expect((await page.request.put("/api/v1/settings/metadata", { data: { tmdbApiKey: tmdbKey } })).ok()).toBeTruthy();
  if ((await (await page.request.get("/api/v1/indexers")).json()).length === 0) {
    const ix = await page.request.post("/api/v1/indexers", { data: { name: "Fake movies indexer", kind: "torznab", baseUrl: indexerUrl, apiKey: "fakekey" } });
    expect(ok(ix.status()), await ix.text()).toBeTruthy();
  }
  const rf = await page.request.post("/api/v1/rootfolders", { data: { path: moviesRoot, kind: "movies" } });
  expect(ok(rf.status()), await rf.text()).toBeTruthy();
  const leftovers = await (await page.request.get("/api/v1/movies?q=Open%20Horizon")).json();
  for (const m of leftovers.items ?? []) await page.request.delete(`/api/v1/movies/${m.id}`);

  const root = (await (await page.request.get("/api/v1/rootfolders")).json()).find((r: { path: string }) => r.path === moviesRoot);
  // UHD refuses the fake 1080p releases, so nothing is grabbed and every cycle ends in a decision.
  const profile = (await (await page.request.get("/api/v1/qualityprofiles")).json())
    .find((p: { name: string; kind: string }) => p.kind === "movies" && p.name === "UHD");
  const added = await page.request.post("/api/v1/movies", { data: { tmdbId: openHorizon, rootFolderId: root.id, qualityProfileId: profile.id,
    minimumAvailability: "released", searchOnAdd: false } });
  expect(added.status(), await added.text()).toBe(201);
  const movie = await added.json();
  const decisions = async () => ((await (await page.request.get(`/api/v1/timeline?item=movie:${movie.id}`)).json()).events as { type: string }[])
    .filter((e) => e.type === "decision").length;
  for (const n of [1, 2]) {
    expect((await page.request.post(`/api/v1/movies/${movie.id}/search`)).status()).toBe(202);
    await expect.poll(decisions, { timeout: 30_000 }).toBe(n);
  }

  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`/movies/${movie.id}`);
  const cycles = page.getByTestId("timeline-cycle");
  await expect(cycles).toHaveCount(2);
  const first = cycles.first();
  await expect(first).toContainText("Automatic search — results:");
  await expect(first.getByTestId("timeline-cycle-decision")).toContainText("Decision (manual)");
  // No separate search or decision rows are left over.
  await expect(page.locator('[data-testid="timeline-event"][data-type="search"]')).toHaveCount(0);
  await expect(page.locator('[data-testid="timeline-event"][data-type="decision"]')).toHaveCount(0);
  // Phone width: nothing scrolls sideways.
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBeTruthy();
  await first.getByRole("button", { name: "Show the queries and the rejected releases" }).click();
  await expect(first.getByTestId("timeline-cycle-detail")).toContainText("Queries, in the order tried");

  // Delete one cycle as a group.
  await first.locator('[data-testid^="timeline-delete-"]').first().click();
  await first.locator('[data-testid$="-confirm"]').click();
  await expect(cycles).toHaveCount(1);

  // Clear the rest after a confirmation with the count.
  await page.getByTestId("timeline-clear").click();
  const dialog = page.getByTestId("timeline-clear-dialog");
  await expect(dialog.getByTestId("timeline-clear-count")).toContainText(/Rows to delete: \d+/);
  await expect(dialog).toContainText("Kept: the files, the blocklist");
  await dialog.getByTestId("timeline-clear-confirm").click();
  await expect(dialog).toBeHidden();
  await expect(page.getByText("Nothing has happened yet.")).toBeVisible();

  // The movie itself is untouched.
  expect((await page.request.get(`/api/v1/movies/${movie.id}`)).status()).toBe(200);
  await page.request.delete(`/api/v1/movies/${movie.id}`);
});
