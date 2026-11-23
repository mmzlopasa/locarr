import { expect, test, type APIRequestContext, type Page } from "@playwright/test";

// A family member joins with an invite link, requests a movie, an administrator approves it, the fakes
// and Transmission bring it in, and the family member sees it as available with a notification.
// Needs the same stack as e2e/movies.spec.ts (fake services, fake TMDb, Transmission).
const setupCode = process.env.E2E_SETUP_CODE ?? "E2E-SETUP";
const adminUser = process.env.E2E_ADMIN_USER ?? "admin";
const adminPassword = process.env.E2E_ADMIN_PASSWORD ?? "e2e-password-123";
const tmHost = process.env.E2E_TRANSMISSION_HOST ?? "transmission";
const tmUser = process.env.E2E_TRANSMISSION_USER ?? "locarr";
const tmPass = process.env.E2E_TRANSMISSION_PASS ?? "locarr-e2e";
const tmdbKey = process.env.E2E_TMDB_KEY ?? "fake0000000000000000000000tmdb01";
const indexerUrl = process.env.E2E_INDEXER_URL ?? "http://fake-indexer:9117/torznab";
const moviesRoot = process.env.E2E_MOVIES_ROOT ?? "/data/media/movies";
const tearsOfSteel = 133701;

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

/** Creates what the flow needs through the API (each call may find it already there) and clears earlier runs. */
async function prepare(api: APIRequestContext) {
  const ok = (status: number) => [200, 201, 409].includes(status);
  const key = await api.put("/api/v1/settings/metadata", { data: { tmdbApiKey: tmdbKey } });
  expect(key.ok(), await key.text()).toBeTruthy();
  const ix = await api.post("/api/v1/indexers", { data: { name: "Fake requests indexer", kind: "torznab", baseUrl: indexerUrl, apiKey: "fakekey" } });
  expect(ok(ix.status()), await ix.text()).toBeTruthy();
  const cl = await api.post("/api/v1/downloadclients", { data: { name: "Transmission requests", implementation: "transmission",
    host: tmHost, port: 9091, username: tmUser, password: tmPass, categories: { movies: "locarr-movies", series: "", music: "" } } });
  expect(ok(cl.status()), await cl.text()).toBeTruthy();
  const rf = await api.post("/api/v1/rootfolders", { data: { path: moviesRoot, kind: "movies" } });
  expect(ok(rf.status()), await rf.text()).toBeTruthy();

  const roots: { id: number; path: string; kind: string }[] = await (await api.get("/api/v1/rootfolders")).json();
  const root = roots.find((r) => r.kind === "movies" && r.path === moviesRoot);
  const profiles: { id: number; name: string; kind: string }[] = await (await api.get("/api/v1/qualityprofiles")).json();
  const profile = profiles.find((p) => p.kind === "movies" && p.name === "HD-720p/1080p");
  expect(root && profile).toBeTruthy();
  const settings = await (await api.get("/api/v1/settings/requests")).json();
  settings.movie = { ...settings.movie, qualityProfileId: profile!.id, rootFolderId: root!.id, minimumAvailability: "released" };
  const put = await api.put("/api/v1/settings/requests", { data: settings });
  expect(put.ok(), await put.text()).toBeTruthy();

  // A Tears of Steel left from an earlier run goes first (to the recycle bin with its files), and so do open requests for it.
  const list = await (await api.get("/api/v1/movies?q=Tears%20of%20Steel")).json();
  for (const m of list.items ?? []) await api.delete(`/api/v1/movies/${m.id}?deleteFiles=true`);
  const open = await (await api.get("/api/v1/requests?status=active&limit=200")).json();
  for (const r of open.items ?? []) {
    if (r.kind === "movie" && r.tmdbId === tearsOfSteel) {
      await api.post(`/api/v1/requests/${r.id}/decline`, { data: { reason: "e2e cleanup" } });
    }
  }
}

test("invite sign-up, request, approval, arrival and notification", async ({ page, browser }) => {
  test.skip(tmHost === "none", "no Transmission instance for this run");
  test.setTimeout(240_000);
  await signInAsAdmin(page);
  await prepare(page.request);

  // The administrator makes an invite link on the Users page.
  await page.goto("/settings/users");
  await page.getByTestId("invite-new").click();
  await page.getByTestId("invite-label").fill("Family");
  await page.getByTestId("invite-create").click();
  const link = (await page.getByTestId("invite-link").textContent())?.trim() ?? "";
  expect(link).toMatch(/\/join#.+/);

  // A family member opens it in their own browser and makes an account.
  const family = await browser.newContext();
  const member = await family.newPage();
  await member.addInitScript(() => localStorage.setItem("locarr.lang", "en"));
  const username = `family-${Date.now().toString(36)}`;
  await member.goto(link.replace(/^https?:\/\/[^/]+/, ""));
  await expect(member.getByTestId("join-form")).toBeVisible();
  await member.getByTestId("join-username").fill(username);
  await member.getByTestId("join-password").fill("family-password-1");
  await member.getByTestId("join-repeat").fill("family-password-1");
  await member.getByTestId("join-submit").click();
  await expect(member).toHaveURL(/\/discover/);
  await expect(member.getByTestId("current-username")).toHaveText(username);

  // Library pages are out of reach for requesters.
  await member.goto("/movies");
  await expect(member).toHaveURL(/\/discover/);
  await expect(member.getByRole("link", { name: "Movies" })).toHaveCount(0);

  // Discover search → title page → request.
  await member.getByTestId("discover-search").fill("Tears of Steel");
  await member.getByTestId("discover-search").press("Enter");
  await member.getByTestId(`poster-movie-${tearsOfSteel}`).click();
  await expect(member.getByTestId("title-name")).toHaveText("Tears of Steel");
  await member.getByTestId("request-button").click();
  await member.getByTestId("request-confirm").click();
  await expect(member.getByTestId("request-result")).toHaveAttribute("data-status", "pending");
  await member.getByTestId("request-done").click();
  await expect(member.getByTestId("title-status")).toHaveAttribute("data-status", "pending");

  // The administrator approves it.
  await page.goto("/requests?view=all&status=pending");
  const row = page.locator("li[data-testid^=request-]").filter({ hasText: "Tears of Steel" }).filter({ hasText: username }).first();
  await expect(row).toBeVisible();
  const id = (await row.getAttribute("data-testid"))!.replace("request-", "");
  await page.getByTestId(`approve-${id}`).click();
  await expect(page.getByTestId(`request-${id}`)).toHaveCount(0);

  // Locarr adds, searches, grabs and imports it; the family member sees it as available.
  await expect(async () => {
    await member.reload();
    await expect(member.getByTestId("title-status")).toHaveAttribute("data-status", "available", { timeout: 2_000 });
  }).toPass({ timeout: 180_000, intervals: [3_000] });
  await member.goto("/requests");
  await expect(member.getByTestId(`request-${id}`)).toHaveAttribute("data-status", "available");

  // And has a notification saying so.
  const bell = member.getByTestId("notif-bell");
  await expect(bell.getByTestId("notif-count")).toBeVisible();
  await bell.click();
  await expect(member.getByTestId("notif-item").filter({ hasText: /Tears of Steel.*is available/ })).toBeVisible();
  await family.close();
});
