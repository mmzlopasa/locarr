import { expect, test, type Page } from "@playwright/test";

// Needs the fake services (scripts/dev-fakes.sh with LOCARR_FAKES_SEED_DIR), Locarr started with
// LOCARR_MUSICBRAINZ_URL / LOCARR_COVERART_URL pointing at the fake MusicBrainz, and a Transmission
// that saves into the seed folder under the same /data path (see the e2e job in .github/workflows/ci.yml).
const setupCode = process.env.E2E_SETUP_CODE ?? "E2E-SETUP";
const adminUser = process.env.E2E_ADMIN_USER ?? "admin";
const adminPassword = process.env.E2E_ADMIN_PASSWORD ?? "e2e-password-123";
const tmHost = process.env.E2E_TRANSMISSION_HOST ?? "transmission";
const tmUser = process.env.E2E_TRANSMISSION_USER ?? "locarr";
const tmPass = process.env.E2E_TRANSMISSION_PASS ?? "locarr-e2e";
const indexerUrl = process.env.E2E_INDEXER_URL ?? "http://fake-indexer:9117/torznab";
const musicRoot = process.env.E2E_MUSIC_ROOT ?? "/data/media/music";
const artistMbid = "5a8e07d5-d932-4484-a7f7-e700793a9c94";

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
  const ix = await page.request.post("/api/v1/indexers", { data: { name: "Fake music indexer", kind: "torznab", baseUrl: indexerUrl, apiKey: "fakekey" } });
  expect(ok(ix.status()), await ix.text()).toBeTruthy();
  const cl = await page.request.post("/api/v1/downloadclients", { data: { name: "Transmission music", implementation: "transmission",
    host: tmHost, port: 9091, username: tmUser, password: tmPass, categories: { movies: "", series: "", music: "locarr-music" } } });
  expect(ok(cl.status()), await cl.text()).toBeTruthy();
  const rf = await page.request.post("/api/v1/rootfolders", { data: { path: musicRoot, kind: "music" } });
  expect(ok(rf.status()), await rf.text()).toBeTruthy();
  // An artist left from an earlier run goes first (to the recycle bin with its files).
  const list = await (await page.request.get("/api/v1/music/artists?q=Open%20Source")).json();
  for (const a of list.items ?? []) await page.request.delete(`/api/v1/music/artists/${a.id}?deleteFiles=true`);
}

test("add an artist: MusicBrainz search, album search and grab, download, import by tags, cover and the album timeline", async ({ page }) => {
  test.skip(tmHost === "none", "no Transmission instance for this run");
  test.setTimeout(240_000);
  await signInAsAdmin(page);
  await prepare(page);

  await page.getByRole("link", { name: "Music" }).first().click();
  await page.getByTestId("add-artist").click();
  await page.getByTestId("add-artist-term").fill("Open Source Artist");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  // Two artists share the name; the disambiguation tells them apart.
  await expect(page.getByTestId("artist-result")).toHaveCount(2);
  await expect(page.getByTestId("artist-result").filter({ hasText: "jazz trio from Zagreb" })).toBeVisible();
  await page.getByTestId(`pick-${artistMbid}`).click();
  await page.getByLabel("Root folder").selectOption({ label: musicRoot });
  await page.getByLabel("Quality profile").selectOption({ label: "Lossless" });
  await page.getByLabel("Metadata profile").selectOption({ label: "Standard" });
  await page.getByLabel("Search now").check();
  await page.getByTestId("add-artist-confirm").click();
  await expect(page.getByTestId("artist-name")).toHaveText("Open Source Artist");

  // The Standard profile keeps albums and EPs; live albums and singles stay out.
  await expect(page.getByTestId("album-group-album")).toContainText("Free Album");
  await expect(page.getByTestId("album-group-ep")).toContainText("Free EP");
  await expect(page.getByTestId("album-group-live")).toHaveCount(0);

  await page.getByRole("link", { name: "Free Album", exact: true }).click();
  await expect(page.getByTestId("album-title")).toHaveText("Free Album");
  await page.getByTestId("open-history").click();
  const drawer = page.getByTestId("history-drawer");
  await expect(drawer.getByTestId("why-box")).toHaveAttribute("data-code", "available", { timeout: 200_000 });
  const timeline = drawer.getByTestId("timeline");
  for (const type of ["search", "decision", "download_grabbed", "download_completed", "release_matched", "file_imported", "renamed", "cover_saved"]) {
    await expect(timeline.locator(`[data-type="${type}"]`).first()).toBeVisible();
  }
  await page.keyboard.press("Escape");

  await page.reload();
  await expect(page.getByTestId("album-path")).toContainText("/Open Source Artist/Free Album (2020)");
  const rows = page.getByTestId("track-row");
  await expect(rows).toHaveCount(5);
  await expect(rows.first()).toHaveAttribute("data-has-file", "true");
  await expect(rows.first()).toContainText("01 - Opening.flac");

  // Interactive search lists the releases with the decision (the MP3 one is rejected by the Lossless profile).
  await page.getByTestId("tab-search").click();
  await page.getByTestId("interactive-search").click();
  await expect(page.getByTestId("decision-row").filter({ hasText: "MP3" })).toHaveAttribute("data-status", "rejected", { timeout: 60_000 });

  await page.goto("/music");
  await expect(page.getByTestId("artist-card").filter({ hasText: "Open Source Artist" })).toContainText("1 of 3 albums");
});
