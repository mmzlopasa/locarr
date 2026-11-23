import SwiftUI
import UserNotifications

/// The app's own settings: account, servers, language, notifications, about.
struct SettingsView: View {
    @Environment(Session.self) private var session
    @Environment(AppModel.self) private var app
    @State private var permission: UNAuthorizationStatus = .notDetermined
    @State private var banners = BackgroundRefresh.enabled
    @State private var confirmSignOut = false
    @State private var refreshStatus: UIBackgroundRefreshStatus = .available
    @Environment(\.openURL) private var openURL

    var body: some View {
        List {
            Section {
                if let me = session.me {
                    InfoRow(label: L("login.username"), value: me.username)
                    InfoRow(label: L("settings.role"), value: Loc.render("roles.\(me.role.rawValue)", [:], fallback: me.role.rawValue))
                }
                InfoRow(label: L("servers.server"), value: session.server.name)
                if !session.canManageLibrary {
                    NavigationLink(value: Route.section(.issues)) {
                        Label { Text(verbatim: L("issues.title")) } icon: { Image(systemName: "exclamationmark.bubble") }
                    }
                }
                NavigationLink { ServerListView() } label: {
                    Label { Text(verbatim: L("servers.title")) } icon: { Image(systemName: "server.rack") }
                }
                Button { openURL(session.server.url.appending(path: "account")) } label: {
                    Label { Text(verbatim: L("settings.accountWeb")) } icon: { Image(systemName: "person.crop.circle") }
                }
            } header: { Text(verbatim: L("settings.account")) }

            Section {
                Picker(selection: Binding(get: { Loc.override ?? "system" }, set: { app.setLanguage($0 == "system" ? nil : $0) })) {
                    Text(verbatim: L("settings.languageSystem")).tag("system")
                    Text(verbatim: "English").tag("en")
                    Text(verbatim: "Hrvatski").tag("hr")
                } label: { Text(verbatim: L("common.language")) }
            } header: { Text(verbatim: L("settings.app")) }

            Section {
                HStack {
                    Text(verbatim: L("settings.notifPermission"))
                    Spacer()
                    Text(verbatim: permissionText).foregroundStyle(permission == .authorized ? Theme.success : Theme.warning)
                }
                if permission == .notDetermined {
                    Button(L("settings.notifAllow")) { Task { await LocalNotifier.requestPermission(); await refresh() } }
                } else if permission == .denied {
                    Button(L("settings.openSystemSettings")) { openSettings() }
                }
                Toggle(isOn: $banners) { Text(verbatim: L("settings.notifBanners")) }
                    .onChange(of: banners) { _, on in BackgroundRefresh.enabled = on }
                HStack {
                    Text(verbatim: L("settings.backgroundRefresh"))
                    Spacer()
                    Text(verbatim: refreshStatus == .available ? L("settings.on") : L("settings.off"))
                        .foregroundStyle(refreshStatus == .available ? Theme.success : Theme.warning)
                }
                if refreshStatus != .available {
                    Button(L("settings.openSystemSettings")) { openSettings() }
                }
                InfoRow(label: L("settings.lastBackground"), value: BackgroundRefresh.lastBackgroundRun.map { Fmt.relative($0) } ?? L("common.never"))
                Button(L("settings.checkNow")) { Task { await session.pollNow() } }
            } header: {
                Text(verbatim: L("notifications.title"))
            } footer: {
                Text(verbatim: L("settings.notifHonest"))
            }

            Section {
                InfoRow(label: L("settings.appVersion"), value: Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "–")
                ServerVersionRow()
            } header: { Text(verbatim: L("settings.about")) }

            Section {
                Button(role: .destructive) { confirmSignOut = true } label: { Text(verbatim: L("settings.signOut")) }
            }
        }
        .navigationTitle(Text(verbatim: L("settings.app")))
        .screenStyle()
        .task { await refresh() }
        .confirmationDialog(Text(verbatim: L("settings.signOut")), isPresented: $confirmSignOut) {
            Button(L("settings.signOut"), role: .destructive) { Task { await app.signOut() } }
        }
    }

    private var permissionText: String {
        switch permission {
        case .authorized, .provisional, .ephemeral: return L("settings.on")
        case .denied: return L("settings.off")
        default: return L("settings.notAsked")
        }
    }

    private func refresh() async {
        permission = await LocalNotifier.authorizationStatus()
        refreshStatus = UIApplication.shared.backgroundRefreshStatus
    }

    private func openSettings() {
        if let url = URL(string: UIApplication.openSettingsURLString) { openURL(url) }
    }
}

private struct ServerVersionRow: View {
    @Environment(Session.self) private var session
    @State private var version: String?
    var body: some View {
        InfoRow(label: L("settings.serverVersion"), value: version ?? "–")
            .task { version = (try? await session.client.systemInfo())?.version }
    }
}

/// Saved servers: switch, add, forget.
struct ServerListView: View {
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var forgetting: SavedServer?

    var body: some View {
        List {
            Section {
                ForEach(app.servers) { server in
                    Button {
                        app.switchTo(server)
                        dismiss()
                    } label: {
                        HStack {
                            VStack(alignment: .leading, spacing: 3) {
                                Text(verbatim: server.name).foregroundStyle(Theme.text)
                                Text(verbatim: server.url.absoluteString).font(.caption.monospaced()).foregroundStyle(Theme.textMuted)
                                Text(verbatim: app.hasSession(server) ? L("servers.signedInAs", ["user": server.username ?? "–"]) : L("servers.signedOut"))
                                    .font(.caption2).foregroundStyle(Theme.textFaint)
                            }
                            Spacer()
                            if server.id == app.activeServer?.id { Image(systemName: "checkmark").foregroundStyle(Theme.accent) }
                        }
                    }
                    .swipeActions {
                        Button(role: .destructive) { forgetting = server } label: { Text(verbatim: L("servers.forget")) }
                    }
                }
            } footer: { Text(verbatim: L("servers.hint")) }
            Section {
                Button {
                    dismiss()
                    app.addingServer = true
                } label: {
                    Label { Text(verbatim: L("servers.add")) } icon: { Image(systemName: "plus") }
                }
            }
        }
        .navigationTitle(Text(verbatim: L("servers.title")))
        .screenStyle()
        .confirmationDialog(Text(verbatim: L("servers.forget")), isPresented: Binding(get: { forgetting != nil }, set: { if !$0 { forgetting = nil } })) {
            Button(L("servers.forget"), role: .destructive) {
                if let s = forgetting { app.forget(s) }
                forgetting = nil
            }
        } message: { Text(verbatim: L("servers.forgetHint")) }
    }
}

/// Deep settings stay on the web: these open the right page of the server's web client.
struct WebSettingsView: View {
    @Environment(Session.self) private var session
    @Environment(\.openURL) private var openURL

    struct Page: Hashable { let key: String; let path: String; let icon: String }

    static let pages: [Page] = [
        Page(key: "web.profiles", path: "settings/profiles", icon: "slider.horizontal.3"),
        Page(key: "web.customFormats", path: "settings/customformats", icon: "tag"),
        Page(key: "web.rules", path: "settings/rules", icon: "line.3.horizontal.decrease.circle"),
        Page(key: "web.quality", path: "settings/quality", icon: "chart.bar"),
        Page(key: "web.mediaManagement", path: "settings/mediamanagement", icon: "folder.badge.gearshape"),
        Page(key: "web.metadata", path: "settings/metadata", icon: "text.magnifyingglass"),
        Page(key: "web.requests", path: "settings/requests", icon: "tray.and.arrow.down"),
        Page(key: "web.users", path: "settings/users", icon: "person.2"),
        Page(key: "web.indexers", path: "indexers", icon: "antenna.radiowaves.left.and.right"),
        Page(key: "web.downloaders", path: "downloaders", icon: "externaldrive.connected.to.line.below"),
        Page(key: "web.subtitles", path: "settings/subtitles", icon: "captions.bubble"),
        Page(key: "web.notifications", path: "settings/notifications", icon: "bell.badge"),
        Page(key: "web.mediaServer", path: "settings/mediaserver", icon: "play.tv"),
        Page(key: "web.importLists", path: "settings/importlists", icon: "list.bullet.rectangle"),
        Page(key: "web.monitoring", path: "settings/system", icon: "waveform.path.ecg"),
        Page(key: "web.logs", path: "system/logs", icon: "doc.plaintext"),
        Page(key: "web.backups", path: "system/backups", icon: "externaldrive.badge.timemachine"),
        Page(key: "web.migrate", path: "migrate", icon: "square.and.arrow.down.on.square"),
    ]

    var body: some View {
        List {
            Section {
                ForEach(Self.pages, id: \.path) { page in
                    Button { openURL(session.server.url.appending(path: page.path)) } label: {
                        HStack {
                            Label { Text(verbatim: L(page.key)).foregroundStyle(Theme.text) } icon: { Image(systemName: page.icon).foregroundStyle(Theme.accent) }
                            Spacer()
                            Image(systemName: "arrow.up.right").font(.caption).foregroundStyle(Theme.textFaint)
                        }
                    }
                }
            } footer: { Text(verbatim: L("web.hint")) }
        }
        .navigationTitle(Text(verbatim: L("web.title")))
        .screenStyle()
    }
}
