import BackgroundTasks
import Foundation

/// Background App Refresh: iOS decides when (and whether) the app runs; it is usually a few
/// times a day for an app the person opens often, never after force-quit, and never in Low
/// Power Mode. The app also polls on every foreground and every minute while open.
enum BackgroundRefresh {
    static let identifier = "net.scyde.locarr.refresh"
    private static let enabledKey = "locarr.notifications.enabled"
    private static let lastRunKey = "locarr.notifications.lastBackgroundRun"

    /// Whether new notifications become banners (in-app list is always kept).
    static var enabled: Bool {
        get { UserDefaults.standard.object(forKey: enabledKey) as? Bool ?? true }
        set { UserDefaults.standard.set(newValue, forKey: enabledKey) }
    }

    /// When iOS last gave the app background time (shown in Settings, so it is honest).
    static var lastBackgroundRun: Date? {
        get { UserDefaults.standard.object(forKey: lastRunKey) as? Date }
        set { UserDefaults.standard.set(newValue, forKey: lastRunKey) }
    }

    static func schedule() {
        let request = BGAppRefreshTaskRequest(identifier: identifier)
        request.earliestBeginDate = Date(timeIntervalSinceNow: 15 * 60)
        try? BGTaskScheduler.shared.submit(request)
    }

    /// Polls every saved server that has a session and shows the banners. Returns the unread
    /// count of the active server (for the badge).
    @discardableResult
    static func pollAll(store: ServerStore = ServerStore(), tokens: TokenStore = KeychainTokenStore(),
                        session: URLSession = APIClient.defaultSession) async -> Int? {
        let servers = store.load()
        let showServer = servers.filter { tokens.token(for: $0.id) != nil }.count > 1
        var activeUnread: Int?
        for server in servers {
            guard let token = tokens.token(for: server.id) else { continue }
            let client = APIClient(baseURL: server.url, token: token, session: session)
            guard let result = try? await NotificationPoller.poll(client: client, cursor: server.notificationCursor) else { continue }
            store.update(server.id) { $0.notificationCursor = result.cursor }
            if server.id == store.activeID { activeUnread = result.unread }
            if enabled {
                await LocalNotifier.deliver(NotificationPoller.banners(result.fresh, server: server, showServer: showServer))
            }
        }
        if let activeUnread { await LocalNotifier.setBadge(activeUnread) }
        return activeUnread
    }
}
