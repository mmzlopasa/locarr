import Foundation
import Security

/// Normalizes a typed server address ("192.168.1.10:8484", "locarr.example.com") into a base URL.
enum ServerURL {
    /// A bare host gets `http://` when it is clearly on the home network and `https://`
    /// otherwise; a typed scheme is kept.
    static func normalize(_ input: String) -> URL? {
        var text = input.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return nil }
        let lower = text.lowercased()
        if !lower.hasPrefix("http://") && !lower.hasPrefix("https://") {
            let host = String(lower.split(separator: "/").first ?? "").split(separator: ":").first.map { String($0) } ?? ""
            text = (isLocal(host: host) ? "http://" : "https://") + text
        }
        while text.hasSuffix("/") { text.removeLast() }
        guard var comps = URLComponents(string: text), let host = comps.host, !host.isEmpty else { return nil }
        comps.scheme = comps.scheme?.lowercased()
        guard comps.scheme == "http" || comps.scheme == "https" else { return nil }
        for suffix in ["/api/v1", "/api"] where comps.path.hasSuffix(suffix) {
            comps.path = String(comps.path.dropLast(suffix.count))
        }
        comps.query = nil
        comps.fragment = nil
        return comps.url
    }

    /// Hosts that plain HTTP is allowed to (matches NSAppTransportSecurity in project.yml).
    static func isLocal(host raw: String) -> Bool {
        let host = raw.lowercased().trimmingCharacters(in: CharacterSet(charactersIn: "[]"))
        if host == "localhost" || !host.contains(".") && !host.contains(":") { return true }
        let suffixes = [".local", ".lan", ".home", ".home.arpa", ".internal", ".localdomain", ".ts.net"]
        if suffixes.contains(where: { host.hasSuffix($0) }) { return true }
        let parts = host.split(separator: ".").compactMap { Int($0) }
        if parts.count == 4 && host.split(separator: ".").count == 4 {
            switch (parts[0], parts[1]) {
            case (10, _), (127, _), (192, 168), (169, 254): return true
            case (172, 16...31): return true
            case (100, 64...127): return true
            default: return false
            }
        }
        if host.contains(":") {
            return host.hasPrefix("fc") || host.hasPrefix("fd") || host.hasPrefix("fe80") || host == "::1"
        }
        return false
    }

    /// Plain HTTP to a public address is refused by iOS (and would send the password in clear).
    static func isAllowed(_ url: URL) -> Bool {
        url.scheme == "https" || isLocal(host: url.host() ?? "")
    }
}

/// One Locarr server the app knows. The session token is in the Keychain, never here.
struct SavedServer: Codable, Hashable, Identifiable, Sendable {
    var id: String
    var url: URL
    var name: String
    var username: String?
    /// The newest notification id already shown, per server (the poller's cursor).
    var notificationCursor: Int64?
}

/// Stores session tokens per server.
protocol TokenStore: Sendable {
    func token(for serverID: String) -> String?
    func setToken(_ token: String?, for serverID: String)
}

/// Keychain generic passwords, `AfterFirstUnlockThisDeviceOnly`: readable by Background App
/// Refresh after the first unlock, never in backups or on another device. Needs no
/// entitlement, so it survives Feather re-signing.
struct KeychainTokenStore: TokenStore {
    private let service = "net.scyde.locarr.session"

    func token(for serverID: String) -> String? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: serverID,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        var item: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess, let data = item as? Data else { return nil }
        return String(data: data, encoding: .utf8)
    }

    func setToken(_ token: String?, for serverID: String) {
        let base: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: serverID,
        ]
        SecItemDelete(base as CFDictionary)
        guard let token, let data = token.data(using: .utf8) else { return }
        var add = base
        add[kSecValueData as String] = data
        add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        SecItemAdd(add as CFDictionary, nil)
    }
}

/// In-memory store for tests.
final class MemoryTokenStore: TokenStore, @unchecked Sendable {
    private var tokens: [String: String] = [:]
    private let lock = NSLock()

    func token(for serverID: String) -> String? { lock.withLock { tokens[serverID] } }
    func setToken(_ token: String?, for serverID: String) { lock.withLock { tokens[serverID] = token } }
}

/// The server list and which one is active, in UserDefaults (no secrets).
struct ServerStore: @unchecked Sendable {
    let defaults: UserDefaults
    private let listKey = "locarr.servers"
    private let activeKey = "locarr.activeServer"

    init(defaults: UserDefaults = .standard) { self.defaults = defaults }

    func load() -> [SavedServer] {
        guard let data = defaults.data(forKey: listKey) else { return [] }
        return (try? JSONDecoder().decode([SavedServer].self, from: data)) ?? []
    }

    func save(_ servers: [SavedServer]) {
        defaults.set(try? JSONEncoder().encode(servers), forKey: listKey)
    }

    var activeID: String? {
        get { defaults.string(forKey: activeKey) }
        nonmutating set { defaults.set(newValue, forKey: activeKey) }
    }

    func update(_ id: String, _ change: (inout SavedServer) -> Void) {
        var all = load()
        guard let i = all.firstIndex(where: { $0.id == id }) else { return }
        change(&all[i])
        save(all)
    }
}
