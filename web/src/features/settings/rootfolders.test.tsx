import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { ApiError } from "@/api/client";
import { ErrorAlert } from "@/components/ui";
import { errorDetail } from "@/components/errors";
import { mockApi, renderApp, testUser } from "@/test-utils";

describe("error details", () => {
  it("shows what the download client said under the translated sentence", () => {
    render(<ErrorAlert error={new ApiError(502, "client_error", "qBittorrent refused the category name \"movies\"")} />);
    expect(screen.getByRole("alert")).toHaveTextContent("The download client returned an error.");
    expect(screen.getByTestId("error-detail")).toHaveTextContent("qBittorrent refused the category name \"movies\"");
  });

  it("keeps sign-in and path errors to the translated sentence, with the /data hint", () => {
    expect(errorDetail(new ApiError(401, "invalid_credentials", "invalid username or password"))).toBe("");
    render(<ErrorAlert error={new ApiError(400, "path_not_allowed", "path is outside the allowed folders")} />);
    expect(screen.getByRole("alert")).toHaveTextContent("/data/movies");
    expect(screen.queryByTestId("error-detail")).toBeNull();
  });
});

describe("media folders", () => {
  it("says why a folder is read-only and links to the fix", async () => {
    mockApi((req) => {
      if (req.path === "/rootfolders") {
        return { status: 200, body: [{ id: 1, path: "/data/movies", kind: "movies", accessible: true, writable: false,
          notWritableReason: "read_only_filesystem", notWritableDetail: "open /data/movies/.locarr-write-test: read-only file system",
          freeSpaceBytes: 1, totalSpaceBytes: 2, createdAt: new Date().toISOString() }] };
      }
      return undefined;
    });
    renderApp("/settings/media", { me: testUser() });
    const problem = await screen.findByTestId("rootfolder-problem");
    expect(problem).toHaveTextContent("mounted read-only");
    expect(problem).toHaveTextContent("read-only file system");
    expect(screen.getByRole("link", { name: "How to fix" })).toHaveAttribute("href", expect.stringContaining("troubleshooting.md#permissions"));
  });
});
