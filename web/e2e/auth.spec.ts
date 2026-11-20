import { expect, test, type Page } from "@playwright/test";

const setupCode = process.env.E2E_SETUP_CODE ?? "E2E-SETUP";
const adminUser = process.env.E2E_ADMIN_USER ?? "admin";
const adminPassword = process.env.E2E_ADMIN_PASSWORD ?? "e2e-password-123";

async function signIn(page: Page, username: string, password: string) {
  await page.goto("/login");
  await page.getByLabel("Username").fill(username);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByTestId("current-username")).toHaveText(username);
}

async function signOut(page: Page) {
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login/);
}

test("first-run setup, sign-in, creating a user and signing out", async ({ page, request }) => {
  await page.addInitScript(() => localStorage.setItem("locarr.lang", "en"));
  const info = await (await request.get("/api/v1/system/info")).json();

  if (info.setupRequired) {
    await page.goto("/");
    await expect(page).toHaveURL(/\/setup$/);
    await page.getByLabel("Setup code").fill(setupCode);
    await page.getByLabel("Administrator username").fill(adminUser);
    await page.getByLabel("Password", { exact: true }).fill(adminPassword);
    await page.getByLabel("Repeat password").fill(adminPassword);
    await page.getByRole("button", { name: "Create administrator" }).click();
    await expect(page.getByTestId("current-username")).toHaveText(adminUser);
    await signOut(page);
  }

  await signIn(page, adminUser, adminPassword);
  // Sections of later phases say so instead of pretending.
  await expect(page.getByTestId("coming-soon")).toBeVisible();

  const username = `e2e-${Date.now().toString(36)}`;
  await page.getByRole("link", { name: "Settings" }).click();
  await page.getByRole("link", { name: "Users" }).click();
  await page.getByRole("button", { name: "Add user" }).click();
  const form = page.getByTestId("create-user-form");
  await form.getByLabel("Username").fill(username);
  await form.getByLabel("Password").fill("e2e-user-password");
  await form.getByRole("button", { name: "Create user" }).click();
  await expect(page.getByTestId(`user-${username}`)).toBeVisible();

  await signOut(page);
  // The session is really gone: the app sends us back to sign-in.
  await page.goto("/account");
  await expect(page).toHaveURL(/\/login/);

  await signIn(page, username, "e2e-user-password");
  await expect(page.getByRole("link", { name: "Settings" })).toHaveCount(0);
  await signOut(page);
});

test("scanner paths are not answered with the app", async ({ request }) => {
  expect((await request.get("/.env")).status()).toBe(404);
  expect((await request.get("/wp-login.php")).status()).toBe(404);
  const robots = await request.get("/robots.txt");
  expect(await robots.text()).toContain("Disallow: /");
});
