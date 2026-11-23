import SwiftUI

/// The manager's home: downloads, why things did not land, health, the next days and disk.
struct OverviewView: View {
    @Environment(Session.self) private var session
    @State private var queue: Queue?
    @State private var board: BoardSummary?
    @State private var health: HealthReport?
    @State private var upcoming: [CalendarEntry] = []
    @State private var albums: [AlbumCalendarEntry] = []
    @State private var folders: [RootFolder] = []
    @State private var summary: RequestSummary?
    @State private var error: String?
    @State private var loaded = false

    var body: some View {
        ScrollView {
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 320), spacing: 14, alignment: .top)], spacing: 14) {
                if let error, !loaded { ErrorState(message: error) { await load() } }
                if session.canManageRequests, let s = summary, (s.pending ?? 0) > 0 || (s.openIssues ?? 0) > 0 {
                    requestsPanel(s)
                }
                queuePanel
                boardPanel
                if session.isAdmin { healthPanel }
                upcomingPanel
                if session.isAdmin && !folders.isEmpty { diskPanel }
            }
            .padding()
        }
        .refreshable { await load() }
        .task { await load() }
        .navigationTitle(Text(verbatim: L("overview.title")))
        .toolbar { ToolbarItem(placement: .topBarTrailing) { InboxButton() } }
        .screenStyle()
    }

    private func panel<Content: View>(_ title: String, link: Route?, linkTitle: String, @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text(verbatim: title).font(.headline).foregroundStyle(Theme.text).accessibilityAddTraits(.isHeader)
                Spacer()
                if let link { NavigationLink(value: link) { Text(verbatim: linkTitle).font(.subheadline) } }
            }
            content()
        }
        .card()
    }

    private func requestsPanel(_ s: RequestSummary) -> some View {
        panel(L("requests.title"), link: .section(.requests), linkTitle: L("overview.open")) {
            HStack(spacing: 12) {
                stat(String(s.pending ?? 0), L("requests.status.pending"), tone: (s.pending ?? 0) > 0 ? "warning" : nil)
                stat(String(s.processing ?? 0), L("requests.status.processing"))
                stat(String(s.openIssues ?? 0), L("issues.title"), tone: (s.openIssues ?? 0) > 0 ? "danger" : nil)
            }
        }
    }

    private func stat(_ value: String, _ label: String, tone: String? = nil) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: value).font(.title2.weight(.bold).monospacedDigit()).foregroundStyle(tone.map { Theme.tone($0).fg } ?? Theme.text)
            Text(verbatim: label).font(.caption).foregroundStyle(Theme.textMuted).lineLimit(2)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }

    private var queuePanel: some View {
        panel(L("overview.queue"), link: .section(.queue), linkTitle: L("overview.openQueue")) {
            if let q = queue {
                let active = q.items.filter { $0.state == "downloading" }
                let speed = active.reduce(Int64(0)) { $0 + ($1.downloadSpeed ?? 0) }
                Text(verbatim: L("overview.queueSummary", ["count": String(q.items.count), "active": String(active.count), "speed": Fmt.speed(speed)]))
                    .font(.footnote).foregroundStyle(Theme.textMuted)
                if q.items.isEmpty { Text(verbatim: L("queue.empty")).font(.callout).foregroundStyle(Theme.textMuted) }
                ForEach(q.items.prefix(4)) { item in QueueRow(item: item, compact: true) }
                ForEach((q.clients ?? []).filter { $0.status?.ok == false }) { c in
                    Callout(text: L("queue.clientError", ["name": c.name, "error": c.status?.error ?? ""]), tone: "danger")
                }
            } else { ProgressView().tint(Theme.accent) }
        }
    }

    private var boardPanel: some View {
        panel(L("board.title"), link: .section(.board), linkTitle: L("overview.openBoard")) {
            if let b = board {
                let groups = b.groups.filter { $0.code != "available" && $0.count > 0 }
                if groups.isEmpty { Text(verbatim: L("board.empty")).font(.callout).foregroundStyle(Theme.textMuted) }
                ForEach(groups.prefix(5)) { g in
                    NavigationLink(value: Route.boardGroup(code: g.code)) { BoardGroupRow(group: g) }.buttonStyle(.plain)
                }
            } else { ProgressView().tint(Theme.accent) }
        }
    }

    private var healthPanel: some View {
        panel(L("health.title"), link: .section(.health), linkTitle: L("overview.openHealth")) {
            if let h = health {
                if h.issues.isEmpty {
                    Label { Text(verbatim: L("health.allGood")) } icon: { Image(systemName: "checkmark.circle.fill").foregroundStyle(Theme.success) }
                        .font(.callout)
                }
                ForEach(h.issues.prefix(4)) { i in Callout(text: Describe.health(i), tone: i.severity) }
            } else { ProgressView().tint(Theme.accent) }
        }
    }

    private var upcomingPanel: some View {
        panel(L("overview.upcoming"), link: .section(.calendar), linkTitle: L("overview.openCalendar")) {
            if upcoming.isEmpty && albums.isEmpty {
                Text(verbatim: L("overview.nothingUpcoming")).font(.callout).foregroundStyle(Theme.textMuted)
            }
            ForEach(Array(upcoming.prefix(6).enumerated()), id: \.offset) { _, e in CalendarRow(entry: e) }
            ForEach(Array(albums.prefix(3).enumerated()), id: \.offset) { _, a in AlbumCalendarRow(entry: a) }
        }
    }

    private var diskPanel: some View {
        panel(L("overview.disk"), link: .section(.folders), linkTitle: L("overview.manage")) {
            ForEach(folders) { f in DiskRow(folder: f) }
        }
    }

    private func load() async {
        let client = session.client
        let today = Date()
        let start = Fmt.dayString(today), end = Fmt.dayString(today.addingTimeInterval(7 * 86_400))
        async let q = try? client.queue()
        async let b = try? client.board()
        async let c = try? client.calendar(start: start, end: end)
        async let m = try? client.musicCalendar(start: start, end: end)
        queue = await q
        board = await b
        upcoming = await c ?? []
        albums = await m ?? []
        if session.isAdmin {
            async let h = try? client.health()
            async let f = try? client.rootFolders()
            health = await h
            folders = await f ?? []
        }
        if session.canManageRequests { summary = try? await client.requestSummary() }
        loaded = queue != nil || board != nil
        error = loaded ? nil : L("errors.network")
    }
}

/// Space left on one media folder.
struct DiskRow: View {
    let folder: RootFolder
    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack {
                Text(verbatim: folder.path).font(.footnote.monospaced()).foregroundStyle(Theme.text).lineLimit(1)
                Pill(text: Loc.render("mediaKinds.\(folder.kind)", [:], fallback: folder.kind))
                Spacer()
                if folder.accessible == false {
                    Pill(text: L("folders.notAccessible"), tone: "danger")
                } else if folder.writable == false {
                    Pill(text: L("folders.readOnly"), tone: "warning")
                }
            }
            if let total = folder.totalSpaceBytes, total > 0, let free = folder.freeSpaceBytes {
                let used = Double(total - free) / Double(total)
                ProgressBar(value: used, tint: used > 0.92 ? Theme.danger : used > 0.8 ? Theme.warning : Theme.accent)
                Text(verbatim: L("folders.free", ["free": Fmt.bytes(free), "total": Fmt.bytes(total)])).font(.caption2).foregroundStyle(Theme.textMuted)
            }
        }
        .accessibilityElement(children: .combine)
    }
}
