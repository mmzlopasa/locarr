import { expect, test, type Page } from "@playwright/test";

// Needs the fake indexers on the stack's network: ./scripts/dev-fakes.sh up
const fakeBase = process.env.E2E_FAKE_INDEXER ?? "http://fake-indexer:9117";
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

test("add a fake indexer, test it and search with streamed results", async ({ page }) => {
  await signInAsAdmin(page);
  const name = `Fake ${Date.now().toString(36)}`;
  await page.getByRole("link", { name: "Indexers" }).click();
  await page.getByRole("button", { name: "Add indexer" }).click();
  await page.getByRole("tab", { name: "Torznab / Newznab" }).click();
  const form = page.getByTestId("indexer-form");
  await form.getByLabel("Name").fill(name);
  await form.getByLabel("URL", { exact: true }).fill(`${fakeBase}/torznab`);
  await form.getByLabel("API key").fill("fakekey");
  await form.getByRole("button", { name: "Test" }).click();
  await expect(form.getByText(/Works: \d+ results/)).toBeVisible();
  await expect(form.getByTestId("category-picker")).toBeVisible();
  await form.getByRole("button", { name: "Add", exact: true }).click();
  const row = page.getByTestId(`indexer-${name}`);
  await expect(row).toBeVisible();

  await page.getByRole("link", { name: "Search" }).click();
  await page.getByLabel("Search for").fill("big buck bunny");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  const results = page.getByTestId("search-results");
  await expect(results.getByText("Big.Buck.Bunny.2008.1080p.BluRay.x264-LOCARR").first()).toBeVisible();
  await expect(page.getByTestId("search-summary")).toBeVisible();
  await expect(results.getByRole("button", { name: /^Grab: / }).first()).toBeEnabled();
  await expect(results.getByRole("link", { name: "Download file" }).first()).toBeVisible();

  // Clean up so the test can run again against the same stack.
  await page.getByRole("link", { name: "Indexers" }).click();
  await row.getByRole("button", { name: "Delete" }).click();
  await row.getByRole("button", { name: `Delete ${name}` }).click();
  await expect(row).toHaveCount(0);
});

test("switch on a built-in site, point it at a local copy and search it", async ({ page }) => {
  await signInAsAdmin(page);
  await page.getByRole("link", { name: "Indexers" }).click();
  // A leftover from an earlier run would make the switch flip it off.
  const old = page.getByTestId("indexer-Nyaa");
  if (await old.count()) {
    await old.getByRole("button", { name: "Delete" }).click();
    await old.getByRole("button", { name: "Delete Nyaa" }).click();
    await expect(old).toHaveCount(0);
  }
  await page.getByRole("button", { name: "Add indexer" }).click();
  await expect(page.getByRole("tab", { name: "Built-in sites" })).toHaveAttribute("aria-selected", "true");
  await page.getByTestId("builtin-nyaa").getByRole("switch", { name: "Use Nyaa" }).click();
  const row = page.getByTestId("indexer-Nyaa");
  await expect(row).toBeVisible();

  // The recorded answers of the site, served by the fakes.
  await row.getByRole("button", { name: "Edit" }).click();
  const form = page.getByTestId("native-form");
  await form.getByLabel("Address").selectOption({ label: "Other address…" });
  await form.getByLabel("Custom address").fill(`${fakeBase}/sites/nyaa`);
  await form.getByRole("button", { name: "Save" }).click();
  await expect(row).toContainText(`${fakeBase}/sites/nyaa`);
  await row.getByRole("button", { name: "Test" }).click();
  await expect(row.getByText(/Works: \d+ results/)).toBeVisible();

  await page.getByRole("link", { name: "Search" }).click();
  await page.getByLabel("Search for").fill("frieren");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  const results = page.getByTestId("search-results");
  await expect(results.getByText(/^\[Erai-raws\] Sousou no Frieren 2nd Season/).first()).toBeVisible();

  await page.getByRole("link", { name: "Indexers" }).click();
  await row.getByRole("button", { name: "Delete" }).click();
  await row.getByRole("button", { name: "Delete Nyaa" }).click();
  await expect(row).toHaveCount(0);
});

// Needs the built-in browser on the stack (docker compose --profile browser up -d); CI sets E2E_BROWSER=1.
test("a 1337x-style site behind a browser check is searched through the built-in browser", async ({ page }) => {
  test.skip(!process.env.E2E_BROWSER, "the built-in browser is not running on this stack");
  test.setTimeout(180_000);
  await signInAsAdmin(page);
  await page.getByRole("link", { name: "Indexers" }).click();
  const panel = page.getByTestId("browser-panel");
  await expect(panel.getByText("Connected")).toBeVisible();
  await panel.getByRole("button", { name: "Test", exact: true }).click();
  await expect(panel.getByTestId("browser-test-result")).toContainText("Passed: page opened", { timeout: 60_000 });
  await panel.getByLabel("Or open an address").fill(`${fakeBase}/protected/test`);
  await panel.getByRole("button", { name: "Open" }).click();
  await expect(panel.getByTestId("browser-test-result")).toContainText("Passed: page opened", { timeout: 60_000 });

  const old = page.getByTestId("indexer-1337x");
  if (await old.count()) {
    await old.getByRole("button", { name: "Delete" }).click();
    await old.getByRole("button", { name: "Delete 1337x" }).click();
    await expect(old).toHaveCount(0);
  }
  await page.getByRole("button", { name: "Add indexer" }).click();
  await page.getByTestId("builtin-1337x").getByRole("switch", { name: "Use 1337x" }).click();
  const row = page.getByTestId("indexer-1337x");
  await expect(row).toBeVisible();
  await row.getByRole("button", { name: "Edit" }).click();
  const form = page.getByTestId("native-form");
  await form.getByLabel("Address").selectOption({ label: "Other address…" });
  // The recorded 1337x pages behind a JavaScript check that sets a cookie and reloads.
  await form.getByLabel("Custom address").fill(`${fakeBase}/protected/sites/1337x`);
  await form.getByRole("button", { name: "Save" }).click();
  await expect(row).toContainText(`${fakeBase}/protected/sites/1337x`);
  await row.getByRole("button", { name: "Test" }).click();
  await expect(row.getByText(/Works: \d+ results/)).toBeVisible({ timeout: 90_000 });

  await page.getByRole("link", { name: "Search" }).click();
  await page.getByLabel("Search for").fill("ubuntu");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await expect(page.getByTestId("search-results").getByText("Ubuntu Linux Bible 11E by David Clinton (epub)(Nonfiction)").first())
    .toBeVisible({ timeout: 90_000 });

  await page.getByRole("link", { name: "Indexers" }).click();
  await expect(panel.getByTestId("browser-last-solve")).toBeVisible();
  await row.getByRole("button", { name: "Delete" }).click();
  await row.getByRole("button", { name: "Delete 1337x" }).click();
  await expect(row).toHaveCount(0);
});
