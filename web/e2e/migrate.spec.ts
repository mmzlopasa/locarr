import { expect, test, type Page } from "@playwright/test";

// Needs the fakes on the stack's network with a seed folder inside the data
// folder (as CI starts them): LOCARR_FAKES_SEED_DIR=<data>/downloads/fake-seed ./scripts/dev-fakes.sh up
// The fake applications live at http://fake-indexer:9117/arr/<kind> and their
// files at /data/downloads/fake-seed/migrate/{movies,tv,music} as Locarr sees them.
const fakeBase = process.env.E2E_FAKE_INDEXER ?? "http://fake-indexer:9117";
const seed = process.env.E2E_MIGRATE_DIR ?? "/data/downloads/fake-seed/migrate";
const adminUser = process.env.E2E_ADMIN_USER ?? "admin";
const adminPassword = process.env.E2E_ADMIN_PASSWORD ?? "e2e-password-123";
const setupCode = process.env.E2E_SETUP_CODE ?? "E2E-SETUP";

async function signInAsAdmin(page: Page) {
  await page.addInitScript(() => localStorage.setItem("locarr.lang", "en"));
  const info = await (await page.request.get("/api/v1/system/info")).json();
  if (info.setupRequired) {
    await page.request.post("/api/v1/setup", { data: { setupCode, username: adminUser, password: adminPassword } });
  }
  await page.goto("/login");
  await page.getByLabel("Username").fill(adminUser);
  await page.getByLabel("Password").fill(adminPassword);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByTestId("current-username")).toHaveText(adminUser);
}

async function addSource(page: Page, kind: string, label: string, secret: string, username?: string) {
  if (await page.getByTestId(`source-${kind}`).count()) return; // a previous run added it
  const form = page.getByTestId("migrate-add-source");
  await form.getByLabel("Application").selectOption(kind);
  await form.getByLabel("Address").fill(`${fakeBase}/arr/${kind}`);
  if (username) await form.getByLabel("User name").fill(username);
  await form.getByLabel(kind === "qbittorrent" ? "Password" : "API key").fill(secret);
  await form.getByRole("button", { name: "Test", exact: true }).click();
  await expect(form.getByTestId("migrate-test-ok")).toContainText(`Connected to ${label}`);
  await form.getByRole("button", { name: "Test and add" }).click();
  await expect(page.getByTestId(`source-${kind}`)).toBeVisible();
}

test("import a setup from fake applications: preview, folders, import, report, checklist", async ({ page }) => {
  test.setTimeout(180_000);
  await signInAsAdmin(page);
  await page.getByRole("link", { name: "Import setup" }).click();
  await expect(page.getByText(/Nothing is changed on them/)).toBeVisible();

  await addSource(page, "radarr", "Radarr", "migratekey");
  await addSource(page, "sonarr", "Sonarr", "migratekey");
  await addSource(page, "qbittorrent", "qBittorrent", "migratepass", "admin");
  // A wrong key is caught by the test and never saved.
  const form = page.getByTestId("migrate-add-source");
  await form.getByLabel("Application").selectOption("lidarr");
  await form.getByLabel("Address").fill(`${fakeBase}/arr/lidarr`);
  await form.getByLabel("API key").fill("wrong");
  await form.getByRole("button", { name: "Test", exact: true }).click();
  await expect(form.getByTestId("migrate-test-failed")).toContainText("refused");

  await page.getByRole("button", { name: "Next" }).click();
  await expect(page.getByTestId("choose-radarr")).toBeVisible();
  await page.getByTestId("migrate-read").click();

  // Folders: the applications see /movies and /tv; Locarr sees the seed folder.
  await expect(page.getByRole("heading", { name: "Folders" })).toBeVisible({ timeout: 60_000 });
  const rows = page.getByTestId("migrate-mapping");
  while (await rows.count()) await rows.first().getByRole("button", { name: "Remove" }).click();
  const mappings: [string, string][] = [["/movies", `${seed}/movies`], ["/tv", `${seed}/tv`]];
  for (const [from, to] of mappings) {
    await page.getByRole("button", { name: "Add a mapping" }).click();
    const row = rows.last();
    await row.getByLabel("Application's folder").fill(from);
    await row.getByLabel("Locarr's folder").fill(to);
  }
  const checks = page.getByTestId("migrate-path-checks");
  await expect(checks).toContainText(`Found 2 of 2 files under ${seed}/movies`);
  await expect(checks).toContainText(`Found 4 of 4 files under ${seed}/tv`);
  await page.getByTestId("migrate-to-preview").click();

  // Preview: counts per category and reasons.
  const counts = page.getByTestId("migrate-counts");
  await expect(counts.getByTestId("count-movie")).toBeVisible();
  await expect(counts.getByTestId("count-series")).toBeVisible();
  await expect(page.getByTestId("migrate-items")).toBeVisible();
  await page.getByTestId("migrate-import").click();

  // Report.
  const report = page.getByTestId("migrate-report");
  await expect(report).toBeVisible({ timeout: 90_000 });
  await expect(report).toContainText("done");
  const download = page.waitForEvent("download");
  await page.getByTestId("migrate-download").click();
  expect((await download).suggestedFilename()).toMatch(/^locarr-import-\d+\.json$/);

  // The movies are in the library with their files in place.
  await page.getByRole("link", { name: "Movies" }).click();
  await page.getByRole("button", { name: "Table" }).click();
  await expect(page.getByText("Sintel").first()).toBeVisible();

  await page.goto("/migrate/checklist");
  const list = page.getByTestId("migrate-checklist");
  await expect(list.getByTestId("check-import_done")).toContainText("Done");
  await expect(list.getByTestId("check-stop_radarr")).toContainText("Stop automatic searches in");
});
