import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { IndexerSettings } from "@/api/client";
import { mockApi, renderApp, testUser } from "@/test-utils";

const settings: IndexerSettings = { rssSyncIntervalMinutes: 15, releaseRetentionDays: 14, searchTimeoutSeconds: 45, searchCacheSeconds: 120,
  allowedHosts: [], blockPrivateNetworks: false, publicTrackers: [], userAgent: "",
  browserUrl: "", browserDefaultUrl: "http://locarr-browser:8490", browserToken: { configured: false }, browserTokenFromEnv: true };

function serve(state: { browser: unknown; sent: { method: string; path: string; body: unknown }[] }) {
  mockApi(({ method, path, body }) => {
    if (method !== "GET") state.sent.push({ method, path, body });
    if (method === "GET" && path === "/indexers") return { status: 200, body: [] };
    if (method === "GET" && path === "/indexers/sources") return { status: 200, body: [] };
    if (method === "GET" && path === "/indexers/definitions") return { status: 200, body: { definitions: [], presets: [], errors: [], customDir: "/config/definitions" } };
    if (method === "GET" && path === "/indexers/settings") return { status: 200, body: settings };
    if (method === "GET" && path === "/indexers/browser") return { status: 200, body: state.browser };
    if (method === "PATCH" && path === "/indexers/settings") return { status: 200, body: { ...settings, browserToken: { configured: true } } };
    if (method === "POST" && path === "/indexers/browser/test") {
      const b = body as { variant?: string };
      return b.variant === "human"
        ? { status: 200, body: { passed: true, outcome: "human", challenged: true, cookies: 0, elapsedMs: 13200 } }
        : { status: 200, body: { passed: true, outcome: "solved", challenged: true, cookies: 1, elapsedMs: 2300, title: "Locarr browser self-test" } };
    }
  });
}

describe("browser for protected sites", () => {
  it("shows the compose command when the browser is not running", async () => {
    serve({ sent: [], browser: { state: "missing", url: "http://locarr-browser:8490", command: "docker compose --profile browser up -d",
      message: "not reachable" } });
    renderApp("/indexers", { me: testUser() });
    const panel = await screen.findByTestId("browser-panel");
    expect(await within(panel).findByText("Not running")).toBeInTheDocument();
    expect(within(panel).getByText("docker compose --profile browser up -d")).toBeInTheDocument();
    expect(within(panel).getByLabelText("Token")).toHaveAttribute("placeholder", "from LOCARR_BROWSER_TOKEN");
  });

  it("shows the state, runs the tests and saves the token", async () => {
    const state = { sent: [] as { method: string; path: string; body: unknown }[], browser: { state: "connected", url: "http://locarr-browser:8490",
      command: "docker compose --profile browser up -d", version: "0.6.0", chromium: "Chrome/156.0.8078.108", running: true, sandbox: false,
      tabs: 2, tabsInUse: 1, solves: 4, restarts: 0,
      lastSolve: { at: new Date().toISOString(), host: "1337x.to", outcome: "human", elapsedMs: 16400 } } };
    serve(state);
    renderApp("/indexers", { me: testUser() });
    const panel = await screen.findByTestId("browser-panel");
    expect(await within(panel).findByText("Connected")).toBeInTheDocument();
    expect(within(panel).getByTestId("browser-facts")).toHaveTextContent("1 of 2");
    expect(within(panel).getByTestId("browser-facts")).toHaveTextContent("Chrome/156.0.8078.108");
    expect(within(panel).getByTestId("browser-last-solve")).toHaveTextContent("1337x.to, needs a human check");

    await userEvent.click(within(panel).getByRole("button", { name: "Test" }));
    expect(await within(panel).findByTestId("browser-test-result")).toHaveTextContent("Passed: page opened in 2.3 s, 1 cookies.");
    await userEvent.click(within(panel).getByRole("button", { name: "Test a CAPTCHA page" }));
    await waitFor(() => expect(within(panel).getByTestId("browser-test-result")).toHaveTextContent("needs a human check"));
    expect(state.sent.filter((s) => s.path === "/indexers/browser/test").map((s) => s.body)).toEqual([{}, { variant: "human" }]);

    await userEvent.type(within(panel).getByLabelText("Token"), "s3cret-token");
    await userEvent.click(within(panel).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(state.sent.some((s) => s.method === "PATCH")).toBe(true));
    expect(state.sent.find((s) => s.method === "PATCH")?.body).toEqual({ browserToken: "s3cret-token" });
  });
});
