import Foundation
import Observation
import UIKit

/// Saved servers and which one is in use: add, sign in, switch, sign out, forget.
@MainActor
@Observable
final class AppModel {
    enum Phase {
        /// No server yet: the connect screen.
        case connect
        /// A server without a valid session; `notice` explains why (expired session).
        case signIn(SavedServer, notice: String?)
        case ready(Session)
    }

    private(set) var servers: [SavedServer] = []
    var phase: Phase = .connect
    /// A link that arrived while signed out; opened after signing in.
    var pendingLink: (link: DeepLink, server: String?)?
    /// Sheet: add another server while signed in.
    var addingServer = false
    /// Bumped when the app language changes, so every screen is built again.
    var languageTick = 0

    let store: ServerStore
    let tokens: TokenStore
    let urlSession: URLSession

    init(store: ServerStore = ServerStore(), tokens: TokenStore = KeychainTokenStore(), urlSession: URLSession = APIClient.defaultSession) {
        self.store = store
        self.tokens = tokens
        self.urlSession = urlSession
        servers = store.load()
        if let active = servers.first(where: { $0.id == store.activeID }) ?? servers.first {
            open(active)
        }
    }

    var activeServer: SavedServer? {
        switch phase {
        case .connect: return nil
        case .signIn(let s, _): return s
        case .ready(let session): return session.server
        }
    }

    var session: Session? {
        if case .ready(let s) = phase { return s }
        return nil
    }

    /// A client without a session (server checks and sign-in).
    func anonymousClient(_ url: URL) -> APIClient {
        APIClient(baseURL: url, token: nil, session: urlSession)
    }

    /// Opens a saved server: its session when there is a token, otherwise sign-in.
    func open(_ server: SavedServer, notice: String? = nil) {
        store.activeID = server.id
        if let token = tokens.token(for: server.id) {
            let id = server.id
            let client = APIClient(baseURL: server.url, token: token, session: urlSession) { [weak self] in
                Task { @MainActor in self?.sessionExpired(serverID: id) }
            }
            let session = Session(server: server, client: client, app: self)
            phase = .ready(session)
            Task { await session.start() }
        } else {
            phase = .signIn(server, notice: notice)
        }
    }

    /// Adds (or finds) a server after its address was checked.
    func addServer(url: URL, name: String) -> SavedServer {
        if let existing = servers.first(where: { $0.url == url }) { return existing }
        let server = SavedServer(id: UUID().uuidString, url: url, name: name.isEmpty ? (url.host() ?? url.absoluteString) : name)
        servers.append(server)
        store.save(servers)
        return server
    }

    func signedIn(_ server: SavedServer, token: String, user: User) {
        tokens.setToken(token, for: server.id)
        var updated = server
        updated.username = user.username
        replace(updated)
        if let lang = user.language, Loc.override == nil, lang != Loc.language {
            // Follow the account's language from the web unless the app has its own choice.
            Loc.override = lang
            languageTick += 1
        }
        addingServer = false
        open(updated)
    }

    func signOut() async {
        guard let session else { return }
        try? await session.client.logout()
        tokens.setToken(nil, for: session.server.id)
        await LocalNotifier.setBadge(0)
        phase = .signIn(session.server, notice: nil)
    }

    func forget(_ server: SavedServer) {
        tokens.setToken(nil, for: server.id)
        servers.removeAll { $0.id == server.id }
        store.save(servers)
        if activeServer?.id == server.id {
            if let next = servers.first { open(next) } else { phase = .connect }
        }
    }

    func switchTo(_ server: SavedServer) {
        guard activeServer?.id != server.id else { return }
        open(server)
    }

    func setLanguage(_ language: String?) {
        Loc.override = language
        LocalNotifier.registerCategories()
        languageTick += 1
    }

    func hasSession(_ server: SavedServer) -> Bool { tokens.token(for: server.id) != nil }

    /// 401 from the server: the session ended (signed out elsewhere, expired, revoked).
    func sessionExpired(serverID: String) {
        tokens.setToken(nil, for: serverID)
        guard let server = servers.first(where: { $0.id == serverID }), activeServer?.id == serverID else { return }
        phase = .signIn(server, notice: L("errors.unauthorized"))
    }

    private func replace(_ server: SavedServer) {
        if let i = servers.firstIndex(where: { $0.id == server.id }) { servers[i] = server } else { servers.append(server) }
        store.save(servers)
    }

    /// A `locarr://` link: switches server when the link names another one.
    func handle(url: URL) {
        guard let parsed = DeepLink.parse(url) else { return }
        handle(link: parsed.link, server: parsed.server)
    }

    func handle(link: DeepLink, server: String?) {
        if let server, server != activeServer?.id, let target = servers.first(where: { $0.id == server }) {
            open(target)
        }
        if let session {
            session.open(link)
        } else {
            pendingLink = (link, server)
        }
    }

    /// The device name sent with a sign-in (listed under the account's sessions on the web).
    static var device: DeviceInfo {
        DeviceInfo(name: UIDevice.current.name, client: "Locarr iOS \(Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "")")
    }
}

/// A signed-in server: the client, the account, the unread count and navigation requests.
@MainActor
@Observable
final class Session {
    let server: SavedServer
    let client: APIClient
    @ObservationIgnored weak var app: AppModel?
    var me: User?
    var meError: String?
    var unread = 0
    var pendingRequests = 0
    /// Set by deep links; the shell selects the right section and pushes the screen.
    var link: DeepLink?
    var linkTick = 0
    @ObservationIgnored private var pollTask: Task<Void, Never>?

    init(server: SavedServer, client: APIClient, app: AppModel?) {
        self.server = server
        self.client = client
        self.app = app
    }

    var canManageLibrary: Bool { me?.canManageLibrary == true }
    var canManageRequests: Bool { me?.canManageRequests == true }
    var isAdmin: Bool { me?.isAdmin == true }

    func start() async {
        await loadMe()
        if let pending = app?.pendingLink, pending.server == nil || pending.server == server.id {
            app?.pendingLink = nil
            open(pending.link)
        }
        await pollNow()
    }

    func loadMe() async {
        do {
            me = try await client.me()
            meError = nil
        } catch {
            if me == nil { meError = error.userMessage }
        }
    }

    func open(_ link: DeepLink) {
        self.link = link
        linkTick += 1
    }

    /// Foreground polling: once now, then every minute while the app is active.
    func startForegroundPolling() {
        pollTask?.cancel()
        pollTask = Task { [weak self] in
            while !Task.isCancelled {
                await self?.pollNow()
                try? await Task.sleep(for: .seconds(60))
            }
        }
    }

    func stopForegroundPolling() {
        pollTask?.cancel()
        pollTask = nil
    }

    func pollNow() async {
        guard let app else { return }
        if let unread = await BackgroundRefresh.pollAll(store: app.store, tokens: app.tokens, session: app.urlSession) {
            self.unread = unread
        }
        if canManageRequests, let summary = try? await client.requestSummary() {
            pendingRequests = summary.pending ?? 0
        }
    }

    func setUnread(_ n: Int) async {
        unread = n
        await LocalNotifier.setBadge(n)
    }
}
