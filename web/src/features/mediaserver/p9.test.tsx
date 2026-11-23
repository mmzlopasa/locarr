import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { NotificationChannel, StreameroSettings } from "@/api/client";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { emptyNotifications, item, requester, summary, titleDetail } from "@/features/discover/fixtures";
import { toInput } from "@/features/notifications/channels";

const now = new Date().toISOString();
const watch = "https://media.example.com/item/0190a8b0-0000-7000-8000-000000000001";

const streamero = (over: Partial<StreameroSettings> = {}): StreameroSettings => ({
  enabled: true, url: "http://streamero:8787", publicUrl: "", apiKey: { configured: true }, pathMappings: [],
  libraryMap: [], signIn: { enabled: false, autoCreate: false },
  status: { indexedItems: 42, indexedAt: now, lastScanAt: now, pendingChanges: 0 }, ...over,
});

const channel = (over: Partial<NotificationChannel> = {}): NotificationChannel => ({
  id: 3, name: "Family ntfy", kind: "ntfy", enabled: true, personal: false, events: ["request.available"],
  settings: { topic: "family", language: "en" }, secretsSet: ["token"], failures: 0, createdAt: now, updatedAt: now, ...over,
});

describe("media server settings", () => {
  it("loads, tests, applies suggestions and saves without resending the stored key", async () => {
    const saved: Record<string, unknown>[] = [];
    const tests: Record<string, unknown>[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/mediaserver/streamero" && method === "GET") return { status: 200, body: streamero() };
      if (path === "/mediaserver/streamero" && method === "PUT") {
        saved.push(body as Record<string, unknown>);
        return { status: 200, body: streamero() };
      }
      if (path === "/mediaserver/streamero/libraries") return { status: 200, body: [
        { id: "lib-movies", name: "Movies", kind: "movies", paths: ["/media/movies"], itemCount: 3, suggestedRootFolderIds: [5] }] };
      if (path === "/mediaserver/streamero/test") {
        tests.push(body as Record<string, unknown>);
        return { status: 200, body: { ok: false, errorCode: "not_admin", libraries: [] } };
      }
      if (path === "/rootfolders") return { status: 200, body: [{ id: 5, path: "/data/movies", kind: "movies", accessible: true, writable: true, createdAt: now }] };
      if (path === "/notifications") return emptyNotifications;
    });
    renderApp("/settings/mediaserver", { me: testUser() });
    expect(await screen.findByTestId("streamero-status")).toHaveTextContent("42 titles");
    await userEvent.click(screen.getByTestId("streamero-test"));
    expect(await screen.findByTestId("streamero-test-result")).toHaveTextContent("does not belong to an administrator");
    expect(tests[0]).toEqual({ url: "http://streamero:8787" });

    await userEvent.click(await screen.findByTestId("use-suggestions"));
    expect(screen.getByTestId("map-5-lib-movies")).toBeChecked();
    await userEvent.click(screen.getByTestId("add-path-mapping"));
    const row = screen.getByTestId("path-mapping-0");
    await userEvent.type(within(row).getByLabelText("Path in Locarr"), "/data");
    await userEvent.type(within(row).getByLabelText("Path in Streamero"), "/media");
    await userEvent.click(screen.getByTestId("signin-enabled"));
    await userEvent.click(screen.getByTestId("save-streamero"));
    await waitFor(() => expect(saved).toHaveLength(1));
    expect(saved[0]).not.toHaveProperty("apiKey");
    expect(saved[0]).toMatchObject({ enabled: true, url: "http://streamero:8787", pathMappings: [{ locarrPath: "/data", streameroPath: "/media" }],
      libraryMap: [{ rootFolderId: 5, libraryIds: ["lib-movies"] }], signIn: { enabled: true, autoCreate: false } });
  });
});

describe("notification channels", () => {
  it("shows only the chosen kind's fields, sends typed secrets only and tests the channel", async () => {
    const created: Record<string, unknown>[] = [];
    const tested: Record<string, unknown>[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/notificationchannels" && method === "GET") return { status: 200, body: [channel({ failures: 2, lastError: "HTTP 500" })] };
      if (path === "/notificationchannels" && method === "POST") {
        created.push(body as Record<string, unknown>);
        return { status: 201, body: channel() };
      }
      if (path === "/notificationchannels/test") {
        tested.push(body as Record<string, unknown>);
        return { status: 200, body: { ok: true } };
      }
      if (path === "/settings/notifications") return { status: 200, body: { baseUrl: "", smtp: { host: "", port: 0, security: "starttls", username: "",
        password: { configured: false }, from: "" } } };
      if (path === "/notifications") return emptyNotifications;
    });
    renderApp("/settings/notifications", { me: testUser() });
    expect(await screen.findByTestId("channel-error")).toHaveTextContent("HTTP 500");

    await userEvent.click(screen.getByTestId("add-channel"));
    const editor = await screen.findByTestId("channel-editor");
    await userEvent.selectOptions(within(editor).getByTestId("channel-kind"), "gotify");
    expect(within(editor).queryByTestId("field-topic")).not.toBeInTheDocument();
    expect(within(editor).getByTestId("secret-token")).toBeInTheDocument();
    await userEvent.type(within(editor).getByTestId("channel-name"), "Gotify");
    await userEvent.type(within(editor).getByTestId("field-url"), "https://gotify.example.com");
    await userEvent.type(within(editor).getByTestId("secret-token"), "app-token");
    await userEvent.click(within(editor).getByTestId("event-health.warning"));
    await userEvent.click(within(editor).getByTestId("test-channel"));
    expect(await within(editor).findByTestId("channel-test-result")).toHaveTextContent("Test message sent.");
    expect(tested[0]).not.toHaveProperty("id");
    await userEvent.click(within(editor).getByTestId("save-channel"));
    await waitFor(() => expect(created).toHaveLength(1));
    expect(created[0]).toMatchObject({ name: "Gotify", kind: "gotify", personal: false, settings: { url: "https://gotify.example.com" },
      secrets: { token: "app-token" } });
    // Shared channels start with every event; the click turned health warnings off.
    expect(created[0]!.events).toContain("library.imported");
    expect(created[0]!.events).not.toContain("health.warning");
  });

  it("keeps stored secrets unless typed or removed and drops other kinds' fields", () => {
    const form = { name: " x ", kind: "ntfy" as const, enabled: true, events: ["request.available" as const, "library.imported" as const],
      settings: { topic: "t", chatId: "leftover", language: "hr" as const }, secrets: {} };
    expect(toInput(form, true)).toEqual({ name: "x", kind: "ntfy", enabled: true, personal: true, events: ["request.available"],
      settings: { topic: "t", language: "hr" } });
    expect(toInput({ ...form, secrets: { token: "" } }, false).secrets).toEqual({ token: "" });
  });

  it("lets a requester add a personal channel from the account page", async () => {
    const created: Record<string, unknown>[] = [];
    mockApi(({ method, path, body, query }) => {
      if (path === "/notificationchannels" && method === "GET") {
        expect(query.get("personal")).toBe("true");
        return { status: 200, body: [] };
      }
      if (path === "/notificationchannels" && method === "POST") {
        created.push(body as Record<string, unknown>);
        return { status: 201, body: channel({ personal: true }) };
      }
      if (path === "/auth/sessions" || path === "/auth/apikeys" || path === "/auth/identities") return { status: 200, body: [] };
      if (path === "/auth/providers") return { status: 200, body: [{ id: "local", kind: "password", name: "Locarr" }] };
      if (path === "/notifications") return emptyNotifications;
    });
    renderApp("/account", { me: requester() });
    const panel = await screen.findByTestId("personal-channels");
    await userEvent.click(within(panel).getByTestId("add-channel"));
    const editor = await screen.findByTestId("channel-editor");
    const kinds = within(within(editor).getByTestId("channel-kind")).getAllByRole("option").map((o) => o.getAttribute("value"));
    expect(kinds).not.toContain("webhook");
    expect(kinds).not.toContain("apprise");
    expect(within(editor).queryByTestId("event-library.imported")).not.toBeInTheDocument();
    await userEvent.type(within(editor).getByTestId("channel-name"), "My phone");
    await userEvent.type(within(editor).getByTestId("field-topic"), "mia-news");
    await userEvent.click(within(editor).getByTestId("save-channel"));
    await waitFor(() => expect(created).toHaveLength(1));
    expect(created[0]).toMatchObject({ kind: "ntfy", personal: true, settings: { topic: "mia-news" } });
    expect(created[0]).not.toHaveProperty("secrets");
  });
});

describe("sign in with Streamero", () => {
  const providers = { status: 200, body: [{ id: "local", kind: "password", name: "Locarr" }, { id: "streamero", kind: "password", name: "Streamero" }] };

  it("offers the provider, asks for the two-factor code and signs in", async () => {
    const bodies: Record<string, unknown>[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/auth/providers") return providers;
      if (method === "POST" && path === "/auth/providers/streamero/login") {
        const b = body as Record<string, unknown>;
        bodies.push(b);
        if (!b.totpCode) return { status: 401, body: { code: "totp_required", message: "code" } };
        return { status: 200, body: { user: requester() } };
      }
    });
    const { router } = renderApp("/login");
    await userEvent.click(await screen.findByTestId("provider-streamero"));
    await userEvent.type(screen.getByLabelText("Username"), "mia");
    await userEvent.type(screen.getByLabelText("Password"), "stream-pass");
    await userEvent.click(screen.getByRole("button", { name: "Sign in with Streamero" }));
    await userEvent.type(await screen.findByLabelText("Two-factor code from Streamero"), "123456");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Verify" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/discover"));
    expect(bodies.at(-1)).toMatchObject({ username: "mia", password: "stream-pass", totpCode: "123456", useCookie: true });
  });

  it("explains an account that is not linked", async () => {
    mockApi(({ path }) => {
      if (path === "/auth/providers") return providers;
      if (path === "/auth/providers/streamero/login") return { status: 403, body: { code: "identity_not_linked", message: "x" } };
    });
    renderApp("/login");
    await userEvent.click(await screen.findByTestId("provider-streamero"));
    await userEvent.type(screen.getByLabelText("Username"), "zoe");
    await userEvent.type(screen.getByLabelText("Password"), "stream-pass");
    await userEvent.click(screen.getByRole("button", { name: "Sign in with Streamero" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("No Locarr account is linked to this Streamero account");
  });

  it("shows only local sign-in without other providers", async () => {
    mockApi(({ path }) => (path === "/auth/providers" ? { status: 200, body: [{ id: "local", kind: "password", name: "Locarr" }] } : undefined));
    renderApp("/login");
    await screen.findByLabelText("Username");
    expect(screen.queryByTestId("login-providers")).not.toBeInTheDocument();
  });
});

describe("already in Streamero", () => {
  it("marks discover cards and puts Watch first on the title page", async () => {
    mockApi(({ path }) => {
      if (path === "/discover") return { status: 200, body: { metadataReady: true, musicReady: true, requests: [], rows: [
        { key: "trending", items: [item({ status: "available", streameroUrl: watch }), item({ tmdbId: 45745, title: "Sintel" })] }] } };
      if (path === "/discover/genres") return { status: 200, body: { movieGenres: [], seriesGenres: [], studios: [], networks: [] } };
      if (path === "/discover/movies/133701") return { status: 200, body: titleDetail({ item: item({ streameroUrl: watch }) }) };
      if (path === "/requests/summary") return { status: 200, body: summary() };
      if (path === "/notifications") return emptyNotifications;
    });
    const { router } = renderApp("/discover", { me: requester() });
    const row = await screen.findByTestId("discover-row-trending");
    expect(within(within(row).getByTestId("poster-movie-133701")).getByTestId("card-streamero")).toHaveTextContent("In Streamero");
    expect(within(within(row).getByTestId("poster-movie-45745")).queryByTestId("card-streamero")).not.toBeInTheDocument();

    await router.navigate({ to: "/discover/movie/$tmdbId", params: { tmdbId: "133701" } });
    const link = await screen.findByTestId("watch-in-streamero");
    expect(link).toHaveAttribute("href", watch);
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
    expect(screen.getByTestId("request-button").className).not.toContain("bg-accent ");
  });

  it("marks lookup results in Add movie and ignores unsafe links", async () => {
    mockApi(({ path }) => {
      if (path === "/movies/lookup") return { status: 200, body: [
        { tmdbId: 1, title: "Big Buck Bunny", originalTitle: "", year: 2008, releaseDate: "", overview: "", poster: "", backdrop: "", popularity: 1,
          rating: 1, streameroUrl: watch },
        { tmdbId: 2, title: "Evil", originalTitle: "", year: 2008, releaseDate: "", overview: "", poster: "", backdrop: "", popularity: 1,
          rating: 1, streameroUrl: "javascript:alert(1)" }] };
      if (path === "/rootfolders" || path === "/qualityprofiles" || path === "/movies") return { status: 200, body: [] };
      if (path === "/notifications") return emptyNotifications;
    });
    renderApp("/movies/add", { me: testUser() });
    await userEvent.type(await screen.findByTestId("add-movie-term"), "bunny{Enter}");
    expect(await screen.findByTestId("lookup-watch-1")).toHaveAttribute("href", watch);
    expect(screen.getByTestId("lookup-streamero-1")).toBeInTheDocument();
    expect(screen.queryByTestId("lookup-watch-2")).not.toBeInTheDocument();
  });
});

describe("users page Streamero link", () => {
  it("links a user to a Streamero account", async () => {
    const puts: { path: string; body: unknown }[] = [];
    const mia = requester();
    mockApi(({ method, path, body }) => {
      if (path === "/users") return { status: 200, body: [testUser(), mia] };
      if (path === "/invites") return { status: 200, body: [] };
      if (path === `/users/${mia.id}/quotas`) return { status: 200, body: { overrides: {}, movie: { limit: 0, days: 7, used: 0, remaining: 0, unlimited: true, custom: false },
        series: { limit: 0, days: 7, used: 0, remaining: 0, unlimited: true, custom: false }, music: { limit: 0, days: 7, used: 0, remaining: 0, unlimited: true, custom: false } } };
      if (path === "/mediaserver/streamero/users") return { status: 200, body: [
        { id: "s-1", username: "mia.s", isAdmin: true, disabled: false }, { id: "s-2", username: "ana.s", isAdmin: false, disabled: false, linkedUserId: 1, linkedUsername: "ana" }] };
      if (method === "PUT" && path.startsWith("/users/")) {
        puts.push({ path, body });
        return { status: 200, body: { provider: "streamero", subject: "s-1", username: "mia.s", userId: mia.id, linkedAt: now } };
      }
      if (path === "/notifications") return emptyNotifications;
    });
    renderApp("/settings/users", { me: testUser() });
    const row = await screen.findByTestId("user-mia");
    await userEvent.click(within(row).getByRole("button", { name: "Edit" }));
    const box = await within(row).findByTestId("streamero-link-mia");
    expect(within(box).getByRole("option", { name: /ana\.s/ })).toBeDisabled();
    await userEvent.selectOptions(within(box).getByTestId("streamero-account"), "s-1");
    await userEvent.click(within(box).getByTestId("streamero-link"));
    await waitFor(() => expect(puts).toHaveLength(1));
    expect(puts[0]).toEqual({ path: `/users/${mia.id}/identities/streamero`, body: { subject: "s-1", username: "mia.s" } });
  });
});
