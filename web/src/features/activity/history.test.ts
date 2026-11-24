import type { HistoryEntry } from "@/api/client";
import { entryTitle, groupHistory, shortenUrls } from "./history";

const t = ((k: string) => k) as never;
const at = (min: number) => new Date(Date.UTC(2026, 10, 24, 12, 0) - min * 60_000).toISOString();
const fail = (id: string, min: number, over: Partial<HistoryEntry> = {}): HistoryEntry => ({
  id, at: at(min), category: "failed", type: "grab_failed", itemRef: "movie:7", title: "Iron Man (2008)", itemTitle: "Iron Man (2008)",
  message: "could not grab", indexer: "X", data: { code: "indexer_limited", indexer: "X" }, ...over,
});

describe("groupHistory", () => {
  it("folds consecutive failures of one kind, item and cycle", () => {
    const rows = groupHistory([
      fail("t:9", 0), fail("t:8", 1), fail("t:7", 2),
      fail("t:6", 3, { data: { code: "release_gone", indexer: "X" } }),
      fail("t:5", 4, { itemRef: "movie:8" }),
      fail("t:4", 60), fail("t:3", 61),
      { id: "d:1", at: at(62), category: "grabbed", type: "download_grabbed", itemRef: "movie:7", title: "Iron Man (2008)", message: "" },
      { id: "d:2", at: at(63), category: "grabbed", type: "download_grabbed", itemRef: "movie:7", title: "Iron Man (2008)", message: "" },
    ]);
    expect(rows.map((r) => r.entries.map((e) => e.id))).toEqual([
      ["t:9", "t:8", "t:7"], ["t:6"], ["t:5"], ["t:4", "t:3"], ["d:1"], ["d:2"],
    ]);
    expect(rows[0]?.first.id).toBe("t:9");
  });

  it("starts a new group after a pause longer than one cycle", () => {
    expect(groupHistory([fail("a", 0), fail("b", 30)]).length).toBe(2);
  });
});

describe("shortenUrls", () => {
  it("keeps host and path and hides the query", () => {
    const long = "x".repeat(300);
    expect(shortenUrls(`request https://pro.example.net/8/download?apikey=[redacted]&link=${long}: redirect to another host refused`))
      .toBe("request pro.example.net/8/download?…: redirect to another host refused");
    expect(shortenUrls("see http://host.test/.")).toBe("see host.test.");
    expect(shortenUrls("no links")).toBe("no links");
  });
});

describe("entryTitle", () => {
  it("prefers the stored title and marks removed items", () => {
    expect(entryTitle(t, fail("a", 0))).toBe("Iron Man (2008)");
    expect(entryTitle(t, fail("a", 0, { itemTitle: "", title: "Iron.Man.2008.720p", itemGone: true, itemRef: "movie:7#removed" })))
      .toBe("history.removedItem");
    expect(entryTitle(t, fail("a", 0, { itemTitle: undefined, title: "Old server title" }))).toBe("Old server title");
  });
});
