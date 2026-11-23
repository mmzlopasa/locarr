import BackgroundTasks
import SwiftUI
import UserNotifications

@main
struct LocarrApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @Environment(\.scenePhase) private var scenePhase

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(delegate.model)
                .onOpenURL { delegate.model.handle(url: $0) }
        }
        .onChange(of: scenePhase) { _, phase in
            switch phase {
            case .active:
                delegate.model.session?.startForegroundPolling()
            case .background:
                delegate.model.session?.stopForegroundPolling()
                BackgroundRefresh.schedule()
            default:
                break
            }
        }
        .backgroundTask(.appRefresh(BackgroundRefresh.identifier)) {
            // Schedule the next one first: iOS runs at most one pending request per identifier.
            BackgroundRefresh.schedule()
            BackgroundRefresh.lastBackgroundRun = Date()
            await BackgroundRefresh.pollAll()
        }
    }
}

/// Notification categories, taps and the Approve action.
final class AppDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {
    let model: AppModel = {
        #if DEBUG
        if ScreenshotStub.isActive { return ScreenshotStub.model() }
        #endif
        return AppModel()
    }()

    func application(_ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        UNUserNotificationCenter.current().delegate = self
        LocalNotifier.registerCategories()
        return true
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        [.banner, .list, .sound]
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        let info = response.notification.request.content.userInfo
        let link = (info["link"] as? String).flatMap(URL.init(string:))
        let server = info["server"] as? String
        let requestId = (info["requestId"] as? String).flatMap { Int64($0) }
        let action = response.actionIdentifier
        await handleResponse(link: link, server: server, requestId: requestId, action: action)
    }

    @MainActor
    private func handleResponse(link: URL?, server: String?, requestId: Int64?, action: String) async {
        if action == LocalNotifier.approveAction, let requestId, let server {
            await approve(requestId, server: server)
            return
        }
        if let link { model.handle(url: link) }
    }

    /// Approves from the notification without opening the app; failures show a notification.
    @MainActor
    private func approve(_ id: Int64, server: String) async {
        guard let saved = model.servers.first(where: { $0.id == server }), let token = model.tokens.token(for: server) else { return }
        let client = APIClient(baseURL: saved.url, token: token, session: model.urlSession)
        let content = UNMutableNotificationContent()
        do {
            let request = try await client.approve(id)
            content.title = request.title
            content.body = L("requests.approvedToast")
        } catch {
            content.title = L("requests.approveFailed")
            content.body = error.userMessage
            content.userInfo = ["link": DeepLink.request(id).url(server: server).absoluteString, "server": server]
        }
        try? await UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: "approve-\(server)-\(id)", content: content, trigger: nil))
    }
}
