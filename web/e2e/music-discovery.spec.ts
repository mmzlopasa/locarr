import { expect, test, type Page } from "@playwright/test";

// Needs the fake services (scripts/dev-fakes.sh) and Locarr started with LOCARR_MUSICBRAINZ_URL,
// LOCARR_COVERART_URL, LOCARR_LASTFM_URL and LOCARR_LISTENBRAINZ(_LABS)_URL pointing at them
// (see the e2e job in .github/workflows/ci.yml). No download client is needed.
const setupCode = process.env.E2E_SETUP_CODE ?? "E2E-SETUP";
const adminUser = process.env.E2E_ADMIN_USER ?? "admin";
const adminPassword = process.env.E2E_ADMIN_PASSWORD ?? "e2e-password-123";
const musicRoot = process.env.E2E_MUSIC_ROOT ?? "/data/media/music";
const lastfmKey = "lastfmkey";
const openMbid = "5a8e07d5-d932-4484-a7f7-e700793a9c94";
const choirMbid = "5a8e07d5-d932-4484-a7f7-e700793a9c96";

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

test("discover artists: a Last.fm key, suggestions from the library's artist, add one in a click", async ({ page }) => {
  test.setTimeout(180_000);
  await signInAsAdmin(page);
  const ok = (status: number) => [200, 201, 409].includes(status);
  const rf = await page.request.post("/api/v1/rootfolders", { data: { path: musicRoot, kind: "music" } });
  expect(ok(rf.status()), await rf.text()).toBeTruthy();
  // Artists left from earlier runs (and the music flow's artist) go first, and "not interested" is cleared.
  for (const q of ["Open%20Source", "Creative%20Commons"]) {
    const list = await (await page.request.get(`/api/v1/music/artists?q=${q}`)).json();
    for (const a of list.items ?? []) await page.request.delete(`/api/v1/music/artists/${a.id}?deleteFiles=true`);
  }
  await page.request.delete(`/api/v1/music/discover/exclusions/${choirMbid}`);

  // Settings → Metadata: the Last.fm key is checked with Last.fm and never shown again.
  await page.goto("/settings/metadata");
  await page.getByTestId("lastfm-key").fill(lastfmKey);
  await page.getByTestId("save-discovery").click();
  await expect(page.getByTestId("lastfm-key")).toHaveAttribute("placeholder", /set/i);
  await expect(page.getByTestId("lastfm-key")).toHaveValue("");

  // A library artist to start from.
  const roots = await (await page.request.get("/api/v1/rootfolders")).json();
  const root = roots.find((r: { kind: string; path: string }) => r.kind === "music" && r.path === musicRoot);
  const profiles = await (await page.request.get("/api/v1/qualityprofiles")).json();
  const quality = profiles.find((p: { kind: string }) => p.kind === "music");
  const meta = (await (await page.request.get("/api/v1/music/metadataprofiles")).json())[0];
  const add = await page.request.post("/api/v1/music/artists", { data: { mbid: openMbid, rootFolderId: root.id, qualityProfileId: quality.id,
    metadataProfileId: meta.id, monitor: "none", searchOnAdd: false } });
  expect(add.status(), await add.text()).toBe(201);
  await page.request.post("/api/v1/music/discover/refresh", { data: { seeds: [openMbid] } });

  await page.getByRole("link", { name: "Music" }).first().click();
  await page.getByTestId("discover-artists").click();
  const card = page.locator(`[data-testid="artist-suggestion"][data-mbid="${choirMbid}"]`);
  await expect(card).toBeVisible({ timeout: 90_000 });
  await expect(card).toContainText("Similar to Open Source Artist");
  await expect(card.getByRole("link", { name: /at Last\.fm/ })).toBeVisible();
  await expect(page.getByTestId("discovery-attribution")).toContainText("Last.fm");

  await card.getByTestId(`add-${choirMbid}`).click();
  const dialog = page.getByTestId("add-suggestions-dialog");
  await expect(dialog.getByTestId("add-suggestions-confirm")).toBeEnabled();
  await dialog.getByTestId("add-suggestions-confirm").click();
  await expect(dialog).toBeHidden({ timeout: 30_000 });
  await expect(card).toBeHidden({ timeout: 15_000 });

  const added = await (await page.request.get("/api/v1/music/artists?q=Creative%20Commons")).json();
  expect(added.items.map((a: { mbid: string }) => a.mbid)).toContain(choirMbid);

  // The artist page lists it among the similar artists, already in the library.
  const open = (await (await page.request.get("/api/v1/music/artists?q=Open%20Source")).json()).items[0];
  await page.goto(`/music/artists/${open.id}`);
  await expect(page.getByTestId("similar-artists")).toContainText("Creative Commons Choir");
});

test.afterAll(async ({ request }) => {
  // Leave the library as the other specs expect it (the session cookie is per test, so sign in again).
  await request.post("/api/v1/auth/login", { data: { username: adminUser, password: adminPassword, useCookie: true } });
  const list = await (await request.get("/api/v1/music/artists?q=Creative%20Commons")).json().catch(() => ({ items: [] }));
  for (const a of list.items ?? []) await request.delete(`/api/v1/music/artists/${a.id}?deleteFiles=true`);
});
