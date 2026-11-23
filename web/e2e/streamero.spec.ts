import fs from "node:fs";
import path from "node:path";
import { expect, test, type Page } from "@playwright/test";

// Needs the compose stack and the fakes (scripts/dev-fakes.sh) with the fake Streamero the fakes serve under /streamero (internal/mediaserver/streamero/fake: admin key
// sk_fakeadminkey, users admin / ana / tom with password streamero-pass, library "Movies" at /media/movies).
const setupCode = process.env.E2E_SETUP_CODE ?? "E2E-SETUP";
const adminUser = process.env.E2E_ADMIN_USER ?? "admin";
const adminPassword = process.env.E2E_ADMIN_PASSWORD ?? "e2e-password-123";
const tmdbKey = process.env.E2E_TMDB_KEY ?? "fake0000000000000000000000tmdb01";
const moviesRoot = process.env.E2E_MOVIES_ROOT ?? "/data/media/movies";
const streameroUrl = process.env.E2E_STREAMERO_URL ?? "http://fake-indexer:9117/streamero";
const streameroKey = "sk_fakeadminkey";
// The host folder mounted as /data in the Locarr container (the test writes a movie file there).
const dataDir = process.env.E2E_DATA_DIR ?? "";

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

test("Streamero: settings, a scan of the right library after an import, titles it has, sign-in as a requester", async ({ page, browser }) => {
  test.skip(!dataDir, "E2E_DATA_DIR (the host folder mounted as /data) is not set");
  test.setTimeout(180_000);
  await signInAsAdmin(page);
  const ok = (status: number) => [200, 201, 409].includes(status);
  expect((await page.request.put("/api/v1/settings/metadata", { data: { tmdbApiKey: tmdbKey } })).ok()).toBeTruthy();
  const rf = await page.request.post("/api/v1/rootfolders", { data: { path: moviesRoot, kind: "movies" } });
  expect(ok(rf.status()), await rf.text()).toBeTruthy();

  // Settings → Media server: Test shows the libraries, then save with a path mapping and sign-in.
  await page.goto("/settings/mediaserver");
  await page.getByTestId("streamero-url").fill(streameroUrl);
  await page.getByTestId("secret-streamero-key").fill(streameroKey);
  await page.getByTestId("streamero-test").click();
  const result = page.getByTestId("streamero-test-result");
  await expect(result).toContainText("Fake Streamero");
  await expect(result).toContainText("Movies");
  await page.getByTestId("add-path-mapping").click();
  const mapping = page.getByTestId("path-mapping-0");
  await mapping.getByRole("textbox").nth(0).fill("/data/media");
  await mapping.getByRole("textbox").nth(1).fill("/media");
  await page.getByTestId("streamero-enabled").check();
  await page.getByTestId("signin-enabled").check();
  await page.getByTestId("signin-autocreate").check();
  await page.getByTestId("save-streamero").click();
  await expect(page.getByTestId("streamero-status")).toBeVisible();
  const saved = await (await page.request.get("/api/v1/mediaserver/streamero")).json();
  expect(saved.enabled && saved.apiKey.configured && saved.signIn.autoCreate).toBeTruthy();

  // Import a movie (manual import of a file the test writes into the shared data folder): Locarr asks
  // Streamero to scan the Movies library, and nothing else.
  const release = "Sintel.2010.1080p.BluRay.x264-LOCARR";
  const hostDir = path.join(dataDir, "downloads", "streamero-e2e", release);
  fs.mkdirSync(hostDir, { recursive: true });
  fs.writeFileSync(path.join(hostDir, `${release}.mkv`), Buffer.alloc(2 * 1024 * 1024, 7));
  const leftovers = await (await page.request.get("/api/v1/movies?q=Sintel")).json();
  for (const m of leftovers.items ?? []) await page.request.delete(`/api/v1/movies/${m.id}?deleteFiles=true`);
  const roots = await (await page.request.get("/api/v1/rootfolders")).json();
  const root = roots.find((r: { path: string }) => r.path === moviesRoot);
  const profiles = await (await page.request.get("/api/v1/qualityprofiles")).json();
  const profile = profiles.find((p: { name: string; kind: string }) => p.kind === "movies" && p.name === "HD-720p/1080p");
  const added = await page.request.post("/api/v1/movies", { data: { tmdbId: 45745, rootFolderId: root.id, qualityProfileId: profile.id,
    minimumAvailability: "released", searchOnAdd: false } });
  expect(added.status(), await added.text()).toBe(201);
  const movie = await added.json();
  const imported = await page.request.post("/api/v1/movies/import/manual", { data: { items: [{
    path: `/data/downloads/streamero-e2e/${release}/${release}.mkv`, movieId: movie.id, mode: "copy" }] } });
  expect(imported.status(), await imported.text()).toBe(200);
  const results = await imported.json();
  expect(results[0].error, JSON.stringify(results[0])).toBeFalsy();
  await expect.poll(async () => {
    const tl = await (await page.request.get(`/api/v1/timeline?item=movie:${movie.id}`)).json();
    return (tl.events ?? []).filter((e: { type: string }) => e.type === "notified").map((e: { message: string }) => e.message).join("|");
  }, { timeout: 90_000, intervals: [2000] }).toContain("notified Streamero: scanning Movies");
  await page.goto(`/movies/${movie.id}`);
  await page.getByTestId("tab-history").click();
  await expect(page.getByTestId("timeline")).toContainText("notified Streamero: scanning Movies");
  const after = await (await page.request.get("/api/v1/mediaserver/streamero")).json();
  expect(after.status.lastScanAt).toBeTruthy();
  expect(after.status.pendingChanges).toBe(0);

  // The index knows Sintel and Cosmos Laundromat.
  expect((await page.request.post("/api/v1/mediaserver/streamero/sync")).status()).toBe(202);
  await expect.poll(async () => (await (await page.request.get("/api/v1/mediaserver/streamero")).json()).status.indexedItems,
    { timeout: 30_000 }).toBeGreaterThanOrEqual(2);

  // Sign in with a Streamero account: a requester who sees that Sintel is already watchable.
  const ctx = await browser.newContext();
  const guest = await ctx.newPage();
  await guest.addInitScript(() => localStorage.setItem("locarr.lang", "en"));
  await guest.goto("/login");
  await guest.getByTestId("provider-streamero").click();
  await guest.getByLabel("Username").fill("ana");
  await guest.getByLabel("Password").fill("streamero-pass");
  await guest.getByRole("button", { name: "Sign in" }).click();
  await guest.waitForURL(/\/discover/);
  const me = await (await guest.request.get("/api/v1/auth/me")).json();
  expect(me.role).toBe("requester");
  await guest.goto("/discover/movie/45745");
  await expect(guest.getByTestId("watch-in-streamero")).toBeVisible();
  await expect(guest.getByTestId("watch-in-streamero")).toHaveAttribute("href", /\/item\//);
  await ctx.close();
});
