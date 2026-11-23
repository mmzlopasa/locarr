import SwiftUI

/// One request in a list.
struct RequestRow: View {
    let request: MediaRequest
    var compact = false
    @Environment(Session.self) private var session

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            if !compact {
                Poster(url: request.isMusic ? ImageURL.cover(session.server.url, request.poster, size: "250") : ImageURL.tmdb(session.server.url, request.poster),
                       title: request.title, square: request.isMusic, width: 56)
            }
            VStack(alignment: .leading, spacing: 5) {
                HStack(spacing: 6) {
                    Text(verbatim: request.title).font(.callout.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(2)
                    if request.is4k == true { Pill(text: "4K") }
                }
                Text(verbatim: meta).font(.caption).foregroundStyle(Theme.textMuted).lineLimit(2)
                HStack(spacing: 6) {
                    Pill(text: Describe.requestStatus(request.status), tone: Theme.tone(for: request.status))
                    if let p = request.progress, request.status == .processing || request.status == .partial, (p.downloads ?? 0) > 0 {
                        Text(verbatim: Fmt.percent((p.percent ?? 0) / 100)).font(.caption2.monospacedDigit()).foregroundStyle(Theme.info)
                    }
                }
                if let p = request.progress, (p.downloads ?? 0) > 0, request.status == .processing || request.status == .partial {
                    ProgressBar(value: (p.percent ?? 0) / 100, tint: Theme.info)
                }
            }
            Spacer(minLength: 0)
        }
        .card(padding: 10)
        .accessibilityElement(children: .combine)
    }

    private var meta: String {
        var parts = [Describe.requestKind(request.kind)]
        if let y = request.year, y > 0 { parts.append(String(y)) }
        if let s = Describe.requestSeasons(request) { parts.append(s) }
        if session.canManageRequests, let by = request.requestedBy { parts.append(L("requests.byWhen", ["user": by.username, "when": Fmt.relative(request.createdAt)])) }
        else { parts.append(Fmt.relative(request.createdAt)) }
        return parts.joined(separator: " · ")
    }
}

/// My requests (everyone's for those who manage requests), with approval for pending ones.
struct RequestsView: View {
    @Environment(Session.self) private var session
    @State private var filter = "active"
    @State private var mine = false
    @State private var pager: Pager<MediaRequest>?
    @State private var selection: Set<Int64> = []
    @State private var toast: String?
    @State private var summary: RequestSummary?

    private var filters: [String] { ["active", "pending", "processing", "available", "failed", "declined", "all"] }

    var body: some View {
        List {
            ChipScroller {
                if session.canManageRequests {
                    Chip(text: L(mine ? "requests.viewMine" : "requests.viewAll"), selected: true) { mine.toggle() }
                }
                ForEach(filters, id: \.self) { f in
                    Chip(text: L("requests.filters.\(f)"), selected: filter == f) { filter = f }
                }
            }
            .segmentRow()
            if !session.canManageRequests || mine, let quota = quotaLine {
                Text(verbatim: quota).font(.footnote).foregroundStyle(Theme.textMuted)
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
            }

            if let pager {
                Section {
                    ForEach(pager.items) { r in
                        NavigationLink(value: Route.request(r.id)) { RequestRow(request: r) }
                            .listRowBackground(Color.clear)
                            .listRowSeparator(.hidden)
                            .listRowInsets(EdgeInsets(top: 4, leading: 16, bottom: 4, trailing: 16))
                            .task { await pager.loadMore(after: r) }
                            .swipeActions(edge: .trailing) {
                                if session.canManageRequests && r.status == .pending {
                                    Button { Task { await approve(r) } } label: { Label { Text(verbatim: L("requests.approve")) } icon: { Image(systemName: "checkmark") } }
                                        .tint(Theme.accent)
                                }
                            }
                    }
                    PagerFooter(pager: pager, emptyText: L("requests.none"))
                        .listRowBackground(Color.clear)
                        .listRowSeparator(.hidden)
                }
            }
        }
        .listStyle(.plain)
        .refreshable { await reload() }
        .task(id: "\(filter)|\(mine)|\(session.canManageRequests)") { await setUp() }
        .navigationTitle(Text(verbatim: L("requests.title")))
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                let pending = pager?.items.filter { $0.status == .pending } ?? []
                if session.canManageRequests && !pending.isEmpty {
                    Button(L("requests.approveAll")) { Task { await approveAll(pending) } }
                }
            }
        }
        .screenStyle()
        .toast($toast)
    }

    private var quotaLine: String? {
        guard let q = summary?.quotas else { return nil }
        let parts: [(String, QuotaStatus?)] = [(L("mediaKinds.movies"), q.movie), (L("mediaKinds.series"), q.series), (L("mediaKinds.music"), q.music)]
        let limited = parts.compactMap { name, s -> String? in
            guard let s, s.unlimited != true else { return nil }
            return "\(name): \(s.remaining ?? 0)/\(s.limit ?? 0)"
        }
        return limited.isEmpty ? nil : L("requests.quotaShort", ["list": limited.joined(separator: ", ")])
    }

    private func setUp() async {
        let client = session.client, filter = filter
        let mineOnly = !session.canManageRequests || mine
        let p = Pager<MediaRequest> { offset in
            let list = try await client.requests(status: filter, mine: mineOnly ? true : nil, offset: offset)
            return (list.items, list.total, nil)
        }
        pager = p
        await p.reload()
        summary = try? await client.requestSummary()
    }

    private func reload() async {
        await pager?.reload()
        summary = try? await session.client.requestSummary()
        await session.pollNow()
    }

    private func approve(_ r: MediaRequest) async {
        do {
            _ = try await session.client.approve(r.id)
            toast = L("requests.approvedToast")
            await reload()
        } catch { toast = error.userMessage }
    }

    private func approveAll(_ items: [MediaRequest]) async {
        do {
            let result = try await session.client.approve(ids: items.map(\.id))
            toast = L("requests.approvedMany", ["done": String(result.done), "failed": String(result.errors?.count ?? 0)])
            await reload()
        } catch { toast = error.userMessage }
    }
}

/// One request: status, why it is not here yet, progress, decision, comments.
struct RequestDetailView: View {
    let id: Int64
    @Environment(Session.self) private var session
    @State private var request = Loader<MediaRequest>()
    @State private var declining = false
    @State private var reason = ""
    @State private var toast: String?
    @State private var busy = false

    var body: some View {
        ScrollView {
            LoadView(loader: request, reload: load) { r in
                VStack(alignment: .leading, spacing: 18) {
                    HStack(alignment: .top, spacing: 14) {
                        Poster(url: r.isMusic ? ImageURL.cover(session.server.url, r.poster) : ImageURL.tmdb(session.server.url, r.poster),
                               title: r.title, square: r.isMusic, width: 100)
                        VStack(alignment: .leading, spacing: 6) {
                            Text(verbatim: r.title).font(.title3.weight(.bold)).foregroundStyle(Theme.text)
                            Text(verbatim: L("requests.detailSubtitle", ["id": String(r.id), "when": Fmt.dateTime(r.createdAt)]))
                                .font(.footnote).foregroundStyle(Theme.textMuted)
                            HStack {
                                Pill(text: Describe.requestStatus(r.status), tone: Theme.tone(for: r.status))
                                if r.is4k == true { Pill(text: "4K") }
                                Pill(text: Describe.requestKind(r.kind))
                            }
                        }
                    }
                    if let s = Describe.requestSeasons(r) { InfoRow(label: L("requests.seasonsField"), value: s) }
                    if let by = r.requestedBy { InfoRow(label: L("requests.requestedBy"), value: by.username) }
                    if let decided = r.decidedBy {
                        Text(verbatim: L(r.status == .declined ? "requests.declinedBy" : "requests.approvedBy", ["user": decided.username]))
                            .font(.footnote).foregroundStyle(Theme.textMuted)
                    } else if r.autoApproved == true {
                        Text(verbatim: L("requests.auto")).font(.footnote).foregroundStyle(Theme.textMuted)
                    }
                    if let reason = r.declineReason, !reason.isEmpty {
                        Callout(text: L("requests.declinedBecause", ["reason": reason]), tone: "danger")
                    }
                    if let ex = r.explanation {
                        Callout(text: Describe.explanation(ex), tone: ex.tone)
                    }
                    if let p = r.progress, (p.downloads ?? 0) > 0 {
                        VStack(alignment: .leading, spacing: 6) {
                            Text(verbatim: L("requests.progress")).font(.subheadline.weight(.semibold)).foregroundStyle(Theme.text)
                            ProgressBar(value: (p.percent ?? 0) / 100, tint: Theme.info)
                            Text(verbatim: [Fmt.percent((p.percent ?? 0) / 100), Describe.downloadState(p.state), p.etaSeconds.map { Fmt.duration(seconds: $0) } ?? ""]
                                .filter { !$0.isEmpty }.joined(separator: " · "))
                                .font(.caption).foregroundStyle(Theme.textMuted)
                        }
                        .card()
                    }
                    actions(r)
                    if let ref = titleRoute(r) {
                        NavigationLink(value: ref) {
                            Label { Text(verbatim: L("requests.openTitle")) } icon: { Image(systemName: "arrow.up.right.square") }
                        }
                        .buttonStyle(SecondaryButtonStyle())
                    }
                    CommentsSection(load: { [client = session.client] in try await client.requestComments(r.id) },
                                    add: { [client = session.client] text in try await client.addRequestComment(r.id, text) })
                }
                .padding()
            }
        }
        .refreshable { await load() }
        .task { if request.value == nil { await load() } }
        .navigationTitle(Text(verbatim: request.value?.title ?? L("requests.title")))
        .navigationBarTitleDisplayMode(.inline)
        .screenStyle()
        .alert(Text(verbatim: L("requests.declineConfirm")), isPresented: $declining) {
            TextField(text: $reason, prompt: Text(verbatim: L("requests.declineReason"))) { Text(verbatim: L("requests.declineReason")) }
            Button(L("common.cancel"), role: .cancel) {}
            Button(L("requests.decline"), role: .destructive) {
                let why = reason.isEmpty ? nil : reason
                Task { await act { try await $0.decline(id, reason: why) } }
            }
        }
        .toast($toast)
    }

    private func titleRoute(_ r: MediaRequest) -> Route? {
        if session.canManageLibrary, let ref = r.libraryRef, let link = DeepLink.forItemRef(ref), let route = Route(link) { return route }
        switch r.kind {
        case "movie": return r.tmdbId.map { .title(.movie($0)) }
        case "series": return r.tmdbId.map { .title(.series($0)) }
        case "artist": return r.mbid.map { .title(.artist($0)) }
        case "album": return r.artistMbid.map { .title(.artist($0)) }
        default: return nil
        }
    }

    @ViewBuilder private func actions(_ r: MediaRequest) -> some View {
        VStack(spacing: 10) {
            if session.canManageRequests && r.status == .pending {
                HStack(spacing: 10) {
                    Button { Task { await act { try await $0.approve(id) } } } label: { Text(verbatim: L("requests.approve")) }
                        .buttonStyle(PrimaryButtonStyle())
                    Button { declining = true } label: { Text(verbatim: L("requests.decline")) }
                        .buttonStyle(SecondaryButtonStyle(tint: Theme.danger))
                }
            }
            if session.canManageRequests && (r.status == .failed || r.status == .processing || r.status == .partial) {
                Button { Task { await act { try await $0.retryRequest(id) } } } label: { Text(verbatim: L("requests.retry")) }
                    .buttonStyle(SecondaryButtonStyle())
            }
            if r.canCancel == true {
                Button { Task { await act { try await $0.cancelRequest(id) } } } label: { Text(verbatim: L("requests.cancel")) }
                    .buttonStyle(SecondaryButtonStyle(tint: Theme.danger))
            }
        }
        .disabled(busy)
    }

    private func act(_ op: (APIClient) async throws -> MediaRequest) async {
        busy = true
        defer { busy = false }
        do {
            request.value = try await op(session.client)
            toast = L("common.saved")
            await session.pollNow()
        } catch {
            toast = error.userMessage
        }
    }

    private func load() async {
        let client = session.client, id = id
        await request.load { try await client.request(id) }
    }
}

/// Comments under a request or an issue.
struct CommentsSection: View {
    let load: @Sendable () async throws -> [Comment]
    let add: @Sendable (String) async throws -> Comment
    @State private var comments: [Comment] = []
    @State private var draft = ""
    @State private var error: String?
    @State private var sending = false

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            SectionTitle(text: L("comments.title"))
            if comments.isEmpty { Text(verbatim: L("comments.none")).font(.footnote).foregroundStyle(Theme.textMuted) }
            ForEach(comments) { c in
                VStack(alignment: .leading, spacing: 4) {
                    HStack {
                        Text(verbatim: c.user.username).font(.footnote.weight(.semibold)).foregroundStyle(Theme.text)
                        Spacer()
                        Text(verbatim: Fmt.relative(c.createdAt)).font(.caption2).foregroundStyle(Theme.textFaint)
                    }
                    Text(verbatim: c.body).font(.callout).foregroundStyle(Theme.text)
                }
                .card(padding: 10)
                .accessibilityElement(children: .combine)
            }
            HStack(alignment: .bottom, spacing: 8) {
                TextField(text: $draft, prompt: Text(verbatim: L("comments.add")), axis: .vertical) { Text(verbatim: L("comments.add")) }
                    .lineLimit(1...4)
                    .fieldStyle()
                Button {
                    Task { await send() }
                } label: { Image(systemName: "arrow.up.circle.fill").font(.title) }
                .disabled(draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || sending)
                .accessibilityLabel(Text(verbatim: L("comments.send")))
            }
            if let error { Text(verbatim: error).font(.footnote).foregroundStyle(Theme.danger) }
        }
        .task { comments = (try? await load()) ?? comments }
    }

    private func send() async {
        sending = true
        defer { sending = false }
        do {
            let c = try await add(draft.trimmingCharacters(in: .whitespacesAndNewlines))
            comments.append(c)
            draft = ""
            error = nil
        } catch {
            self.error = error.userMessage
        }
    }
}
