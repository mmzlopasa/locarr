import SwiftUI

/// A title on the discover side: what it is, where it stands, request it, report a problem.
struct TitleDetailView: View {
    let ref: TitleRef
    @Environment(Session.self) private var session
    @State private var detail = Loader<TitleDetail>()
    @State private var requesting: RequestDraft?
    @State private var reporting = false
    @State private var toast: String?

    var body: some View {
        ScrollView {
            LoadView(loader: detail, reload: load) { d in content(d) }
        }
        .ignoresSafeArea(edges: .top)
        .refreshable { await load() }
        .task { if detail.value == nil { await load() } }
        .navigationTitle(Text(verbatim: detail.value?.item.title ?? ""))
        .navigationBarTitleDisplayMode(.inline)
        .toolbarBackground(.hidden, for: .navigationBar)
        .screenStyle()
        .sheet(item: $requesting) { draft in
            RequestSheet(draft: draft) { message in
                toast = message
                Task { await load() }
            }
            .environment(session)
        }
        .sheet(isPresented: $reporting) {
            if let d = detail.value {
                ReportIssueSheet(detail: d) { toast = L("issues.sent") }.environment(session)
            }
        }
        .toast($toast)
    }

    @ViewBuilder private func content(_ d: TitleDetail) -> some View {
        let item = d.item
        let music = item.mediaType.isMusic
        VStack(alignment: .leading, spacing: 22) {
            ZStack(alignment: .bottomLeading) {
                RemoteImage(url: music ? nil : ImageURL.tmdb(session.server.url, item.backdrop, size: "w1280"), title: "", maxPixel: 1400)
                    .frame(height: 300)
                    .overlay(LinearGradient(colors: [Theme.background.opacity(0.2), Theme.background], startPoint: .top, endPoint: .bottom))
                HStack(alignment: .bottom, spacing: 16) {
                    Poster(url: ImageURL.item(session.server.url, item, large: true), title: item.title, square: music, width: 116)
                        .shadow(color: .black.opacity(0.5), radius: 16, y: 8)
                    VStack(alignment: .leading, spacing: 6) {
                        Text(verbatim: item.title).font(.title2.weight(.bold)).foregroundStyle(Theme.text).lineLimit(3)
                        Text(verbatim: facts(d)).font(.footnote).foregroundStyle(Theme.textMuted).lineLimit(2)
                        HStack(spacing: 6) {
                            if let s = item.status, s != .none { Pill(text: Describe.discoverStatus(s), tone: Theme.tone(for: s)) }
                            if let s = item.status4k, s != .none { Pill(text: "4K · " + Describe.discoverStatus(s), tone: Theme.tone(for: s)) }
                        }
                    }
                }
                .padding(.horizontal)
            }
            .frame(height: 300)

            actions(d).padding(.horizontal)

            if let tagline = d.tagline, !tagline.isEmpty {
                Text(verbatim: tagline).font(.callout.italic()).foregroundStyle(Theme.textMuted).padding(.horizontal)
            }
            if let overview = item.overview, !overview.isEmpty {
                ExpandableText(text: overview, lines: 5).padding(.horizontal)
            }
            if let genres = d.genres, !genres.isEmpty {
                Text(verbatim: genres.joined(separator: " · ")).font(.footnote).foregroundStyle(Theme.textFaint).padding(.horizontal)
            }

            if let requests = d.requests, !requests.isEmpty {
                VStack(alignment: .leading, spacing: 10) {
                    SectionTitle(text: L("title.requests"))
                    ForEach(requests) { r in
                        NavigationLink(value: Route.request(r.id)) { RequestRow(request: r, compact: true) }
                            .buttonStyle(PressStyle())
                    }
                }
                .padding(.horizontal)
            }

            if let seasons = d.seasons, !seasons.isEmpty {
                VStack(alignment: .leading, spacing: 10) {
                    SectionTitle(text: L("title.seasons"))
                    ForEach(seasons, id: \.number) { s in seasonRow(s) }
                }
                .padding(.horizontal)
            }

            if item.mediaType == .artist {
                VStack(alignment: .leading, spacing: 10) {
                    SectionTitle(text: L("title.albums"))
                    if (d.albums ?? []).isEmpty {
                        Text(verbatim: L("title.noAlbums")).foregroundStyle(Theme.textMuted)
                    }
                    ForEach(d.albums ?? []) { album in albumRow(album, ability: d.ability) }
                }
                .padding(.horizontal)
            }

            if let cast = d.cast, !cast.isEmpty {
                VStack(alignment: .leading, spacing: 10) {
                    SectionTitle(text: L("title.cast")).padding(.horizontal)
                    ScrollView(.horizontal, showsIndicators: false) {
                        LazyHStack(alignment: .top, spacing: 12) {
                            ForEach(Array(cast.prefix(20).enumerated()), id: \.offset) { _, c in castCard(c) }
                        }
                        .padding(.horizontal)
                    }
                }
            }
        }
        .padding(.bottom, 32)
    }

    private func facts(_ d: TitleDetail) -> String {
        var parts: [String] = []
        if let y = d.item.year, y > 0 { parts.append(String(y)) }
        if let r = d.runtime, r > 0 { parts.append(L("title.minutes", ["n": String(r)])) }
        if let c = d.certification, !c.isEmpty { parts.append(c) }
        if let n = d.network ?? d.studio, !n.isEmpty { parts.append(n) }
        if d.item.mediaType.isMusic, let s = d.item.subtitle, !s.isEmpty { parts.append(s) }
        return parts.joined(separator: " · ")
    }

    @ViewBuilder private func actions(_ d: TitleDetail) -> some View {
        let ability = d.ability ?? RequestAbility()
        let item = d.item
        VStack(spacing: 10) {
            if ability.canRequest == true {
                Button {
                    requesting = RequestDraft(detail: d, is4k: false)
                } label: {
                    Label { Text(verbatim: requestTitle(d)) } icon: { Image(systemName: "plus") }
                }
                .buttonStyle(PrimaryButtonStyle())
            }
            HStack(spacing: 10) {
                if ability.canRequest4k == true && !item.mediaType.isMusic {
                    Button { requesting = RequestDraft(detail: d, is4k: true) } label: { Text(verbatim: L("request.fourK")) }
                        .buttonStyle(SecondaryButtonStyle())
                }
                if ability.canReportIssue == true {
                    Button { reporting = true } label: {
                        Label { Text(verbatim: L("title.reportIssue")) } icon: { Image(systemName: "exclamationmark.bubble") }
                    }
                    .buttonStyle(SecondaryButtonStyle())
                }
                if let ref = d.libraryRef, let link = DeepLink.forItemRef(ref), let route = Route(link) {
                    NavigationLink(value: route) {
                        Label { Text(verbatim: L("title.openLibrary")) } icon: { Image(systemName: "square.stack.3d.up") }
                    }
                    .buttonStyle(SecondaryButtonStyle())
                }
            }
        }
    }

    private func requestTitle(_ d: TitleDetail) -> String {
        if d.item.mediaType == .artist { return L("title.requestArtist") }
        let some = (d.seasons ?? []).contains { $0.status != nil && $0.status != DiscoverStatus.none }
        return some ? L("title.requestMore") : L("title.request")
    }

    private func seasonRow(_ s: SeasonAvailability) -> some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: s.number == 0 ? L("request.specials") : (s.title?.isEmpty == false ? s.title! : L("request.season", ["n": String(s.number)])))
                    .font(.callout.weight(.semibold)).foregroundStyle(Theme.text)
                Text(verbatim: seasonFacts(s)).font(.caption).foregroundStyle(Theme.textMuted)
            }
            Spacer()
            if let st = s.status, st != .none { Pill(text: Describe.discoverStatus(st), tone: Theme.tone(for: st)) }
        }
        .card(padding: 12)
        .accessibilityElement(children: .combine)
    }

    private func seasonFacts(_ s: SeasonAvailability) -> String {
        var parts = [L("request.episodes", ["count": String(s.episodeCount ?? 0)])]
        if let here = s.episodesAvailable, here > 0 { parts.append(L("title.episodesHere", ["n": String(here)])) }
        if let a = s.airDate, !a.isEmpty { parts.append(Fmt.date(a)) }
        return parts.joined(separator: " · ")
    }

    private func albumRow(_ album: DiscoverItem, ability: RequestAbility?) -> some View {
        HStack(spacing: 12) {
            Poster(url: ImageURL.item(session.server.url, album), title: album.title, square: true, width: 54)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: album.title).font(.callout.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(2)
                Text(verbatim: [album.year.map { String($0) } ?? "", album.subtitle ?? ""].filter { !$0.isEmpty && $0 != "0" }.joined(separator: " · "))
                    .font(.caption).foregroundStyle(Theme.textMuted)
            }
            Spacer()
            if let st = album.status, st != .none {
                Pill(text: Describe.discoverStatus(st), tone: Theme.tone(for: st))
            } else if ability?.canRequest == true {
                Button {
                    requesting = RequestDraft(album: album)
                } label: { Text(verbatim: L("title.request")) }
                .buttonStyle(SecondaryButtonStyle())
            }
        }
        .card(padding: 10)
    }

    private func castCard(_ c: CastMember) -> some View {
        let card = VStack(alignment: .leading, spacing: 4) {
            Poster(url: ImageURL.tmdb(session.server.url, c.profile, size: "w185"), title: c.name, width: 92)
            Text(verbatim: c.name).font(.caption.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(2)
            if let ch = c.character, !ch.isEmpty { Text(verbatim: ch).font(.caption2).foregroundStyle(Theme.textMuted).lineLimit(2) }
        }
        .frame(width: 92, alignment: .leading)
        return Group {
            if let id = c.personId {
                NavigationLink(value: Route.person(id)) { card }.buttonStyle(PressStyle())
            } else {
                card
            }
        }
    }

    private func load() async {
        let client = session.client, ref = ref
        await detail.load { try await client.title(ref) }
    }
}

/// What is about to be requested.
struct RequestDraft: Identifiable {
    var id = UUID()
    var kind: String
    var title: String
    var tmdbId: Int64?
    var mbid: String?
    var is4k: Bool
    var seasons: [SeasonAvailability]
    var autoApprove: Bool

    init(detail d: TitleDetail, is4k: Bool) {
        kind = d.item.mediaType == .movie ? "movie" : d.item.mediaType == .series ? "series" : "artist"
        title = d.item.title
        tmdbId = d.item.tmdbId
        mbid = d.item.mbid
        self.is4k = is4k
        seasons = d.seasons ?? []
        autoApprove = (is4k ? d.ability?.autoApprove4k : d.ability?.autoApprove) == true
    }

    init(album: DiscoverItem) {
        kind = "album"
        title = album.title
        mbid = album.mbid
        is4k = false
        seasons = []
        autoApprove = false
    }
}

/// Request a movie, seasons of a series, an album or every album of an artist.
struct RequestSheet: View {
    let draft: RequestDraft
    var onDone: (String) -> Void
    @Environment(Session.self) private var session
    @Environment(\.dismiss) private var dismiss
    @State private var picked: Set<Int> = []
    @State private var allSeasons = true
    @State private var future = false
    @State private var comment = ""
    @State private var busy = false
    @State private var error: String?
    @State private var quota: QuotaStatus?

    private var requestable: [SeasonAvailability] {
        draft.seasons.filter { s in
            let status = draft.is4k ? s.status4k : s.status
            return status == nil || status == DiscoverStatus.none
        }
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text(verbatim: draft.title).font(.headline)
                    if draft.is4k { Text(verbatim: L("request.fourKHint")).font(.footnote).foregroundStyle(Theme.textMuted) }
                }
                if draft.kind == "series" {
                    Section {
                        Toggle(isOn: $allSeasons) {
                            VStack(alignment: .leading) {
                                Text(verbatim: L("request.allSeasons"))
                                Text(verbatim: L("request.allSeasonsHint")).font(.caption).foregroundStyle(Theme.textMuted)
                            }
                        }
                        if !allSeasons {
                            ForEach(requestable, id: \.number) { s in
                                Toggle(isOn: Binding(get: { picked.contains(s.number) }, set: { on in
                                    if on { picked.insert(s.number) } else { picked.remove(s.number) }
                                })) {
                                    Text(verbatim: s.number == 0 ? L("request.specials") : L("request.season", ["n": String(s.number)]))
                                }
                            }
                        }
                        Toggle(isOn: $future) {
                            VStack(alignment: .leading) {
                                Text(verbatim: L("request.futureSeasons"))
                                Text(verbatim: L("request.futureSeasonsHint")).font(.caption).foregroundStyle(Theme.textMuted)
                            }
                        }
                    } header: { Text(verbatim: L("request.seasons")) }
                }
                Section {
                    TextField(text: $comment, prompt: Text(verbatim: L("request.commentHint")), axis: .vertical) { Text(verbatim: L("request.comment")) }
                        .lineLimit(2...5)
                } header: { Text(verbatim: L("request.comment")) }
                if let quota { Section { Text(verbatim: quotaText(quota)).font(.footnote).foregroundStyle(Theme.textMuted) } }
                if let error { Section { Callout(text: error, tone: "danger") } }
            }
            .scrollContentBackground(.hidden)
            .background(Theme.background)
            .navigationTitle(Text(verbatim: L("request.title", ["title": draft.title])))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(L("common.cancel")) { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    if busy { ProgressView() } else {
                        Button(L("request.send")) { Task { await send() } }
                            .disabled(draft.kind == "series" && !allSeasons && picked.isEmpty)
                    }
                }
            }
            .task {
                guard let summary = try? await session.client.requestSummary() else { return }
                quota = draft.kind == "movie" ? summary.quotas?.movie : draft.kind == "series" ? summary.quotas?.series : summary.quotas?.music
            }
        }
        .presentationDetents([.medium, .large])
    }

    private func quotaText(_ q: QuotaStatus) -> String {
        if q.unlimited == true { return L("requests.quota.unlimited") }
        var text = L("requests.quota.remaining", ["remaining": String(q.remaining ?? 0), "limit": String(q.limit ?? 0), "days": String(q.days ?? 0)])
        if let next = q.nextSlotAt, (q.remaining ?? 0) == 0 { text += " " + L("requests.quota.nextSlot", ["when": Fmt.relative(next)]) }
        return text
    }

    private func send() async {
        busy = true
        defer { busy = false }
        var body = CreateRequest(kind: draft.kind, tmdbId: draft.tmdbId, mbid: draft.mbid, is4k: draft.is4k ? true : nil,
                                 comment: comment.isEmpty ? nil : comment)
        if draft.kind == "series" {
            body.allSeasons = allSeasons
            body.futureSeasons = future
            if !allSeasons { body.seasons = picked.sorted() }
        }
        do {
            let r = try await session.client.createRequest(body)
            let auto = r.status != .pending
            onDone(L(auto ? "request.autoApproved" : "request.sent"))
            dismiss()
        } catch {
            self.error = error.userMessage
        }
    }
}

/// Report a problem with a title that is in the library.
struct ReportIssueSheet: View {
    let detail: TitleDetail
    var onDone: () -> Void
    @Environment(Session.self) private var session
    @Environment(\.dismiss) private var dismiss
    @State private var type = "video"
    @State private var season = ""
    @State private var episode = ""
    @State private var message = ""
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker(selection: $type) {
                        ForEach(["video", "audio", "subtitles", "other"], id: \.self) { Text(verbatim: L("issues.types.\($0)")).tag($0) }
                    } label: { Text(verbatim: L("issues.type")) }
                    .pickerStyle(.segmented)
                } header: { Text(verbatim: L("issues.type")) }
                if detail.item.mediaType == .series {
                    Section {
                        TextField(text: $season, prompt: Text(verbatim: L("issues.optional"))) { Text(verbatim: L("issues.season")) }.keyboardType(.numberPad)
                        TextField(text: $episode, prompt: Text(verbatim: L("issues.optional"))) { Text(verbatim: L("issues.episode")) }.keyboardType(.numberPad)
                    }
                }
                Section {
                    TextField(text: $message, prompt: Text(verbatim: L("issues.messageHint")), axis: .vertical) { Text(verbatim: L("issues.message")) }
                        .lineLimit(3...8)
                } header: { Text(verbatim: L("issues.message")) }
                if let error { Section { Callout(text: error, tone: "danger") } }
            }
            .scrollContentBackground(.hidden)
            .background(Theme.background)
            .navigationTitle(Text(verbatim: L("issues.reportTitle", ["title": detail.item.title])))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(L("common.cancel")) { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    if busy { ProgressView() } else {
                        Button(L("issues.send")) { Task { await send() } }
                            .disabled(message.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                    }
                }
            }
        }
    }

    private func send() async {
        busy = true
        defer { busy = false }
        let kind = detail.item.mediaType == .movie ? "movie" : detail.item.mediaType == .series ? "series" : "album"
        let body = IssueInput(kind: kind, tmdbId: detail.item.tmdbId, mbid: detail.item.mbid, type: type,
                              seasonNumber: Int(season), episodeNumber: Int(episode), message: message)
        do {
            _ = try await session.client.createIssue(body)
            onDone()
            dismiss()
        } catch {
            self.error = error.userMessage
        }
    }
}
