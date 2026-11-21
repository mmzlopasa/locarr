import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { QualityProfile } from "@/api/client";
import { mockApi, renderApp, testUser } from "@/test-utils";

const qualities = [
  { id: "webdl-1080p", name: "WEBDL-1080p", source: "webdl", resolution: 1080, music: false, rank: 1 },
  { id: "bluray-1080p", name: "Bluray-1080p", source: "bluray", resolution: 1080, music: false, rank: 2 },
  { id: "remux-1080p", name: "Remux-1080p", source: "bluray", resolution: 1080, modifier: "remux", music: false, rank: 3 },
];
const profile: QualityProfile = { id: 7, name: "HD", kind: "movies", cutoff: "bluray-1080p", upgradesAllowed: true, minFormatScore: 0,
  cutoffFormatScore: 0, formatScores: [], languages: [],
  items: [{ quality: "remux-1080p", allowed: false }, { quality: "bluray-1080p", allowed: true }, { quality: "webdl-1080p", allowed: true }] };

describe("quality profiles", () => {
  it("reorders, groups, picks the cutoff and saves", async () => {
    let saved: Record<string, unknown> | undefined;
    mockApi(({ method, path, body }) => {
      if (path === "/qualities") return { status: 200, body: qualities };
      if (path === "/qualityprofiles" && method === "GET") return { status: 200, body: [profile] };
      if (path === "/customformats") return { status: 200, body: [{ id: 4, name: "x265", includeWhenRenaming: false, specifications: [], unsupported: [] }] };
      if (path === "/languages") return { status: 200, body: [{ code: "hr", name: "Croatian", native: "Hrvatski" }] };
      if (path === "/qualityprofiles/7" && method === "PUT") {
        saved = body as Record<string, unknown>;
        return { status: 200, body: profile };
      }
    });
    renderApp("/settings/profiles", { me: testUser() });
    await userEvent.click(await screen.findByTestId("profile-movies-HD"));
    const editor = await screen.findByTestId("profile-editor");
    // Group Bluray with WEB-DL (the cutoff moves onto the group), then move the group above Remux.
    await userEvent.click(within(screen.getByTestId("item-Bluray-1080p")).getByRole("button", { name: "Group with the next row" }));
    await userEvent.click(within(screen.getByTestId("item-Bluray-1080p group")).getByRole("button", { name: "Move up" }));
    await userEvent.click(within(editor).getByLabelText("Allow Remux-1080p"));
    await userEvent.clear(within(editor).getByLabelText("Score for x265"));
    await userEvent.type(within(editor).getByLabelText("Score for x265"), "-500");
    await userEvent.selectOptions(within(editor).getByLabelText("Add a language"), "hr");
    await userEvent.click(within(editor).getByRole("button", { name: "Add" }));
    await userEvent.click(within(editor).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(saved).toBeDefined());
    expect(saved).toMatchObject({
      items: [{ name: "Bluray-1080p group", qualities: ["bluray-1080p", "webdl-1080p"], allowed: true }, { quality: "remux-1080p", allowed: true }],
      cutoff: "group:Bluray-1080p group", formatScores: [{ formatId: 4, score: -500 }], languages: ["hr"],
    });
  });
});

describe("custom formats", () => {
  it("checks pasted JSON before importing it", async () => {
    const calls: Record<string, unknown>[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/customformats" && method === "GET") return { status: 200, body: [] };
      if (path === "/customformats/import") {
        const b = body as Record<string, unknown>;
        calls.push(b);
        return { status: 200, body: { formats: [{ id: 1, name: "x265", includeWhenRenaming: false, specifications: [], unsupported: [] }],
          warnings: ["x265: condition uses Mystery"], stored: !b.dryRun } };
      }
    });
    renderApp("/settings/customformats", { me: testUser() });
    await userEvent.click(await screen.findByRole("button", { name: "Import JSON" }));
    await userEvent.click(screen.getByTestId("import-json"));
    await userEvent.paste('{"name":"x265","specifications":[]}');
    await userEvent.selectOptions(screen.getByLabelText("Numbering of values"), "series");
    await userEvent.click(screen.getByRole("button", { name: "Check" }));
    expect(await screen.findByText("1 formats are valid; nothing was saved yet.")).toBeInTheDocument();
    expect(screen.getByText("x265: condition uses Mystery")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Import" }));
    expect(await screen.findByText("Imported 1 formats.")).toBeInTheDocument();
    expect(calls).toEqual([
      { json: '{"name":"x265","specifications":[]}', flavor: "series", replace: false, dryRun: true },
      { json: '{"name":"x265","specifications":[]}', flavor: "series", replace: false, dryRun: false },
    ]);
  });
});

describe("quality definitions", () => {
  it("previews sizes for a runtime and saves only changes", async () => {
    let saved: unknown;
    mockApi(({ method, path, body }) => {
      if (path === "/qualities") return { status: 200, body: qualities };
      if (path === "/qualitydefinitions" && method === "GET")
        return { status: 200, body: [{ kind: "movies", quality: "bluray-1080p", min: 10, preferred: 0, max: 100 }] };
      if (path === "/qualitydefinitions" && method === "PUT") {
        saved = body;
        return { status: 200, body: [{ kind: "movies", quality: "bluray-1080p", min: 10, preferred: 0, max: 50 }] };
      }
    });
    renderApp("/settings/quality", { me: testUser() });
    const row = await screen.findByTestId("def-bluray-1080p");
    expect(within(row).getByTestId("def-preview")).toHaveTextContent("1.1 GB – 10.7 GB");
    await userEvent.click(screen.getByRole("button", { name: "45 min" }));
    expect(within(row).getByTestId("def-preview")).toHaveTextContent("450 MB – 4.4 GB");
    await userEvent.click(within(row).getByRole("button", { expanded: false }));
    const max = within(row).getByRole("spinbutton", { name: "Maximum" });
    await userEvent.clear(max);
    await userEvent.type(max, "50");
    await userEvent.click(screen.getByRole("button", { name: "Save 1 changes" }));
    await waitFor(() => expect(saved).toEqual({ definitions: [{ kind: "movies", quality: "bluray-1080p", min: 10, preferred: 0, max: 50 }] }));
  });
});
