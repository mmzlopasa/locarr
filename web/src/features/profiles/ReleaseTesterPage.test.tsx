import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ParseResult, QualityProfile } from "@/api/client";
import { mockApi, renderApp, testUser } from "@/test-utils";

const profile: QualityProfile = { id: 3, name: "HD-1080p", kind: "movies", items: [{ quality: "bluray-1080p", allowed: true }], cutoff: "bluray-1080p",
  upgradesAllowed: true, minFormatScore: 0, cutoffFormatScore: 0, formatScores: [], languages: [] };

const result = (withDecision: boolean): ParseResult => ({
  release: { input: "x", kind: "movie", title: "Dune Part Two", year: 2024, video: { source: "bluray", resolution: 1080, codec: "h265", encoder: "x265" },
    audio: { codecs: ["truehd"], channels: "7.1", atmos: true }, revision: { version: 1 }, group: "GRP", confidence: 0.95, unparsed: ["WEIRD"] },
  quality: { id: "bluray-1080p", name: "Bluray-1080p", source: "bluray", resolution: 1080, music: false, rank: 21 },
  customFormats: [
    { id: 1, name: "x265 (HD)", matched: true, score: withDecision ? -10000 : 0,
      conditions: [{ name: "x265", implementation: "ReleaseTitleSpecification", required: true, negate: false, passed: true }] },
    { id: 2, name: "Remux Tier", matched: false, score: 0, conditions: [] },
  ],
  score: withDecision ? -10000 : 0,
  decision: withDecision ? {
    candidate: { title: "x" }, release: { input: "x", kind: "movie", title: "Dune Part Two", video: {}, audio: {}, revision: { version: 1 }, confidence: 1 },
    quality: { id: "bluray-1080p", name: "Bluray-1080p", source: "bluray", rank: 21, music: false }, customFormats: [], score: -10000, status: "rejected",
    rejections: [
      { code: "custom_format_score_too_low", message: "custom format score -10000 is below the minimum 0", params: { score: -10000, minimum: 0 } },
      { code: "size_too_large", message: "x", params: { size: 30 * 1024 ** 3, limit: 25 * 1024 ** 3, quality: "Bluray-1080p", runtime: 166 } },
    ],
    rankKey: { qualityPosition: 0, score: -10000, revision: 10, protocolPreferred: false, indexerPriority: 25, seeders: 0, sizeDistance: -1, ageMinutes: 0 },
  } : undefined,
});

describe("release tester", () => {
  it("parses as you type and explains the decision for a profile", async () => {
    const bodies: Record<string, unknown>[] = [];
    mockApi(({ method, path, body }) => {
      if (method === "GET" && path === "/qualityprofiles") return { status: 200, body: [profile] };
      if (method === "POST" && path === "/parse") {
        const b = body as Record<string, unknown>;
        bodies.push(b);
        return { status: 200, body: result(b.profileId !== undefined) };
      }
    });
    renderApp("/settings/tester", { me: testUser() });
    const input = await screen.findByTestId("tester-input");
    await userEvent.type(input, "Dune.Part.Two.2024.1080p.BluRay.x265-GRP");
    expect(await screen.findByTestId("tester-title")).toHaveTextContent("Dune Part Two");
    expect(screen.getByTestId("tester-quality")).toHaveTextContent("Bluray-1080p");
    expect(screen.getByTestId("tester-unparsed")).toHaveTextContent("WEIRD");
    expect(screen.getByTestId("tester-confidence")).toHaveTextContent("95%");
    expect(screen.queryByTestId("tester-verdict")).not.toBeInTheDocument();
    // typing is debounced: far fewer requests than characters
    expect(bodies.length).toBeLessThan(5);

    await userEvent.selectOptions(screen.getByTestId("tester-profile"), "3");
    await userEvent.type(screen.getByLabelText("Size (GB)"), "30");
    const verdict = await screen.findByTestId("tester-verdict");
    expect(verdict).toHaveTextContent("Would be rejected");
    const reasons = within(verdict).getByTestId("tester-rejections");
    expect(reasons).toHaveTextContent("Custom format score -10000 is below the minimum 0.");
    expect(reasons).toHaveTextContent("30 GB > 25 GB maximum for Bluray-1080p (2 h 46 min).");
    await waitFor(() => expect(bodies.at(-1)).toMatchObject({ profileId: 3, size: 30 * 1024 ** 3, protocol: "torrent" }));

    const formats = screen.getByTestId("tester-formats");
    await userEvent.click(within(formats).getByRole("button", { name: /x265 \(HD\)/ }));
    expect(within(formats).getByText("Release title")).toBeInTheDocument();
  });

  it("shows the server's reason when parsing fails validation", async () => {
    mockApi(({ method, path }) => {
      if (method === "GET" && path === "/qualityprofiles") return { status: 200, body: [] };
      if (method === "POST" && path === "/parse") return { status: 400, body: { code: "validation", message: "title too long" } };
    });
    renderApp("/settings/tester", { me: testUser() });
    await userEvent.type(await screen.findByTestId("tester-input"), "x");
    expect(await screen.findByTestId("error-detail")).toHaveTextContent("title too long");
  });
});
