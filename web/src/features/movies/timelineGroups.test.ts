import type { TimelineEvent } from "@/api/client";
import i18n from "@/i18n";
import { reasonChip, describeExplanation } from "./timeline";
import { cycleSummary, groupTimeline, rowIds } from "./timelineGroups";

const at = (s: number) => new Date(Date.UTC(2026, 10, 25, 10, 0, s)).toISOString();

function search(id: number, s: number, query: string, indexers: { name: string; status: string; count: number }[], ref = "scene:1"): TimelineEvent {
  const count = indexers.reduce((n, i) => n + (i.status === "ok" ? i.count : 0), 0);
  return { id, at: at(s), type: "search", source: "search", itemRef: ref, message: query,
    data: { source: "automatic", query, resultCount: count, finishedAt: at(s + 1), indexers } };
}
const decision = (id: number, s: number, ref = "scene:1"): TimelineEvent => ({ id, at: at(s), type: "decision", source: "timeline", itemRef: ref, message: "",
  data: { reason: "scheduled", summary: { candidates: 2, accepted: 0, pending: 0, rejected: 2, topRejected: [] } } });

describe("item timeline grouping", () => {
  // Newest first, as the server sends it: two cycles of four query forms.
  const cycle = (base: number, firstID: number) => [
    decision(firstID + 10, base + 10),
    search(firstID + 3, base + 8, "Performer Title", [{ name: "BitSearch", status: "ok", count: 0 }, { name: "Sukebei", status: "ok", count: 0 }]),
    search(firstID + 2, base + 6, "Performer One Title", [{ name: "BitSearch", status: "ok", count: 1 }, { name: "Sukebei", status: "error", count: 0 }]),
    search(firstID + 1, base + 4, "StudioA 24.01.15", [{ name: "BitSearch", status: "ok", count: 0 }, { name: "Sukebei", status: "ok", count: 1 }]),
    search(firstID, base + 2, "StudioA 24 01 15", [{ name: "BitSearch", status: "ok", count: 0 }, { name: "Sukebei", status: "error", count: 0 }]),
  ];
  const added: TimelineEvent = { id: 1, at: at(0), type: "added", source: "timeline", itemRef: "scene:1", message: "", data: {} };
  const events = [...cycle(400, 200), ...cycle(20, 100), added];

  it("folds one cycle's searches and its decision into one row", () => {
    const rows = groupTimeline(events);
    expect(rows.map((r) => r.kind)).toEqual(["cycle", "cycle", "event"]);
    const first = rows[0]!;
    if (first.kind !== "cycle") throw new Error("cycle");
    expect(first.searches).toHaveLength(4);
    expect(first.decision?.id).toBe(210);
    expect(rowIds(first)).toEqual(["t:210", "s:203", "s:202", "s:201", "s:200"]);
  });

  it("adds the indexer counts up over the query forms and keeps the order of the queries", () => {
    const row = groupTimeline(events)[0]!;
    if (row.kind !== "cycle") throw new Error("cycle");
    const sum = cycleSummary(row);
    expect(sum.total).toBe(2);
    expect(sum.indexers).toEqual([{ name: "BitSearch", status: "ok", count: 1 }, { name: "Sukebei", status: "ok", count: 1 }]);
    expect(sum.queries).toEqual(["StudioA 24 01 15", "StudioA 24.01.15", "Performer One Title", "Performer Title"]);
  });

  it("keeps searches apart when they are far apart, of another item, or an RSS decision", () => {
    const far = [search(2, 600, "b", []), search(1, 0, "a", [])];
    expect(groupTimeline(far).map((r) => r.kind === "cycle" && r.searches.length)).toEqual([1, 1]);
    const other = [search(2, 3, "b", [], "episode:2"), search(1, 0, "a", [], "episode:1")];
    expect(groupTimeline(other)).toHaveLength(2);
    const rss = { ...decision(3, 5), data: { reason: "rss", summary: {} } };
    const rows = groupTimeline([rss, search(1, 0, "a", [])]);
    expect(rows.map((r) => r.kind)).toEqual(["event", "cycle"]);
  });

  it("never drops a row", () => {
    const ids = groupTimeline(events).flatMap(rowIds);
    expect(ids).toHaveLength(events.length);
  });
});

describe("why it was rejected", () => {
  it("names the quality and the profile, and the blocked term and its list", async () => {
    await i18n.changeLanguage("en");
    const quality = { code: "quality_not_allowed", count: 2, example: "Unknown is not allowed in profile HD-1080p",
      params: { quality: "Unknown", profile: "HD-1080p" }, values: ["Unknown"] };
    const text = describeExplanation(i18n.t, { code: "all_rejected", tone: "warning", message: "", params: { searches: 1, candidates: 2 },
      reasons: [quality] }, "en");
    expect(text).toContain("Most common reason: Unknown is not allowed in profile HD-1080p.");
    expect(reasonChip(i18n.t, quality)).toBe("Unknown not allowed in HD-1080p");
    expect(reasonChip(i18n.t, { ...quality, values: ["Unknown", "WEBDL-480p"] })).toBe("Unknown, WEBDL-480p not allowed in HD-1080p");
    const own = { code: "blocked_tag", count: 1, example: "", params: { term: "ownterm", protected: false }, values: ["ownterm"] };
    expect(reasonChip(i18n.t, own)).toBe("Blocked term “ownterm”");
    expect(reasonChip(i18n.t, { ...own, params: { term: "child", protected: true }, values: ["child"] })).toBe("Always blocked: “child”");
    // Summaries from before keep the short label.
    expect(reasonChip(i18n.t, { code: "quality_not_allowed", count: 2, example: "x" })).toBe("quality not allowed");
    await i18n.changeLanguage("hr");
    expect(reasonChip(i18n.t, quality)).toBe("Unknown nije dopušteno u HD-1080p");
    await i18n.changeLanguage("en");
  });
});
