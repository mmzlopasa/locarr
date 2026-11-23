import { expect, test, type Page } from "@playwright/test";

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

test("settings: grouped sections on desktop, filter and deep links", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await signInAsAdmin(page);
  await page.getByRole("link", { name: "Settings", exact: true }).click();
  const nav = page.getByRole("navigation", { name: "Settings sections" });
  await expect(nav.getByRole("link", { name: "General" })).toHaveAttribute("aria-current", "page");

  await nav.getByRole("link", { name: "Quality profiles" }).click();
  await expect(page).toHaveURL(/\/settings\/profiles$/);
  await expect(nav.getByRole("link", { name: "Quality profiles" })).toHaveAttribute("aria-current", "page");

  // The filter finds a page by name and opens the first match with Enter.
  await nav.getByRole("searchbox", { name: "Filter settings" }).fill("custom");
  await expect(nav.getByRole("link")).toHaveCount(1);
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/settings\/customformats$/);
  await expect(page.getByRole("heading", { name: "Custom formats" })).toBeVisible();

  // ... and by what is inside a page.
  await nav.getByRole("searchbox", { name: "Filter settings" }).fill("rename");
  await expect(nav.getByRole("link", { name: "Media management" })).toBeVisible();

  // A deep link (health checks and the apps link straight to a page) marks its entry.
  await page.goto("/settings/notifications");
  await expect(nav.getByRole("link", { name: "Notifications" })).toHaveAttribute("aria-current", "page");
  await expect(page.locator("[aria-current=page]", { hasText: "Custom formats" })).toHaveCount(0);

  // No sideways scrolling.
  expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);
});

test("settings on a phone: the section bar opens a sheet", async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 780 });
  await signInAsAdmin(page);
  await page.goto("/settings/customformats");
  const bar = page.getByTestId("settings-nav-toggle");
  await expect(bar).toContainText("Custom formats");
  await expect(page.getByRole("navigation", { name: "Settings sections" })).toBeHidden();

  await bar.click();
  const sheet = page.getByRole("dialog", { name: "Settings" });
  await expect(sheet.getByRole("link", { name: "Custom formats" })).toHaveAttribute("aria-current", "page");
  await sheet.getByRole("link", { name: "Media folders" }).click();
  await expect(page).toHaveURL(/\/settings\/media$/);
  await expect(sheet).toBeHidden();
  await expect(bar).toContainText("Media folders");

  // Escape closes the sheet; nothing scrolls sideways at 360 px.
  await bar.click();
  await expect(sheet).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(sheet).toBeHidden();
  expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);
});
