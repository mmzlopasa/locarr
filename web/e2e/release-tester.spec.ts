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

test("release tester parses a name and explains the decision", async ({ page }) => {
  await signInAsAdmin(page);
  await page.getByRole("link", { name: "Settings" }).click();
  await page.getByRole("link", { name: "Release tester" }).click();

  await page.getByTestId("tester-input").fill("Blade.Runner.2049.2017.2160p.UHD.BluRay.REMUX.HDR10.HEVC.TrueHD.7.1.Atmos-FGT");
  await expect(page.getByTestId("tester-title")).toHaveText("Blade Runner 2049");
  await expect(page.getByTestId("tester-quality")).toHaveText("Remux-2160p");
  await expect(page.getByTestId("tester-group")).toHaveText("FGT");

  // HD-1080p does not allow 2160p remuxes: the verdict says why.
  await page.getByTestId("tester-profile").selectOption({ label: "Movies · HD-1080p" });
  const verdict = page.getByTestId("tester-verdict");
  await expect(verdict).toContainText("Would be rejected");
  await expect(verdict).toContainText("Remux-2160p is not allowed in profile HD-1080p.");

  // A 1080p Blu-ray of a sane size is taken.
  await page.getByTestId("tester-input").fill("Blade.Runner.2049.2017.1080p.BluRay.x264-SPARKS");
  await page.getByLabel("Size (GB)").fill("12");
  await expect(verdict).toContainText("Would be taken");

  // Too big for 164 minutes at the default Bluray-1080p limit.
  await page.getByLabel("Size (GB)").fill("40");
  await page.getByRole("button", { name: "Compare with a wanted title" }).click();
  await page.getByLabel("Runtime (min)").fill("164");
  await expect(verdict).toContainText("maximum for Bluray-1080p (2 h 44 min)");
});
