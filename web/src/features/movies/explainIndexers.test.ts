import i18n from "@/i18n";
import { describeExplanation } from "./timeline";

describe("nothing found with failing indexers", () => {
  it("names the indexers that failed in the last search and why", async () => {
    await i18n.changeLanguage("en");
    const text = describeExplanation(i18n.t, { code: "nothing_found", tone: "warning", message: "", reasons: [],
      params: { searches: 3, indexerErrors: [{ name: "Nyaa", code: "layout" }, { name: "1337x", code: "challenge" }] } }, "en");
    expect(text).toContain("Failing in the last search: Nyaa (layout changed), 1337x (browser check).");
    await i18n.changeLanguage("hr");
    expect(describeExplanation(i18n.t, { code: "nothing_found", tone: "warning", message: "", reasons: [],
      params: { searches: 1, indexerErrors: [{ name: "EZTV", code: "unreachable" }] } }, "hr")).toContain("EZTV (stranica ne radi)");
    await i18n.changeLanguage("en");
  });
});
