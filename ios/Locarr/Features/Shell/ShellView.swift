import SwiftUI

struct RootView: View {
    @Environment(AppModel.self) private var app

    var body: some View {
        Group {
            switch app.phase {
            case .connect:
                ConnectView()
            case .signIn(let server, let notice):
                SignInView(server: server, notice: notice).id(server.id)
            case .ready(let session):
                ShellView().environment(session).id(session.server.id)
            }
        }
        .preferredColorScheme(.dark)
        .tint(Theme.accent)
        .environment(\.locale, Loc.locale)
        .id(app.languageTick)
        .sheet(isPresented: Binding(get: { app.addingServer }, set: { app.addingServer = $0 })) {
            ConnectView(onCancel: { app.addingServer = false }).environment(app)
        }
    }
}

/// Tabs on iPhone, a sidebar on iPad (and wide windows).
struct ShellView: View {
    @Environment(Session.self) private var session
    @Environment(\.horizontalSizeClass) private var sizeClass
    @State private var tab: AppSection = .discover
    @State private var paths: [AppSection: [Route]] = [:]
    @State private var sidebarSelection: AppSection? = .discover
    @State private var detailPath: [Route] = []

    var body: some View {
        Group {
            if session.me == nil {
                if let error = session.meError {
                    StartupError(message: error)
                } else {
                    LoadingState().frame(maxHeight: .infinity).background(Theme.background.ignoresSafeArea())
                }
            } else if sizeClass == .regular {
                split
            } else {
                tabs
            }
        }
        .onChange(of: session.me?.id) { _, _ in
            let start: AppSection = session.canManageLibrary ? .overview : .discover
            tab = start
            sidebarSelection = start
            openLink()
        }
        .onChange(of: session.linkTick) { _, _ in openLink() }
        .onAppear { if session.link != nil && session.me != nil { openLink() } }
        .task { await LocalNotifier.requestPermissionOnce() }
    }

    private var tabs: some View {
        TabView(selection: $tab) {
            ForEach(AppSection.tabs(manager: session.canManageLibrary)) { section in
                NavigationStack(path: Binding(get: { paths[section] ?? [] }, set: { paths[section] = $0 })) {
                    SectionRoot(section: section).routes()
                }
                .tabItem { Label { Text(verbatim: section.title) } icon: { Image(systemName: section.icon) } }
                .badge(badge(for: section))
                .tag(section)
            }
        }
    }

    private var split: some View {
        NavigationSplitView {
            List(selection: $sidebarSelection) {
                ForEach(Array(AppSection.sidebar(session: session).enumerated()), id: \.offset) { _, group in
                    Section {
                        ForEach(group.1) { s in
                            Label { Text(verbatim: s.title) } icon: { Image(systemName: s.icon) }
                                .badge(badge(for: s))
                                .tag(s)
                        }
                    } header: {
                        if !group.0.isEmpty { Text(verbatim: group.0) }
                    }
                }
            }
            .navigationTitle(Text(verbatim: L("app.name")))
            .scrollContentBackground(.hidden)
            .background(Theme.surface.ignoresSafeArea())
        } detail: {
            NavigationStack(path: $detailPath) {
                SectionRoot(section: sidebarSelection ?? .discover).routes()
            }
            .id(sidebarSelection)
        }
        .onChange(of: sidebarSelection) { _, _ in detailPath = [] }
    }

    private func badge(for s: AppSection) -> Int {
        switch s {
        case .inbox: return session.unread
        case .requests: return session.canManageRequests ? session.pendingRequests : 0
        case .more: return session.unread + (session.canManageRequests ? session.pendingRequests : 0)
        default: return 0
        }
    }

    private func openLink() {
        guard let link = session.link, session.me != nil, let route = Route(link) else { return }
        session.link = nil
        let manager = session.canManageLibrary
        if sizeClass == .regular {
            if case .section(let s) = route {
                sidebarSelection = s
            } else {
                detailPath.append(route)
            }
            return
        }
        let target = AppSection.tab(for: route, manager: manager)
        tab = target
        if case .section(let s) = route, s == target { paths[target] = []; return }
        paths[target, default: []].append(route)
    }
}

/// The account could not be loaded (server down, offline).
private struct StartupError: View {
    @Environment(Session.self) private var session
    @Environment(AppModel.self) private var app
    let message: String
    @State private var servers = false

    var body: some View {
        VStack(spacing: 18) {
            BrandMark(size: 56)
            Text(verbatim: session.server.name).font(.headline).foregroundStyle(Theme.text)
            ErrorState(message: message) { await session.loadMe() }
            Button { servers = true } label: { Text(verbatim: L("servers.title")) }.buttonStyle(SecondaryButtonStyle())
        }
        .padding()
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Theme.background.ignoresSafeArea())
        .sheet(isPresented: $servers) { NavigationStack { ServerListView() }.environment(app) }
    }
}

/// The More tab of library managers.
struct MoreView: View {
    @Environment(Session.self) private var session

    var body: some View {
        List {
            ForEach(Array(AppSection.moreRows(session: session).enumerated()), id: \.offset) { _, rows in
                Section {
                    ForEach(rows) { s in
                        NavigationLink(value: Route.section(s)) {
                            Label { Text(verbatim: s.title) } icon: { Image(systemName: s.icon).foregroundStyle(Theme.accent) }
                                .badge(s == .inbox ? session.unread : s == .requests && session.canManageRequests ? session.pendingRequests : 0)
                        }
                    }
                }
                .listRowBackground(Theme.surface)
            }
        }
        .navigationTitle(Text(verbatim: L("nav.more")))
        .screenStyle()
    }
}

extension LocalNotifier {
    private static let askedKey = "locarr.notifications.asked"

    /// Asks for permission once, after the first sign-in.
    static func requestPermissionOnce() async {
        guard !UserDefaults.standard.bool(forKey: askedKey) else { return }
        UserDefaults.standard.set(true, forKey: askedKey)
        await requestPermission()
    }
}
