import Foundation
import UserNotifications

/// Push-free notifications (D-087): the app asks `/notifications?since=<newest seen id>`
/// when iOS gives it time (Background App Refresh) and whenever it is in the foreground,
/// and shows what is new as local notifications.
enum NotificationPoller {
    struct Result: Sendable, Equatable {
        /// Unread notifications newer than the cursor, oldest first.
        var fresh: [AppNotification]
        var cursor: Int64
        var unread: Int?
    }

    /// The first poll of a server only remembers where the list ends, so installing the app
    /// does not replay old notifications.
    static func poll(client: APIClient, cursor: Int64?) async throws -> Result {
        guard let cursor else {
            let list = try await client.notifications(limit: 1)
            let newest = max(list.latestId ?? 0, list.items.map(\.id).max() ?? 0)
            return Result(fresh: [], cursor: newest, unread: list.unread)
        }
        let list = try await client.notifications(since: cursor, limit: 50)
        let fresh = list.items.filter { $0.id > cursor && $0.read != true }.sorted { $0.id < $1.id }
        // More than one page newer: the rest is in the in-app list; banners stop at this page.
        let newest = max(cursor, list.items.map(\.id).max() ?? cursor, list.latestId ?? cursor)
        return Result(fresh: fresh, cursor: newest, unread: list.unread)
    }

    /// Category of a notification: new requests get an Approve action for those who manage them.
    static func category(for n: AppNotification) -> String {
        if n.type == "request.created" { return LocalNotifier.requestCategory }
        if n.type.hasPrefix("issue.") { return LocalNotifier.issueCategory }
        return LocalNotifier.generalCategory
    }

    /// The banners for one poll: each notification up to `limit`, otherwise one summary.
    static func banners(_ fresh: [AppNotification], server: SavedServer, showServer: Bool, limit: Int = 4) -> [Banner] {
        guard !fresh.isEmpty else { return [] }
        if fresh.count > limit {
            return [Banner(id: "\(server.id)-summary-\(fresh.last!.id)",
                           title: showServer ? server.name : L("app.name"),
                           body: L("inbox.summary", ["count": String(fresh.count)]),
                           category: LocalNotifier.generalCategory,
                           link: DeepLink.notifications.url(server: server.id), server: server.id, requestId: nil)]
        }
        return fresh.map { n in
            Banner(id: "\(server.id)-\(n.id)",
                   title: showServer ? "\(n.title) · \(server.name)" : n.title,
                   body: Describe.notification(n),
                   category: category(for: n),
                   link: DeepLink.forNotification(n).url(server: server.id),
                   server: server.id,
                   requestId: n.type == "request.created" ? n.requestId : nil)
        }
    }

    struct Banner: Sendable, Equatable {
        var id: String
        var title: String
        var body: String
        var category: String
        var link: URL
        var server: String
        var requestId: Int64?
    }
}

/// Local notifications through UNUserNotificationCenter (no APNs, no entitlement).
enum LocalNotifier {
    static let requestCategory = "LOCARR_REQUEST"
    static let issueCategory = "LOCARR_ISSUE"
    static let generalCategory = "LOCARR_GENERAL"
    static let approveAction = "LOCARR_APPROVE"
    static let openAction = "LOCARR_OPEN"

    static func registerCategories() {
        let open = UNNotificationAction(identifier: openAction, title: L("inbox.open"), options: [.foreground])
        let approve = UNNotificationAction(identifier: approveAction, title: L("requests.approve"), options: [.authenticationRequired])
        UNUserNotificationCenter.current().setNotificationCategories([
            UNNotificationCategory(identifier: requestCategory, actions: [approve, open], intentIdentifiers: []),
            UNNotificationCategory(identifier: issueCategory, actions: [open], intentIdentifiers: []),
            UNNotificationCategory(identifier: generalCategory, actions: [], intentIdentifiers: []),
        ])
    }

    @discardableResult
    static func requestPermission() async -> Bool {
        (try? await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])) ?? false
    }

    static func authorizationStatus() async -> UNAuthorizationStatus {
        await UNUserNotificationCenter.current().notificationSettings().authorizationStatus
    }

    static func deliver(_ banners: [NotificationPoller.Banner]) async {
        let center = UNUserNotificationCenter.current()
        for b in banners {
            let content = UNMutableNotificationContent()
            content.title = b.title
            content.body = b.body
            content.sound = .default
            content.categoryIdentifier = b.category
            content.threadIdentifier = b.server
            var info: [String: String] = ["link": b.link.absoluteString, "server": b.server]
            if let id = b.requestId { info["requestId"] = String(id) }
            content.userInfo = info
            try? await center.add(UNNotificationRequest(identifier: b.id, content: content, trigger: nil))
        }
    }

    static func setBadge(_ count: Int) async {
        try? await UNUserNotificationCenter.current().setBadgeCount(max(0, count))
    }
}
