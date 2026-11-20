import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { User } from "@/api/client";
import { mockApi, renderApp, testUser } from "@/test-utils";

describe("users page", () => {
  it("creates a requester with the chosen permissions", async () => {
    const me = testUser({ id: 1, username: "admin" });
    const users: User[] = [me];
    let created: Record<string, unknown> | undefined;
    mockApi(({ method, path, body }) => {
      if (method === "GET" && path === "/users") return { status: 200, body: users };
      if (method === "POST" && path === "/users") {
        created = body as Record<string, unknown>;
        const u = testUser({ id: 2, username: String(created.username), role: "requester" });
        users.push(u);
        return { status: 201, body: u };
      }
    });
    renderApp("/settings/users", { me });
    expect(await screen.findByTestId("user-admin")).toHaveTextContent("You");

    await userEvent.click(screen.getByRole("button", { name: "Add user" }));
    const form = screen.getByTestId("create-user-form");
    await userEvent.type(within(form).getByLabelText("Username"), "marko");
    await userEvent.type(within(form).getByLabelText("Password"), "long-enough-pw");
    await userEvent.click(within(form).getByLabelText("Request 4K"));
    await userEvent.click(within(form).getByRole("button", { name: "Create user" }));

    expect(await screen.findByTestId("user-marko")).toBeInTheDocument();
    expect(created).toMatchObject({
      username: "marko",
      password: "long-enough-pw",
      role: "requester",
      permissions: { requestMovies: true, request4k: true, autoApprove: false, manageLibrary: false },
    });
    expect(screen.queryByTestId("create-user-form")).not.toBeInTheDocument();
  });

  it("explains why the last administrator cannot be disabled", async () => {
    const me = testUser({ id: 1, username: "admin" });
    const other = testUser({ id: 2, username: "boss" });
    mockApi(({ method, path }) => {
      if (method === "GET" && path === "/users") return { status: 200, body: [me, other] };
      if (method === "PATCH" && path === "/users/2") return { status: 409, body: { code: "last_admin", message: "no" } };
    });
    renderApp("/settings/users", { me });
    const row = await screen.findByTestId("user-boss");
    await userEvent.click(within(row).getByRole("button", { name: "Edit" }));
    await userEvent.click(within(row).getByRole("button", { name: "Disable" }));
    await waitFor(() =>
      expect(within(row).getByRole("alert")).toHaveTextContent("The last enabled administrator cannot be removed"),
    );
  });
});
