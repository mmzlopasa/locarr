import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import i18n from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { QueryClient } from "@tanstack/react-query";
import { createMemoryHistory } from "@tanstack/react-router";
import { createAppRouter } from "@/app/router";
import { activeItem, filterNav, loadCollapsed, normalize, saveCollapsed, SETTINGS_NAV, visibleNav } from "./settingsNav";

const en = i18n.getFixedT("en");
const hr = i18n.getFixedT("hr");
const keysOf = (groups: ReturnType<typeof visibleNav>) => groups.flatMap((g) => g.items.map((i) => i.key));

describe("settings navigation model", () => {
  it("groups every settings page exactly once", () => {
    const paths = SETTINGS_NAV.flatMap((g) => g.items.map((i) => i.to));
    expect(new Set(paths).size).toBe(paths.length);
    // Every child route of /settings has an entry, so no page becomes unreachable.
    const router = createAppRouter(new QueryClient(), createMemoryHistory());
    const settings = Object.keys(router.routesByPath)
      .map((p) => p.replace(/\/$/, ""))
      .filter((p) => p === "/settings" || p.startsWith("/settings/"));
    expect(settings.length).toBeGreaterThan(10);
    for (const p of settings) expect(paths, p).toContain(p);
    for (const g of SETTINGS_NAV) expect(g.items.length).toBeGreaterThan(0);
  });

  it("shows administrators everything and other roles nothing", () => {
    expect(keysOf(visibleNav(testUser()))).toHaveLength(SETTINGS_NAV.flatMap((g) => g.items).length);
    expect(visibleNav(testUser({ role: "manager" }))).toEqual([]);
    expect(visibleNav(testUser({ role: "requester" }))).toEqual([]);
  });

  it("filters by label, group, keywords and every word, ignoring case and diacritics", () => {
    const all = visibleNav(testUser());
    expect(keysOf(filterNav(all, "custom", en))).toEqual(["customFormats"]);
    expect(keysOf(filterNav(all, "  ", en))).toEqual(keysOf(all));
    // A field inside the page: naming lives in media management.
    expect(keysOf(filterNav(all, "rename", en))).toEqual(["mediaManagement"]);
    expect(keysOf(filterNav(all, "delay", en))).toEqual(["rules"]);
    // Group names match their items.
    expect(keysOf(filterNav(all, "quality", en))).toEqual(expect.arrayContaining(["profiles", "quality", "customFormats", "rules", "tester"]));
    // Croatian, with or without diacritics.
    expect(keysOf(filterNav(all, "prilagodeni", hr))).toEqual(["customFormats"]);
    expect(keysOf(filterNav(all, "Općenito", hr))).toEqual(["general"]);
    expect(keysOf(filterNav(all, "titl jezici", hr))).toEqual(["subtitles"]);
    expect(filterNav(all, "zzzz", en)).toEqual([]);
    expect(normalize(" ČĆŽĐ ")).toBe("cczd");
  });

  it("picks the item for the current address, longest path first", () => {
    const all = visibleNav(testUser());
    expect(activeItem(all, "/settings")?.key).toBe("general");
    expect(activeItem(all, "/settings/")?.key).toBe("general");
    expect(activeItem(all, "/settings/customformats")?.key).toBe("customFormats");
    expect(activeItem(all, "/settings/system")?.key).toBe("monitoring");
    expect(activeItem(all, "/system/status")?.key).toBe("system");
    expect(activeItem(all, "/movies")).toBeUndefined();
  });

  it("remembers folded groups and survives broken storage", () => {
    saveCollapsed(["quality"]);
    expect(loadCollapsed()).toEqual(["quality"]);
    localStorage.setItem("locarr.settingsNav.collapsed", "{not json");
    expect(loadCollapsed()).toEqual([]);
    const spy = vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("denied");
    });
    expect(loadCollapsed()).toEqual([]);
    spy.mockRestore();
  });
});

describe("settings navigation", () => {
  beforeEach(() => {
    mockApi(({ path }) => (path === "/settings" ? { status: 200, body: { instanceName: "Home", tmdbApiKey: { configured: false } } } : undefined));
  });

  it("marks the current page and filters with the keyboard", async () => {
    const { router } = renderApp("/settings", { me: testUser() });
    const nav = await screen.findByTestId("rail-settings-nav");
    expect(within(nav).getByRole("link", { name: "General" })).toHaveAttribute("aria-current", "page");
    expect(within(nav).getByRole("link", { name: "Custom formats" })).not.toHaveAttribute("aria-current");

    const filter = within(nav).getByRole("searchbox", { name: "Filter settings" });
    await userEvent.type(filter, "custom");
    expect(within(nav).getAllByRole("link")).toHaveLength(1);
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(router.state.location.pathname).toBe("/settings/customformats"));
    expect(filter).toHaveValue("");

    // Arrow keys walk from the filter through the links.
    filter.focus();
    await userEvent.keyboard("{ArrowDown}");
    expect(within(nav).getByRole("link", { name: "General" })).toHaveFocus();
    await userEvent.keyboard("{ArrowDown}");
    expect(within(nav).getByRole("link", { name: "Monitoring" })).toHaveFocus();
  });

  it("folds a group and remembers it", async () => {
    renderApp("/settings", { me: testUser() });
    const nav = await screen.findByTestId("rail-settings-nav");
    await userEvent.click(within(nav).getByRole("button", { name: "Quality" }));
    expect(within(nav).queryByRole("link", { name: "Custom formats" })).not.toBeInTheDocument();
    expect(loadCollapsed()).toEqual(["quality"]);
    // A search shows matches in folded groups too.
    await userEvent.type(within(nav).getByRole("searchbox"), "custom");
    expect(within(nav).getByRole("link", { name: "Custom formats" })).toBeInTheDocument();
  });

  it("opens the sections as a sheet from the compact bar and closes it after choosing", async () => {
    const { router } = renderApp("/settings/customformats", { me: testUser() });
    const toggle = await screen.findByTestId("settings-nav-toggle");
    expect(toggle).toHaveTextContent("Custom formats");
    await userEvent.click(toggle);
    const sheet = screen.getByRole("dialog", { name: "Settings" });
    expect(within(sheet).getByRole("link", { name: "Custom formats" })).toHaveFocus();
    await userEvent.click(within(sheet).getByRole("link", { name: "Users" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/settings/users"));
    expect(screen.queryByRole("dialog", { name: "Settings" })).not.toBeInTheDocument();
  });
});
