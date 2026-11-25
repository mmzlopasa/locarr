import { expect, test, type Page } from "@playwright/test";

// Needs the compose stack with the fakes (scripts/dev-fakes.sh): fake StashDB/ThePornDB under
// http://fake-indexer:9117/adultmeta (LOCARR_STASHDB_URL / LOCARR_TPDB_URL) with synthetic,
// non-explicit entries and grey placeholder images.
const setupCode = process.env.E2E_SETUP_CODE ?? "E2E-SETUP";
const adminUser = process.env.E2E_ADMIN_USER ?? "admin";
const adminPassword = process.env.E2E_ADMIN_PASSWORD ?? "e2e-password-123";
const stashKey = "stash-test-key";
const adultRoot = process.env.E2E_ADULT_ROOT ?? "/data/media/adult";

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

test("adult content: off by default, enable with confirmation, grant, blurred images", async ({ page }) => {
  test.setTimeout(120_000);
  await signInAsAdmin(page);

  // Off by default: no nav entry, the section redirects away and its API answers 404.
  await expect(page.getByRole("link", { name: "Adult", exact: true })).toHaveCount(0);
  expect((await page.request.get("/api/v1/adult/items")).status()).toBe(404);

  // Settings → Adult content: enabling needs the explicit confirmation.
  await page.goto("/settings/adult");
  await page.getByTestId("adult-enable").click();
  await expect(page.getByTestId("adult-enable-confirm")).toBeDisabled();
  await page.getByTestId("adult-confirm").check();
  await page.getByTestId("adult-enable-confirm").click();
  await expect(page.getByText("On", { exact: true })).toBeVisible();
  // The protected terms are fixed; the owner's own list starts empty (D-109).
  await expect(page.getByTestId("adult-blocklist-protected")).toContainText("underage");
  await expect(page.getByTestId("adult-blocklist-empty")).toBeVisible();

  // Enabled, but the administrator holds no view_adult yet: still hidden.
  expect((await page.request.get("/api/v1/adult/items")).status()).toBe(404);
  const me = await (await page.request.get("/api/v1/auth/me")).json();
  const grant = await page.request.patch(`/api/v1/users/${me.id}`, {
    data: { permissions: { ...me.permissions, viewAdult: true } },
  });
  expect(grant.ok(), await grant.text()).toBeTruthy();
  const key = await page.request.put("/api/v1/adult/settings", { data: { stashdbKey: stashKey } });
  expect(key.ok(), await key.text()).toBeTruthy();
  const rf = await page.request.post("/api/v1/adult/rootfolders", { data: { path: adultRoot } });
  expect([201, 409].includes(rf.status()), await rf.text()).toBeTruthy();

  // The section appears; add a studio from the fake StashDB.
  await page.reload();
  await page.getByRole("link", { name: "Adult", exact: true }).click();
  await expect(page.getByTestId("adult-section")).toBeVisible();
  await page.getByRole("link", { name: "Add", exact: true }).click();
  await page.getByLabel("Search", { exact: true }).fill("studio");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  const results = page.getByTestId("adult-lookup");
  await expect(results).toContainText("Studio A");
  const add = results.getByRole("button", { name: "Add" });
  if (await add.count()) {
    await add.first().click();
    // Adding a studio pulls its scenes in; the page opens when that is done.
    await page.waitForURL(/\/adult\/studios\/\d+/);
  }

  // Images are blurred until clicked, and only served to permitted users.
  await page.goto("/adult");
  await expect(page.getByTestId("adult-items")).toContainText("Scene Title");
  const image = page.getByTestId("adult-image").first();
  await expect(image).toHaveAttribute("data-blurred", "true");
  await expect(image.locator("img")).toBeVisible();
  await image.click();
  await expect(image).toHaveAttribute("data-blurred", "false");

  // Turning blurring off is a per-user preference.
  await page.getByLabel("Blur images").uncheck();
  await page.reload();
  await expect(page.getByTestId("adult-image").first()).toHaveAttribute("data-blurred", "false");
  await page.getByLabel("Blur images").check();

  // Turn it off again: the section disappears and the API is 404.
  const off = await page.request.put("/api/v1/adult/settings", { data: { enabled: false } });
  expect(off.ok()).toBeTruthy();
  await page.goto("/");
  await expect(page.getByRole("link", { name: "Adult", exact: true })).toHaveCount(0);
  expect((await page.request.get("/api/v1/adult/items")).status()).toBe(404);
});
