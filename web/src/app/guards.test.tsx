import { screen, waitFor } from "@testing-library/react";
import { mockApi, renderApp, testInfo, testUser } from "@/test-utils";

describe("route guards", () => {
  it("sends every page to setup while no administrator exists", async () => {
    mockApi(() => undefined);
    const { router } = renderApp("/settings/users", { info: testInfo({ setupRequired: true }) });
    await waitFor(() => expect(router.state.location.pathname).toBe("/setup"));
    expect(await screen.findByLabelText("Setup code")).toBeInTheDocument();
    expect(screen.getByText("docker compose logs locarr")).toBeInTheDocument();
  });

  it("sends setup to sign-in once setup is done", async () => {
    mockApi(() => undefined);
    const { router } = renderApp("/setup");
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
  });

  it("sends signed-out visitors to sign-in and remembers where they were going", async () => {
    mockApi(() => undefined);
    const { router } = renderApp("/account");
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(router.state.location.search).toEqual({ redirect: "/account" });
  });

  it("keeps non-administrators out of administrator pages", async () => {
    mockApi(() => undefined);
    const { router } = renderApp("/system/tasks", { me: testUser({ role: "requester" }) });
    await waitFor(() => expect(router.state.location.pathname).toBe("/movies"));
    expect(screen.queryByRole("link", { name: "System" })).not.toBeInTheDocument();
  });

  it("goes to sign-in when the session ends while using the app", async () => {
    mockApi(({ path }) =>
      path === "/auth/sessions" || path === "/auth/apikeys"
        ? { status: 401, body: { code: "unauthorized", message: "not signed in" } }
        : undefined,
    );
    const { router, queryClient } = renderApp("/account", { me: testUser() });
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(router.state.location.search).toEqual({ redirect: "/account" });
    expect(queryClient.getQueryData(["auth", "me"])).toBeNull();
  });

  it("shows honest placeholders for sections of later phases", async () => {
    mockApi(() => undefined);
    renderApp("/indexers", { me: testUser() });
    expect(await screen.findByTestId("coming-soon")).toHaveTextContent("arrives in phase P1");
  });
});
