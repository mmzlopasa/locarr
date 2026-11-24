import { expect, test, type Page } from "@playwright/test";

// Needs the CI compose stack (fake TMDb and indexer, see .github/workflows/ci.yml). Three grabs of one movie
// fail the same way; the history folds them into one row, expands it and clears the failures after a confirmation.
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

test("history: repeated failures fold into one row, expand, and are cleared after a confirmation", async ({ page }) => {
  test.setTimeout(120_000);
  await signInAsAdmin(page);
  const ok = (status: number) => [200, 201, 409].includes(status);
  expect((await page.request.put("/api/v1/settings/metadata", { data: { tmdbApiKey: tmdbKey } })).ok()).toBeTruthy();
  const ix = await page.request.post("/api/v1/indexers", { data: { name: "Fake movies indexer", kind: "torznab", baseUrl: indexerUrl, apiKey: "fakekey" } });
  expect(ok(ix.status()), await ix.text()).toBeTruthy();
  const rf = await page.request.post("/api/v1/rootfolders", { data: { path: moviesRoot, kind: "movies" } });
  expect(ok(rf.status()), await rf.text()).toBeTruthy();
  const leftovers = await (await page.request.get("/api/v1/movies?q=Open%20Horizon")).json();
  for (const m of leftovers.items ?? []) await page.request.delete(`/api/v1/movies/${m.id}`);

  const root = (await (await page.request.get("/api/v1/rootfolders")).json()).find((r: { path: string }) => r.path === moviesRoot);
  const profile = (await (await page.request.get("/api/v1/qualityprofiles")).json())
    .find((p: { name: string; kind: string }) => p.kind === "movies" && p.name === "Any");
  const indexer = (await (await page.request.get("/api/v1/indexers")).json())[0];
  const added = await page.request.post("/api/v1/movies", { data: { tmdbId: openHorizon, rootFolderId: root.id, qualityProfileId: profile.id,
    minimumAvailability: "released", searchOnAdd: false } });
  expect(added.status(), await added.text()).toBe(201);
  const movie = await added.json();

  // No usenet client exists, so each grab fails the same way.
  for (const n of [1, 2, 3]) {
    const grab = await page.request.post(`/api/v1/movies/${movie.id}/releases`, { data: { indexerId: indexer.id,
      magnetUrl: `magnet:?xt=urn:btih:${String(n).repeat(40)}`, title: `The.Open.Horizon.2027.1080p.WEB-E2E${n}`, protocol: "usenet" } });
    expect(grab.ok()).toBeFalsy();
  }

  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/activity/history");
  await page.getByTestId("history-search").fill("Open Horizon");
  await page.getByTestId("history-category").selectOption("failed");
  const group = page.getByTestId("history-group").first();
  await expect(group).toContainText("The Open Horizon (2027)");
  await expect(group).toContainText("3×");
  await expect(group.getByTestId("history-reason")).toContainText("Could not grab 3 releases");
  // Phone width: nothing scrolls sideways.
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBeTruthy();

  await group.getByTestId("history-toggle").click();
  const items = group.getByTestId("history-group-list").locator("li");
  await expect(items).toHaveCount(3);
  await expect(items.first()).toContainText("The.Open.Horizon.2027.1080p.WEB-E2E3");

  await page.getByTestId("history-clear").click();
  const dialog = page.getByTestId("history-clear-dialog");
  await dialog.getByTestId("history-clear-failed").check();
  await expect(dialog.getByTestId("history-clear-count")).toContainText("Entries to delete: 3");
  await dialog.getByTestId("history-clear-confirm").click();
  await expect(dialog).toBeHidden();
  await expect(page.getByTestId("history-empty")).toBeVisible();

  // The movie itself is untouched.
  expect((await page.request.get(`/api/v1/movies/${movie.id}`)).status()).toBe(200);
  await page.request.delete(`/api/v1/movies/${movie.id}`);
});
