import { expect, test, type Page } from "@playwright/test";

const setupCode = process.env.E2E_SETUP_CODE ?? "E2E-SETUP";
const adminUser = process.env.E2E_ADMIN_USER ?? "admin";
const adminPassword = process.env.E2E_ADMIN_PASSWORD ?? "e2e-password-123";
const tmHost = process.env.E2E_TRANSMISSION_HOST ?? "transmission";
const tmPort = process.env.E2E_TRANSMISSION_PORT ?? "9091";
const tmUser = process.env.E2E_TRANSMISSION_USER ?? "locarr";
const tmPass = process.env.E2E_TRANSMISSION_PASS ?? "locarr-e2e";

async function signInAsAdmin(page: Page) {
  await page.addInitScript(() => localStorage.setItem("locarr.lang", "en"));
  // The app sends us to /setup until an administrator exists, otherwise to /login.
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

const randomHash = () => Array.from({ length: 40 }, () => "0123456789abcdef"[Math.floor(Math.random() * 16)]).join("");

test("add a Transmission client, grab a magnet and manage it in the queue", async ({ page }) => {
  test.skip(tmHost === "none", "no Transmission instance for this run");
  await signInAsAdmin(page);
  const name = `Transmission e2e ${Date.now().toString(36)}`;

  await page.getByRole("link", { name: "Downloaders" }).click();
  await page.getByRole("button", { name: "Add client" }).click();
  await page.getByTestId("pick-transmission").click();
  const form = page.getByTestId("client-form");
  await form.getByLabel("Name", { exact: true }).fill(name);
  await form.getByLabel("Host").fill(tmHost);
  await form.getByLabel("Port", { exact: true }).fill(tmPort);
  await form.getByLabel("Username").fill(tmUser);
  await form.getByLabel("Password", { exact: true }).fill(tmPass);
  await form.getByRole("button", { name: "Test" }).click();
  await expect(form.getByTestId("test-ok").or(form.getByTestId("test-missing"))).toBeVisible();
  await form.getByRole("button", { name: "Save" }).click();
  await expect(page.getByTestId(`client-${name}`)).toBeVisible();

  const title = "Locarr.E2E.Test";
  const hash = randomHash();
  const grab = await page.request.post("/api/v1/queue/grab", {
    data: { downloadUrl: `magnet:?xt=urn:btih:${hash}&dn=${title}`, protocol: "torrent", title, mediaKind: "movies" },
  });
  expect(grab.status()).toBe(201);
  const { id } = await grab.json();

  await page.getByRole("link", { name: "Activity" }).click();
  await expect(page).toHaveURL(/\/activity\/queue$/);
  const row = page.getByTestId(`queue-row-${id}`);
  await expect(row).toContainText(title);
  await expect(row.getByTestId("state-badge")).toBeVisible();

  await row.getByTestId(`remove-data-${id}`).click();
  await row.getByTestId(`remove-data-${id}-confirm`).click();
  await expect(row).toHaveCount(0);

  // Clean up the client so the test can run again.
  await page.getByRole("link", { name: "Downloaders" }).click();
  await page.getByTestId(`client-${name}`).getByRole("button", { name: "Edit" }).click();
  await page.getByTestId("delete-client").click();
  await page.getByTestId("delete-client-confirm").click();
  await expect(page.getByTestId(`client-${name}`)).toHaveCount(0);
});
