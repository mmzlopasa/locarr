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
