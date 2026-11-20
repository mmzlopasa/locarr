import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Settings } from "@/api/client";
import { mockApi, renderApp, testUser } from "@/test-utils";

describe("general settings", () => {
  it("shows a stored secret only as configured and sends a new one write-only", async () => {
    let settings: Settings = { instanceName: "Home", tmdbApiKey: { configured: true } };
    const patches: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (path !== "/settings") return undefined;
      if (method === "PATCH") {
        patches.push(body);
        const b = body as { tmdbApiKey?: string };
        settings = { ...settings, tmdbApiKey: { configured: b.tmdbApiKey !== "" } };
      }
      return { status: 200, body: settings };
    });
    renderApp("/settings", { me: testUser() });
    expect(await screen.findByText("Configured")).toBeInTheDocument();
    const keyInput = screen.getByLabelText("TMDb API key");
    // The stored value never reaches the browser.
    expect(keyInput).toHaveValue("");

    await userEvent.click(screen.getByRole("button", { name: "Remove" }));
    await waitFor(() => expect(screen.getByText("Not set")).toBeInTheDocument());
    expect(patches).toEqual([{ tmdbApiKey: "" }]);
  });
});
