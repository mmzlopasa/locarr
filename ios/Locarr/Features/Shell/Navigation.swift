import SwiftUI

/// Every screen that can be pushed onto a navigation stack.
enum Route: Hashable {
    case section(AppSection)
    case title(TitleRef)
    case person(Int64)
    case discoverList(key: String, title: String, id: Int64?)
    case request(Int64)
    case issue(Int64)
    case movie(Int64)
    case series(Int64)
    case artist(Int64)
    case album(Int64)
    case releases(ReleaseTarget, title: String)
    case timeline(ref: String, title: String)
    case add(kind: String)
    case boardGroup(code: String)

    init?(_ link: DeepLink) {
        switch link {
        case .notifications: self = .section(.inbox)
        case .requests: self = .section(.requests)
        case .board: self = .section(.board)
        case .health: self = .section(.health)
        case .queue: self = .section(.queue)
        case .request(let id): self = .request(id)
        case .issue(let id): self = .issue(id)
        case .movie(let id): self = .movie(id)
        case .series(let id): self = .series(id)
        case .artist(let id): self = .artist(id)
        case .album(let id): self = .album(id)
        }
    }
}

/// Top-level places of the app (tabs on iPhone, the sidebar on iPad).
enum AppSection: String, Hashable, CaseIterable, Identifiable {
    case overview, library, movies, series, music, discover, search, requests, issues, inbox
    case activity, queue, board, wanted, calendar, history, health, indexers, clients, folders, web, settings, more

    var id: String { rawValue }

    var title: String {
        switch self {
        case .overview: return L("overview.title")
        case .library: return L("nav.library")
        case .movies: return L("mediaKinds.movies")
        case .series: return L("mediaKinds.series")
        case .music: return L("mediaKinds.music")
        case .discover: return L("discover.title")
        case .search: return L("discover.searchLabel")
        case .requests: return L("requests.title")
        case .issues: return L("issues.title")
        case .inbox: return L("notifications.title")
        case .activity: return L("nav.activity")
        case .queue: return L("nav.queue")
        case .board: return L("board.title")
        case .wanted: return L("nav.wanted")
        case .calendar: return L("nav.calendar")
        case .history: return L("history.title")
        case .health: return L("health.title")
        case .indexers: return L("nav.indexers")
        case .clients: return L("nav.clients")
        case .folders: return L("overview.manage")
        case .web: return L("web.title")
        case .settings: return L("settings.app")
        case .more: return L("nav.more")
        }
    }

    var icon: String {
        switch self {
        case .overview: return "gauge.with.dots.needle.50percent"
        case .library: return "square.stack.3d.up"
        case .movies: return "film"
        case .series: return "tv"
        case .music: return "music.note"
        case .discover: return "sparkles"
        case .search: return "magnifyingglass"
        case .requests: return "tray.and.arrow.down"
        case .issues: return "exclamationmark.bubble"
        case .inbox: return "bell"
        case .activity: return "arrow.down.circle"
        case .queue: return "arrow.down.circle"
        case .board: return "questionmark.circle"
        case .wanted: return "scope"
        case .calendar: return "calendar"
        case .history: return "clock.arrow.circlepath"
        case .health: return "stethoscope"
        case .indexers: return "antenna.radiowaves.left.and.right"
        case .clients: return "externaldrive.connected.to.line.below"
        case .folders: return "folder"
        case .web: return "safari"
        case .settings: return "gearshape"
        case .more: return "ellipsis.circle"
        }
    }

    /// iPhone tabs.
    static func tabs(manager: Bool) -> [AppSection] {
        manager ? [.overview, .library, .discover, .activity, .more] : [.discover, .search, .requests, .inbox, .settings]
    }

    /// Rows of the More tab (library managers).
    static func moreRows(session: Session) -> [[AppSection]] {
        var manage: [AppSection] = [.requests, .issues, .inbox, .search]
        if !session.canManageRequests { manage = [.requests, .issues, .inbox, .search] }
        var system: [AppSection] = [.wanted, .calendar, .history]
        if session.isAdmin { system += [.health, .indexers, .clients, .folders] }
        return [manage, system, session.isAdmin ? [.web, .settings] : [.settings]]
    }

    /// iPad sidebar groups.
    static func sidebar(session: Session) -> [(String, [AppSection])] {
        if !session.canManageLibrary {
            return [("", [.discover, .search, .requests, .issues, .inbox]), (L("nav.app"), [.settings])]
        }
        var groups: [(String, [AppSection])] = [
            ("", [.overview, .discover, .search]),
            (L("nav.library"), [.movies, .series, .music]),
            (L("nav.activity"), [.queue, .board, .wanted, .calendar, .history]),
            (L("requests.title"), [.requests, .issues, .inbox]),
        ]
        if session.isAdmin { groups.append((L("nav.system"), [.health, .indexers, .clients, .folders, .web])) }
        groups.append((L("nav.app"), [.settings]))
        return groups
    }

    /// Which tab a deep link opens in.
    static func tab(for route: Route, manager: Bool) -> AppSection {
        switch route {
        case .section(let s):
            if manager {
                switch s {
                case .queue, .board, .history, .wanted, .calendar, .activity: return .activity
                case .movies, .series, .music, .library: return .library
                case .overview: return .overview
                case .discover: return .discover
                default: return .more
                }
            }
            return [.discover, .search, .requests, .inbox, .settings].contains(s) ? s : .requests
        case .request, .issue: return manager ? .more : .requests
        case .movie, .series, .artist, .album, .releases, .timeline, .add: return manager ? .library : .requests
        default: return manager ? .discover : .discover
        }
    }
}

/// The screen for a route.
struct RouteView: View {
    let route: Route

    var body: some View {
        switch route {
        case .section(let s): SectionRoot(section: s)
        case .title(let ref): TitleDetailView(ref: ref)
        case .person(let id): PersonView(id: id)
        case .discoverList(let key, let title, let id): DiscoverListView(key: key, title: title, id: id)
        case .request(let id): RequestDetailView(id: id)
        case .issue(let id): IssueDetailView(id: id)
        case .movie(let id): MovieDetailView(id: id)
        case .series(let id): SeriesDetailView(id: id)
        case .artist(let id): ArtistDetailView(id: id)
        case .album(let id): AlbumDetailView(id: id)
        case .releases(let target, let title): ReleasesView(target: target, title: title)
        case .timeline(let ref, let title): ItemTimelineView(ref: ref, title: title)
        case .add(let kind): AddTitleView(kind: kind)
        case .boardGroup(let code): BoardGroupView(code: code)
        }
    }
}

/// The root screen of a section.
struct SectionRoot: View {
    let section: AppSection
    @Environment(Session.self) private var session

    var body: some View {
        Group {
            switch section {
            case .overview: OverviewView()
            case .library: LibraryView()
            case .movies: MoviesView()
            case .series: SeriesListView()
            case .music: ArtistsView()
            case .discover: DiscoverView()
            case .search: DiscoverSearchView()
            case .requests: RequestsView()
            case .issues: IssuesView()
            case .inbox: InboxView()
            case .activity: ActivityView()
            case .queue: QueueView()
            case .board: BoardView()
            case .wanted: WantedView()
            case .calendar: CalendarAgendaView()
            case .history: HistoryView()
            case .health: HealthView()
            case .indexers: IndexersView()
            case .clients: DownloadClientsView()
            case .folders: RootFoldersView()
            case .web: WebSettingsView()
            case .settings: SettingsView()
            case .more: MoreView()
            }
        }
    }
}

/// The bell with the unread count (opens the notification centre).
struct InboxButton: View {
    @Environment(Session.self) private var session
    var body: some View {
        NavigationLink(value: Route.section(.inbox)) {
            Image(systemName: session.unread > 0 ? "bell.badge" : "bell")
                .symbolRenderingMode(.hierarchical)
        }
        .accessibilityLabel(Text(verbatim: session.unread > 0 ? L("notifications.labelUnread", ["n": String(session.unread)]) : L("notifications.label")))
    }
}

extension View {
    /// Navigation destinations shared by every stack.
    func routes() -> some View {
        navigationDestination(for: Route.self) { RouteView(route: $0) }
    }
}
