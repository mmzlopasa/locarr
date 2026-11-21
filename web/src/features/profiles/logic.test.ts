import i18n from "@/i18n";
import type { QualityProfileItem } from "@/api/client";
import { clampDefinition, cutoffAfterChange, fixCutoff, formatSize, groupWithNext, itemKey, limitBytes, moveItem, parseTerms, previewRange, termError, ungroup } from "./logic";
import { describeRejection } from "./rejections";

const items = (): QualityProfileItem[] => [
  { quality: "bluray-1080p", allowed: true },
  { quality: "webdl-1080p", allowed: true },
  { quality: "webrip-1080p", allowed: false },
  { quality: "hdtv-720p", allowed: false },
];

describe("profile items", () => {
  it("moves items and keeps the others in order", () => {
    expect(moveItem(items(), 0, 2).map(itemKey)).toEqual(["webdl-1080p", "webrip-1080p", "bluray-1080p", "hdtv-720p"]);
    expect(moveItem(items(), 0, 9)).toHaveLength(4);
  });
  it("groups with the next row and splits again", () => {
    const g = groupWithNext(items(), 1, "WEB 1080p");
    expect(g).toHaveLength(3);
    expect(g[1]).toEqual({ name: "WEB 1080p", qualities: ["webdl-1080p", "webrip-1080p"], allowed: true });
    expect(itemKey(g[1]!)).toBe("group:WEB 1080p");
    expect(ungroup(g, 1).map(itemKey)).toEqual(items().map(itemKey));
  });
  it("keeps the cutoff on an allowed item", () => {
    expect(fixCutoff(items(), "webrip-1080p")).toBe("bluray-1080p");
    expect(fixCutoff(items(), "webdl-1080p")).toBe("webdl-1080p");
    const before = groupWithNext(items(), 1, "WEB");
    expect(cutoffAfterChange(before, ungroup(before, 1), "group:WEB")).toBe("webdl-1080p");
    expect(cutoffAfterChange(items(), groupWithNext(items(), 1, "WEB"), "webdl-1080p")).toBe("group:WEB");
  });
});

describe("sizes", () => {
  it("previews limits for a runtime", () => {
    expect(limitBytes(1, 1)).toBe(1024 * 1024);
    expect(formatSize(12 * 1024 ** 3, "en")).toBe("12 GB");
    expect(formatSize(500 * 1024 ** 2, "en")).toBe("500 MB");
    expect(formatSize(1.5 * 1024 ** 3, "hr")).toBe("1,5 GB");
    const p = previewRange({ min: 10, preferred: 0, max: 0 }, 110, "en");
    expect(p).toEqual({ min: "1.1 GB", preferred: null, max: null });
  });
  it("keeps min ≤ preferred ≤ max", () => {
    expect(clampDefinition({ min: 5, preferred: 10, max: 20 }, "min", 25)).toEqual({ min: 25, preferred: 25, max: 25 });
    expect(clampDefinition({ min: 5, preferred: 10, max: 20 }, "max", 3)).toEqual({ min: 3, preferred: 3, max: 3 });
    expect(clampDefinition({ min: 5, preferred: 10, max: 0 }, "preferred", 50)).toEqual({ min: 5, preferred: 50, max: 0 });
  });
});

describe("terms", () => {
  it("splits lines and checks regular expressions", () => {
    expect(parseTerms(" x265 \n\n/hevc/\n")).toEqual(["x265", "/hevc/"]);
    expect(termError("plain")).toBeNull();
    expect(termError("/(?<!DD)\\+/")).toBeNull();
    expect(termError("/[/")).not.toBeNull();
  });
});

describe("rejections", () => {
  it("translates with the values and falls back to the server message", async () => {
    await i18n.changeLanguage("hr");
    const t = i18n.t.bind(i18n);
    const big = { code: "size_too_large", message: "x", params: { size: 18.4 * 1024 ** 3, limit: 12 * 1024 ** 3, quality: "Bluray-1080p", runtime: 110 } };
    expect(describeRejection(t, big, "hr")).toBe("18,4 GB > 12 GB najviše za Bluray-1080p (1 h 50 min).");
    expect(describeRejection(t, { code: "something_new", message: "server says" }, "hr")).toBe("server says");
    expect(describeRejection(t, { code: "delayed", message: "", temporary: true,
      params: { remainingMinutes: 30, delayMinutes: 60, protocol: "torrent", profile: "Default" } }, "hr")).toContain("30 min");
    await i18n.changeLanguage("en");
    expect(describeRejection(i18n.t.bind(i18n), { code: "wrong_kind", message: "", params: { got: "series", want: "movies" } }, "en"))
      .toBe("This is Series, not Movies.");
  });
});
