import { expect, test, type Page } from "@playwright/test";

// Needs the same stack as movies.spec.ts (fakes with LOCARR_FAKES_SEED_DIR and a Transmission) plus the
// fake Streamero the fakes serve under /streamero (internal/mediaserver/streamero/fake: admin key
// sk_fakeadminkey, users admin / ana / tom with password streamero-pass, library "Movies" at /media/movies).
const setupCode = process.env.E2E_SETUP_CODE ?? "E2E-SETUP";
const adminUser = process.env.E2E_ADMIN_USER ?? "admin";
const adminPassword = process.env.E2E_ADMIN_PASSWORD ?? "e2e-password-123";
const tmHost = process.env.E2E_TRANSMISSION_HOST ?? "transmission";
const tmUser = process.env.E2E_TRANSMISSION_USER ?? "locarr";
const tmPass = process.env.E2E_TRANSMISSION_PASS ?? "locarr-e2e";
const tmdbKey = process.env.E2E_TMDB_KEY ?? "fake0000000000000000000000tmdb01";
const indexerUrl = process.env.E2E_INDEXER_URL ?? "http://fake-indexer:9117/torznab";
const moviesRoot = process.env.E2E_MOVIES_ROOT ?? "/data/media/movies";
const streameroUrl = process.env.E2E_STREAMERO_URL ?? "http://fake-indexer:9117/streamero";
const streameroKey = "sk_fakeadminkey";

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
  test.skip(tmHost === "none", "no Transmission instance for this run");
  test.setTimeout(240_000);
  await signInAsAdmin(page);
  const ok = (status: number) => [200, 201, 409].includes(status);
  expect((await page.request.put("/api/v1/settings/metadata", { data: { tmdbApiKey: tmdbKey } })).ok()).toBeTruthy();
  for (const [path, data] of [
    ["/api/v1/indexers", { name: "Fake Streamero-test indexer", kind: "torznab", baseUrl: indexerUrl, apiKey: "fakekey" }],
    ["/api/v1/downloadclients", { name: "Transmission Streamero", implementation: "transmission", host: tmHost, port: 9091, username: tmUser,
      password: tmPass, categories: { movies: "locarr-movies", series: "", music: "" } }],
    ["/api/v1/rootfolders", { path: moviesRoot, kind: "movies" }],
  ] as const) {
    const r = await page.request.post(path, { data });
    expect(ok(r.status()), await r.text()).toBeTruthy();
  }

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

  // Import a movie: Locarr asks Streamero to scan the Movies library (and nothing else).
  const list = await (await page.request.get("/api/v1/movies?q=Big%20Buck")).json();
  for (const m of list.items ?? []) await page.request.delete(`/api/v1/movies/${m.id}?deleteFiles=true`);
  const roots = await (await page.request.get("/api/v1/rootfolders")).json();
  const root = roots.find((r: { path: string }) => r.path === moviesRoot);
  const profiles = await (await page.request.get("/api/v1/qualityprofiles")).json();
  const profile = profiles.find((p: { name: string; kind: string }) => p.kind === "movies" && p.name === "HD-720p/1080p");
  const added = await page.request.post("/api/v1/movies", { data: { tmdbId: 10378, rootFolderId: root.id, qualityProfileId: profile.id,
    minimumAvailability: "released", searchOnAdd: true } });
  expect(added.status(), await added.text()).toBe(201);
  const movie = await added.json();
  await expect.poll(async () => {
    const tl = await (await page.request.get(`/api/v1/timeline?item=movie:${movie.id}`)).json();
    return (tl.events ?? []).filter((e: { type: string }) => e.type === "notified").map((e: { message: string }) => e.message).join("|");
  }, { timeout: 200_000, intervals: [2000] }).toContain("notified Streamero: scanning Movies");
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
