import SwiftUI

/// The Activity tab on iPhone: queue, why didn't it land, wanted, calendar, history.
struct ActivityView: View {
    @State private var part: AppSection = .queue

    var body: some View {
        Group {
            switch part {
            case .board: BoardView()
            case .wanted: WantedView()
            case .calendar: CalendarAgendaView()
            case .history: HistoryView()
            default: QueueView()
            }
        }
        .safeAreaInset(edge: .top) {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 8) {
                    ForEach([AppSection.queue, .board, .wanted, .calendar, .history]) { s in
                        Chip(text: s.title, selected: part == s) { part = s }
                    }
                }
                .padding(.horizontal)
                .padding(.bottom, 6)
            }
            .background(Theme.background)
        }
    }
}

// MARK: Queue

struct QueueRow: View {
    let item: QueueItem
    var compact = false

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(verbatim: item.item?.title ?? item.title).font(.callout.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(compact ? 1 : 2)
            if !compact && item.item != nil {
                Text(verbatim: item.title).font(.caption2).foregroundStyle(Theme.textFaint).lineLimit(1)
            }
            ProgressBar(value: item.progress ?? 0, tint: tint)
            HStack(spacing: 6) {
                Pill(text: Describe.downloadState(item.state), tone: tone)
                Text(verbatim: [Fmt.percent(item.progress ?? 0), item.state == "downloading" ? Fmt.speed(item.downloadSpeed) : "",
                                item.state == "downloading" ? Fmt.duration(seconds: item.etaSeconds) : "", Fmt.bytes(item.sizeBytes)]
                        .filter { !$0.isEmpty && $0 != "–" }.joined(separator: " · "))
                    .font(.caption2.monospacedDigit()).foregroundStyle(Theme.textMuted).lineLimit(1)
            }
            if !compact {
                Text(verbatim: [item.clientName ?? "", item.indexer ?? "", item.unknown == true ? L("queue.unknown") : ""].filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(.caption2).foregroundStyle(Theme.textFaint)
                if let m = item.message, !m.isEmpty, item.state == "warning" || item.state == "failed" {
                    Text(verbatim: m).font(.caption).foregroundStyle(Theme.warning)
                }
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var tone: String {
        switch item.state {
        case "downloading": return "info"
        case "completed", "seeding": return "success"
        case "failed": return "danger"
        case "warning": return "warning"
        default: return "neutral"
        }
    }

    private var tint: Color { Theme.tone(tone == "neutral" ? "info" : tone).fg }
}

struct QueueView: View {
    @Environment(Session.self) private var session
    @State private var queue = Loader<Queue>()
    @State private var removing: QueueItem?
    @State private var toast: String?

    var body: some View {
        List {
            if let q = queue.value {
                ForEach((q.clients ?? []).filter { $0.status?.ok == false }) { c in
                    Callout(text: L("queue.clientError", ["name": c.name, "error": c.status?.error ?? ""]), tone: "danger").listRowBackground(Color.clear)
                }
                if (q.clients ?? []).isEmpty {
                    Callout(text: L("queue.noClients"), tone: "warning").listRowBackground(Color.clear)
                }
                if q.items.isEmpty { EmptyState(text: L("queue.empty"), systemImage: "arrow.down.circle").listRowBackground(Color.clear) }
                ForEach(q.items) { item in
                    Group {
                        if let link = item.item.flatMap({ DeepLink.forItemRef($0.ref) }).flatMap(Route.init) {
                            NavigationLink(value: link) { QueueRow(item: item) }
                        } else {
                            QueueRow(item: item)
                        }
                    }
                    .listRowBackground(Theme.background)
                    .swipeActions(edge: .trailing) {
                        Button(role: .destructive) { removing = item } label: {
                            Label { Text(verbatim: L("queue.actions.remove")) } icon: { Image(systemName: "trash") }
                        }
                        if item.state == "paused" {
                            Button { Task { await act(L("common.saved")) { try await $0.resume(item.id) } } } label: {
                                Label { Text(verbatim: L("queue.actions.resume")) } icon: { Image(systemName: "play") }
                            }
                            .tint(Theme.accent)
                        } else if item.state == "downloading" || item.state == "queued" {
                            Button { Task { await act(L("common.saved")) { try await $0.pause(item.id) } } } label: {
                                Label { Text(verbatim: L("queue.actions.pause")) } icon: { Image(systemName: "pause") }
                            }
                            .tint(Theme.warning)
                        }
                    }
                    .contextMenu {
                        Button { Task { await act(L("common.saved")) { try await $0.setPriority(item.id, "top") } } } label: { Text(verbatim: L("queue.actions.top")) }
                        Button { Task { await act(L("common.saved")) { try await $0.setPriority(item.id, "last") } } } label: { Text(verbatim: L("queue.actions.last")) }
                        Button(role: .destructive) { removing = item } label: { Text(verbatim: L("queue.actions.remove")) }
                    }
                }
            } else if let error = queue.error {
                ErrorState(message: error) { await load() }.listRowBackground(Color.clear)
            } else {
                LoadingState().listRowBackground(Color.clear)
            }
        }
        .listStyle(.plain)
        .refreshable {
            try? await session.client.refreshQueue()
            await load()
        }
        .task {
            // Live while visible: the queue changes every few seconds.
            while !Task.isCancelled {
                await load()
                try? await Task.sleep(for: .seconds(5))
            }
        }
        .navigationTitle(Text(verbatim: L("nav.queue")))
        .screenStyle()
        .confirmationDialog(Text(verbatim: removing?.title ?? ""), isPresented: Binding(get: { removing != nil }, set: { if !$0 { removing = nil } }),
                            titleVisibility: .visible, presenting: removing) { item in
            Button(L("queue.actions.removeConfirm"), role: .destructive) { Task { await remove(item, deleteData: false, blocklist: false) } }
            Button(L("queue.actions.removeData"), role: .destructive) { Task { await remove(item, deleteData: true, blocklist: false) } }
            Button(L("queue.actions.blocklist"), role: .destructive) { Task { await remove(item, deleteData: true, blocklist: true) } }
        }
        .toast($toast)
    }

    private func load() async {
        let client = session.client
        await queue.load { try await client.queue() }
    }

    private func act(_ success: String, _ op: (APIClient) async throws -> Void) async {
        do {
            try await op(session.client)
            toast = success
            await load()
        } catch { toast = error.userMessage }
    }

    private func remove(_ item: QueueItem, deleteData: Bool, blocklist: Bool) async {
        await act(L("queue.removed")) { try await $0.removeQueueItem(item.id, deleteData: deleteData, blocklist: blocklist) }
    }
}

// MARK: Why didn't it land?

struct BoardGroupRow: View {
    let group: BoardGroup
    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Circle().fill(Theme.tone(group.tone).fg).frame(width: 9, height: 9).padding(.top, 6)
            VStack(alignment: .leading, spacing: 3) {
                Text(verbatim: Describe.boardGroupTitle(group)).font(.callout.weight(.semibold)).foregroundStyle(Theme.text)
                let hint = Describe.boardGroupHint(group)
                if !hint.isEmpty { Text(verbatim: hint).font(.caption).foregroundStyle(Theme.textMuted).lineLimit(3) }
                if let top = group.reasons?.first {
                    Text(verbatim: L("board.topReasons") + " " + Describe.reason(top) + " (\(top.count ?? 0))").font(.caption2).foregroundStyle(Theme.textFaint)
                }
            }
            Spacer()
            Text(verbatim: String(group.count)).font(.title3.weight(.bold).monospacedDigit()).foregroundStyle(Theme.tone(group.tone).fg)
        }
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
    }
}

struct BoardView: View {
    @Environment(Session.self) private var session
    @State private var board = Loader<BoardSummary>()
    @State private var kind = "all"

    var body: some View {
        List {
            Section {
                Text(verbatim: L("board.subtitle")).font(.footnote).foregroundStyle(Theme.textMuted)
                Picker(selection: $kind) {
                    ForEach(["all", "movie", "episode", "album", "subtitle"], id: \.self) { Text(verbatim: L("board.kinds.\($0)")).tag($0) }
                } label: { Text(verbatim: L("board.kind")) }
                .pickerStyle(.menu)
            }
            .listRowBackground(Color.clear)
            if let b = board.value {
                let groups = b.groups.filter { $0.code != "available" && $0.count > 0 }
                if groups.isEmpty { EmptyState(text: L("board.empty"), systemImage: "checkmark.seal").listRowBackground(Color.clear) }
                ForEach(groups) { g in
                    NavigationLink(value: Route.boardGroup(code: g.code)) { BoardGroupRow(group: g) }
                        .listRowBackground(Theme.surface)
                }
                if let at = b.updatedAt {
                    Text(verbatim: L("board.updated", ["when": Fmt.relative(at)])).font(.caption2).foregroundStyle(Theme.textFaint).listRowBackground(Color.clear)
                }
            } else if let error = board.error {
                ErrorState(message: error) { await load() }.listRowBackground(Color.clear)
            } else {
                LoadingState().listRowBackground(Color.clear)
            }
        }
        .refreshable {
            try? await session.client.refreshBoard()
            await load()
        }
        .task(id: kind) { await load() }
        .navigationTitle(Text(verbatim: L("board.title")))
        .screenStyle()
    }

    private func load() async {
        let client = session.client, kind = kind == "all" ? nil : kind
        await board.load { try await client.board(kind: kind) }
    }
}

/// The items of one board group, with the bulk actions that fit it.
struct BoardGroupView: View {
    let code: String
    @Environment(Session.self) private var session
    @State private var pager: Pager<BoardItem>?
    @State private var selected: Set<String> = []
    @State private var toast: String?
    @State private var text = ""

    private var actions: [String] {
        switch code {
        case "import_failed": return ["retry_import", "blocklist_retry"]
        case "download_failed", "stalled": return ["blocklist_retry", "search"]
        case "not_monitored": return []
        default: return ["search", "unmonitor"]
        }
    }

    var body: some View {
        List {
            if let pager {
                ForEach(pager.items) { item in
                    HStack(alignment: .top, spacing: 10) {
                        if !actions.isEmpty {
                            Button {
                                if selected.contains(item.key) { selected.remove(item.key) } else { selected.insert(item.key) }
                            } label: {
                                Image(systemName: selected.contains(item.key) ? "checkmark.circle.fill" : "circle")
                                    .foregroundStyle(selected.contains(item.key) ? Theme.accent : Theme.textFaint)
                            }
                            .buttonStyle(.plain)
                            .accessibilityLabel(Text(verbatim: L("board.select", ["title": item.title])))
                        }
                        Group {
                            if let route = DeepLink.forItemRef(item.itemRef, parent: item.parentRef).flatMap(Route.init) {
                                NavigationLink(value: route) { itemText(item) }
                            } else {
                                itemText(item)
                            }
                        }
                    }
                    .listRowBackground(Theme.background)
                    .task { await pager.loadMore(after: item) }
                }
                PagerFooter(pager: pager, emptyText: L("board.noItems")).listRowBackground(Color.clear)
            }
        }
        .listStyle(.plain)
        .searchable(text: $text, prompt: Text(verbatim: L("board.filter")))
        .refreshable { await pager?.reload() }
        .task(id: text) {
            let client = session.client, code = code, q = text.isEmpty ? nil : text
            let p = Pager<BoardItem> { offset in
                let page = try await client.boardItems(code: code, text: q, offset: offset)
                return (page.items, page.total, nil)
            }
            pager = p
            selected = []
            await p.reload()
        }
        .navigationTitle(Text(verbatim: Loc.render("board.groups.\(code).title", [:], fallback: code)))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .bottomBar) {
                if !actions.isEmpty {
                    Menu {
                        ForEach(actions, id: \.self) { a in
                            Button { Task { await run(a) } } label: { Text(verbatim: L("board.actions.\(a)")) }
                        }
                    } label: {
                        Text(verbatim: selected.isEmpty ? L("board.actionsLabel") : L("board.actionsSelected", ["count": String(selected.count)]))
                    }
                    .disabled(selected.isEmpty)
                }
            }
            ToolbarItem(placement: .topBarTrailing) {
                if !actions.isEmpty, let pager {
                    Button(L(selected.count == pager.items.count && !selected.isEmpty ? "board.selectNone" : "board.selectAll")) {
                        selected = selected.count == pager.items.count ? [] : Set(pager.items.map(\.key))
                    }
                }
            }
        }
        .screenStyle()
        .toast($toast)
    }

    private func itemText(_ item: BoardItem) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(verbatim: item.title).font(.callout.weight(.semibold)).foregroundStyle(Theme.text)
            Text(verbatim: Describe.boardItem(item)).font(.caption).foregroundStyle(Theme.textMuted).fixedSize(horizontal: false, vertical: true)
            if let n = item.searches, n > 0 {
                Text(verbatim: L("board.searches", ["count": String(n)])).font(.caption2).foregroundStyle(Theme.textFaint)
            }
        }
    }

    private func run(_ action: String) async {
        do {
            let r = try await session.client.boardAction(action, keys: Array(selected))
            toast = L("board.result", ["done": String(r.done), "failed": String(r.failed?.count ?? 0)])
            selected = []
            await pager?.reload()
        } catch { toast = error.userMessage }
    }
}
