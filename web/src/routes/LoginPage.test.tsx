import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { safeRedirect } from "./LoginPage";

describe("sign-in", () => {
  it("signs in with a cookie session and opens the app", async () => {
    const calls = mockApi(({ method, path, body }) => {
      if (method === "POST" && path === "/auth/login") {
        expect(body).toMatchObject({ username: "ana", password: "secret-pass", useCookie: true });
        return { status: 200, body: { user: testUser() } };
      }
    });
    const { router } = renderApp("/login");
    await userEvent.type(await screen.findByLabelText("Username"), "ana");
    await userEvent.type(screen.getByLabelText("Password"), "secret-pass");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/movies"));
    expect(calls).toHaveBeenCalled();
  });

  it("asks for the two-factor code when the server requires it, then accepts a recovery code", async () => {
    const bodies: Record<string, unknown>[] = [];
    mockApi(({ method, path, body }) => {
      if (method === "POST" && path === "/auth/login") {
        const b = body as Record<string, unknown>;
        bodies.push(b);
        if (!b.totpCode && !b.recoveryCode) return { status: 401, body: { code: "totp_required", message: "code" } };
        if (b.totpCode === "000000") return { status: 401, body: { code: "invalid_totp", message: "bad" } };
        return { status: 200, body: { user: testUser({ language: "hr" }) } };
      }
    });
    const { router } = renderApp("/login");
    await userEvent.type(await screen.findByLabelText("Username"), "ana");
    await userEvent.type(screen.getByLabelText("Password"), "secret-pass");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));

    const code = await screen.findByLabelText("Authenticator code");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    await userEvent.type(code, "000000");
    await userEvent.click(screen.getByRole("button", { name: "Verify" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("That code is not valid");

    await userEvent.click(screen.getByRole("button", { name: "Use a recovery code instead" }));
    await userEvent.type(screen.getByLabelText("Recovery code"), "abcd-efgh-jkmn");
    await userEvent.click(screen.getByRole("button", { name: "Verify" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/movies"));
    expect(bodies.at(-1)).toMatchObject({ username: "ana", recoveryCode: "abcd-efgh-jkmn" });
    expect(bodies.at(-1)).not.toHaveProperty("totpCode");
    // The account's language is applied after sign-in.
    expect(document.documentElement.lang).toBe("hr");
  });

  it("shows a translated error for wrong credentials", async () => {
    mockApi(({ path }) =>
      path === "/auth/login" ? { status: 401, body: { code: "invalid_credentials", message: "nope" } } : undefined,
    );
    renderApp("/login");
    await userEvent.type(await screen.findByLabelText("Username"), "ana");
    await userEvent.type(screen.getByLabelText("Password"), "wrong-pass");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Wrong username or password.");
  });

  it("only follows same-site redirects", () => {
    expect(safeRedirect("/settings/users")).toBe("/settings/users");
    expect(safeRedirect("//evil.example/")).toBe("/");
    expect(safeRedirect("https://evil.example/")).toBe("/");
    expect(safeRedirect("/\\evil.example")).toBe("/");
    expect(safeRedirect(undefined)).toBe("/");
  });
});
