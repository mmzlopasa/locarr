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
  // The image really decodes (the page's CSP must allow the blob: URL it is shown from).
  await expect.poll(() => image.locator("img").evaluate((el: HTMLImageElement) => el.complete && el.naturalWidth > 0)).toBe(true);
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

test("adult content: remove a scene as not interested, it stays gone after a refresh; bulk select", async ({ page }) => {
  test.setTimeout(120_000);
  await signInAsAdmin(page);
  const on = await page.request.put("/api/v1/adult/settings", { data: { enabled: true, confirmAdults: true, stashdbKey: stashKey } });
  expect(on.ok(), await on.text()).toBeTruthy();
  const me = await (await page.request.get("/api/v1/auth/me")).json();
  await page.request.patch(`/api/v1/users/${me.id}`, { data: { permissions: { ...me.permissions, viewAdult: true } } });
  const rf = await page.request.post("/api/v1/adult/rootfolders", { data: { path: adultRoot } });
  expect([201, 409].includes(rf.status()), await rf.text()).toBeTruthy();
  const st = await page.request.post("/api/v1/adult/studios", { data: { source: "stashdb", id: "st-studio-a", monitored: true, search: false } });
  expect([201, 409].includes(st.status()), await st.text()).toBeTruthy();
  const studios = await (await page.request.get("/api/v1/adult/studios")).json();
  const studioId = studios.find((s: { name: string }) => s.name === "Studio A").id;
  // Start clean: undo exclusions an earlier run left behind and bring the scenes back.
  for (const e of await (await page.request.get("/api/v1/adult/exclusions")).json()) {
    await page.request.delete(`/api/v1/adult/exclusions/${e.id}`);
  }
  await page.request.post(`/api/v1/adult/studios/${studioId}/refresh`);

  await page.goto("/adult");
  const grid = page.getByTestId("adult-items");
  await expect(grid).toContainText("Second Scene");
  await grid.getByRole("link", { name: "Second Scene" }).click();
  await page.waitForURL(/\/adult\/items\/\d+/);
  await page.getByTestId("adult-remove").click();
  const dialog = page.getByTestId("adult-remove-dialog");
  await expect(dialog.getByTestId("adult-remove-exclude")).toBeChecked();
  await dialog.getByTestId("adult-remove-confirm").click();
  await page.waitForURL(/\/adult$/);
  await expect(grid).toContainText("Scene Title");
  await expect(grid).not.toContainText("Second Scene");

  // Refreshing the followed studio does not bring it back.
  await page.goto(`/adult/studios/${studioId}`);
  await page.getByRole("button", { name: "Look for new scenes" }).click();
  await expect(page.getByText(/New scenes: \d+/)).toBeVisible();
  await expect(page.getByTestId("adult-items")).not.toContainText("Second Scene");

  // Settings → Adult content lists it; undo, and the next refresh adds it again.
  await page.goto("/settings/adult");
  const exclusions = page.getByTestId("adult-exclusions");
  await expect(exclusions).toContainText("Second Scene");
  await exclusions.getByTestId("adult-exclusion-undo").first().click();
  await expect(page.getByTestId("adult-exclusions-empty")).toBeVisible();
  await page.goto(`/adult/studios/${studioId}`);
  await page.getByRole("button", { name: "Look for new scenes" }).click();
  await expect(page.getByTestId("adult-items")).toContainText("Second Scene");

  // Bulk: select two scenes and monitor them, then unmonitor.
  await page.goto("/adult");
  await page.getByTestId("adult-status-filter").selectOption("missing");
  await expect(page.getByTestId("adult-card").first()).toBeVisible();
  await page.getByTestId("adult-select").click();
  await page.getByTestId("adult-card").nth(0).click();
  await page.getByTestId("adult-card").nth(1).click();
  await expect(page.getByTestId("adult-selected-count")).toHaveText("Selected: 2");
  await page.getByTestId("adult-bulk-monitor").click();
  await page.getByTestId("adult-select").click();
  await page.getByTestId("adult-status-filter").selectOption("wanted");
  await expect(page.getByTestId("adult-card")).toHaveCount(2);
  await page.getByTestId("adult-select").click();
  await page.getByTestId("adult-selection-bar").getByRole("button", { name: "Select all" }).click();
  await page.getByTestId("adult-bulk-unmonitor").click();
  await expect(page.getByTestId("adult-empty")).toBeVisible();

  // The tab bar scrolls sideways on a phone without a scrollbar of its own and the page does not.
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/adult");
  await expect(page.getByTestId("adult-card").first()).toBeVisible();
  const nav = page.getByRole("navigation", { name: "Adult" });
  const box = await nav.evaluate((el) => ({ h: el.scrollHeight - el.clientHeight, overflowY: getComputedStyle(el).overflowY }));
  expect(box.h).toBe(0);
  expect(box.overflowY).toBe("hidden");
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();

  const off = await page.request.put("/api/v1/adult/settings", { data: { enabled: false } });
  expect(off.ok()).toBeTruthy();
});
