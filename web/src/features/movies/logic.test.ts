import i18n from "i18next";
import "@/i18n";
import type { Explanation, TimelineEvent } from "@/api/client";
import { columnsFor, pagesFor, visibleRows } from "./virtual";
import { describeEvent, describeExplanation, eventTone } from "./timeline";
import { monthGrid, rangeFor, startOfWeek, step, ymd } from "@/features/calendar/dates";

describe("virtual list maths", () => {
  it("renders only the visible rows plus overscan", () => {
    expect(visibleRows(0, 400, 40, 10_000)).toEqual({ first: 0, last: 13 });
    expect(visibleRows(40_000, 400, 40, 10_000)).toEqual({ first: 997, last: 1013 });
    expect(visibleRows(0, 400, 40, 0)).toEqual({ first: 0, last: -1 });
    expect(visibleRows(1e9, 400, 40, 5)).toEqual({ first: 4, last: 4 });
  });
  it("loads the pages that hold the visible items", () => {
    expect(pagesFor(0, 50, 200)).toEqual([0]);
    expect(pagesFor(190, 410, 200)).toEqual([0, 1, 2]);
    expect(pagesFor(5, 4, 200)).toEqual([0]);
  });
  it("fits poster columns to the width", () => {
    expect(columnsFor(1000, 150, 12)).toBe(6);
    expect(columnsFor(100, 150, 12)).toBe(1);
  });
});

describe("calendar dates", () => {
  it("starts weeks on Monday and covers six weeks per month", () => {
    expect(ymd(startOfWeek(new Date(2026, 10, 22)))).toBe("2026-11-16"); // a Sunday
    const g = monthGrid(new Date(2026, 1, 10));
    expect(g).toHaveLength(42);
    expect(ymd(g[0]!)).toBe("2026-01-26");
    expect(rangeFor("week", new Date(2026, 10, 18))).toEqual({ start: "2026-11-16", end: "2026-11-22" });
    expect(ymd(step("month", new Date(2026, 11, 31), 1))).toBe("2027-01-01");
  });
});

describe("timeline texts", () => {
  const ev = (type: string, data: unknown, message = ""): TimelineEvent => ({ id: 1, at: "2026-11-21T10:00:00Z", type, source: "timeline", message, data });
  it("describes events in both languages", async () => {
    await i18n.changeLanguage("en");
    const t = i18n.t.bind(i18n);
    expect(describeEvent(t, ev("search", { source: "automatic", resultCount: 3, indexers: [{ name: "A", count: 3, status: "ok" }, { name: "B", count: 0, status: "error" }] })))
      .toBe("Automatic search: 3 releases (A: 3, B: error).");
    expect(describeEvent(t, ev("file_imported", { quality: "Bluray-1080p", mode: "hardlink", path: "/m/x.mkv" })))
      .toBe("Imported as Bluray-1080p (Hardlink (copy when not possible)): /m/x.mkv");
    expect(describeEvent(t, ev("decision", { reason: "added", summary: { candidates: 4, accepted: 1, pending: 0, rejected: 3, best: "X" } })))
      .toContain("Best: X.");
    expect(describeEvent(t, ev("something_new", {}, "server text"))).toBe("server text");
    expect(eventTone(ev("download_failed", {}))).toBe("danger");
    await i18n.changeLanguage("hr");
    expect(describeEvent(i18n.t.bind(i18n), ev("renamed", { from: "/a/old.mkv", to: "/a/new.mkv" }))).toBe("Preimenovano old.mkv → new.mkv");
    await i18n.changeLanguage("en");
  });
  it("explains why a movie is not here", async () => {
    await i18n.changeLanguage("en");
    const t = i18n.t.bind(i18n);
    const ex: Explanation = { code: "all_rejected", tone: "warning", message: "x", params: { searches: 3, candidates: 5 },
      reasons: [{ code: "size_too_large", count: 4, example: "big" }] };
    expect(describeExplanation(t, ex, "en")).toBe(
      "Searched 3 times; the last search found 5 releases and rejected all of them. Most common reason: too large");
    expect(describeExplanation(t, { ...ex, code: "downloading", params: { progress: 0.42, title: "Film", state: "downloading" }, reasons: [] }, "en"))
      .toBe("Downloading Film: 42% (Downloading).");
    expect(describeExplanation(t, { ...ex, code: "brand_new_code" }, "en")).toBe("x");
  });
});
