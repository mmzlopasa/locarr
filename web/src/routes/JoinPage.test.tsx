import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi, renderApp } from "@/test-utils";
import { emptyNotifications, requester } from "@/features/discover/fixtures";
import { codeFromHash } from "./JoinPage";

describe("join with an invite", () => {
  it("reads the code from the link's fragment", () => {
    expect(codeFromHash("#abc%2Ddef")).toBe("abc-def");
    expect(codeFromHash("")).toBe("");
  });

  it("checks the code, creates the account and opens Discover", async () => {
    const bodies: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (method === "POST" && path === "/auth/invite") {
        const code = (body as { code: string }).code;
        return code === "good-code" ? { status: 200, body: { role: "requester", instanceName: "Kino", label: "Family" } }
          : { status: 404, body: { code: "invite_invalid", message: "" } };
      }
      if (method === "POST" && path === "/auth/signup") {
        bodies.push(body);
        return { status: 200, body: { user: requester() } };
      }
      if (path === "/discover") return { status: 200, body: { metadataReady: true, musicReady: true, requests: [], rows: [] } };
      if (path === "/notifications") return emptyNotifications;
    });
    const { router, queryClient } = renderApp("/join");
    await userEvent.type(await screen.findByTestId("join-code"), "bad");
    await userEvent.click(screen.getByTestId("join-check"));
    expect(await screen.findByText(/not valid any more/)).toBeInTheDocument();
    await userEvent.clear(screen.getByTestId("join-code"));
    await userEvent.type(screen.getByTestId("join-code"), "good-code");
    await userEvent.click(screen.getByTestId("join-check"));
    expect(await screen.findByText("Join Kino")).toBeInTheDocument();
    await userEvent.type(screen.getByTestId("join-username"), "mia");
    await userEvent.type(screen.getByTestId("join-password"), "mia-password");
    await userEvent.type(screen.getByTestId("join-repeat"), "mia-passwor");
    expect(screen.getByTestId("join-submit")).toBeDisabled();
    await userEvent.type(screen.getByTestId("join-repeat"), "d");
    await userEvent.click(screen.getByTestId("join-submit"));
    await waitFor(() => expect(router.state.location.pathname).toBe("/discover"));
    expect(bodies[0]).toMatchObject({ code: "good-code", username: "mia", password: "mia-password", useCookie: true });
    expect(queryClient.getQueryData(["auth", "me"])).toMatchObject({ username: "mia" });
  });
});
