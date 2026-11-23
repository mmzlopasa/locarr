import SwiftUI

/// A row id that makes any value Identifiable for paging.
struct Keyed<Value: Sendable>: Identifiable, Sendable {
    let id: String
    let value: Value
}

// MARK: Wanted

struct WantedView: View {
    @Environment(Session.self) private var session
    @State private var media = "movies"
    @State private var kind = "missing"
    @State private var pager: Pager<Keyed<WantedItem>>?
    @State private var toast: String?

    var body: some View {
        List {
            ScreenHeaderRow()
            SegmentPicker(options: ["movies", "episodes", "albums"], selection: $media, label: L("wanted.media")) { L("wanted.mediaKinds.\($0)") }
                .segmentRow()
            SegmentPicker(options: ["missing", "cutoff_unmet"], selection: $kind, label: L("wanted.media")) { L("wanted.tabs.\($0)") }
                .segmentRow()
            if let pager {
                ForEach(pager.items) { row in
                    NavigationLink(value: row.value.route) {
                        LibraryRow(poster: row.value.poster, title: row.value.title, subtitle: row.value.subtitle, status: row.value.status,
                                   tone: kind == "missing" ? "danger" : "warning", square: media == "albums")
                    }
                    .listRowBackground(Theme.background)
                    .swipeActions {
                        Button { Task { await search([row.value]) } } label: {
                            Label { Text(verbatim: L("library.search")) } icon: { Image(systemName: "magnifyingglass") }
                        }
                        .tint(Theme.accent)
                    }
                    .task { await pager.loadMore(after: row) }
                }
                PagerFooter(pager: pager, emptyText: L("wanted.empty")).listRowBackground(Color.clear)
            }
        }
        .listStyle(.plain)
        .refreshable { await pager?.reload() }
        .task(id: "\(media)|\(kind)") { await setUp() }
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                if let pager, !pager.items.isEmpty {
                    Button(L("wanted.searchPage")) { Task { await search(pager.items.map(\.value)) } }
                }
            }
        }
        .screenTitle(L("nav.wanted"))
        .screenStyle()
        .toast($toast)
    }

    private func setUp() async {
        let client = session.client, media = media, kind = kind, base = session.server.url
        let p = Pager<Keyed<WantedItem>> { offset in
            switch media {
            case "episodes":
                let list = try await client.wantedEpisodes(kind: kind, offset: offset)
                return (list.items.map { Keyed(id: "e\($0.id)", value: WantedItem(episode: $0, base: base)) }, list.total, nil)
            case "albums":
                let list = try await client.wantedAlbums(status: kind, offset: offset)
                return (list.items.map { Keyed(id: "a\($0.id)", value: WantedItem(album: $0, base: base)) }, list.total, nil)
            default:
                let list = try await client.movies(status: kind, sort: "digital", desc: true, offset: offset, limit: 50)
                return (list.items.map { Keyed(id: "m\($0.id)", value: WantedItem(movie: $0, base: base)) }, list.total, nil)
            }
        }
        pager = p
        await p.reload()
    }

    private func search(_ items: [WantedItem]) async {
        let client = session.client
        do {
            let movies = items.compactMap(\.movieId), episodes = items.compactMap(\.episodeId), albums = items.compactMap(\.albumId)
            var count = 0
            if !movies.isEmpty { count += try await client.searchMovies(movies).count }
            if !episodes.isEmpty { count += try await client.searchEpisodes(episodes).count }
            for a in albums { count += try await client.searchAlbum(a).count }
            toast = L("wanted.queued", ["count": String(count)])
        } catch { toast = error.userMessage }
    }
}

struct WantedItem: Sendable {
    var title: String
    var subtitle: String
    var status: String
    var poster: URL?
    var route: Route
    var movieId: Int64?
    var episodeId: Int64?
    var albumId: Int64?

    init(movie m: Movie, base: URL) {
        title = m.title
        subtitle = [m.year.map { String($0) } ?? "", m.digitalRelease.map { Fmt.date($0) } ?? ""].filter { !$0.isEmpty && $0 != "0" }.joined(separator: " · ")
        status = m.file?.quality?.name ?? L("library.status.missing")
        poster = ImageURL.tmdb(base, m.poster, size: "w185")
        route = .movie(m.id)
        movieId = m.id
    }

    init(episode e: Episode, base: URL) {
        title = "\(e.seriesTitle ?? "") \(e.code)"
        subtitle = [e.title ?? "", Fmt.date(e.airDate)].filter { !$0.isEmpty }.joined(separator: " · ")
        status = e.file?.quality?.name ?? L("library.status.missing")
        poster = ImageURL.tmdb(base, e.poster, size: "w185")
        route = e.seriesId.map { Route.series($0) } ?? .timeline(ref: e.ref, title: title)
        episodeId = e.id
    }

    init(album a: Album, base: URL) {
        title = a.title
        subtitle = [a.artistName ?? "", String((a.releaseDate ?? "").prefix(4))].filter { !$0.isEmpty }.joined(separator: " · ")
        status = a.quality?.name ?? "\(a.fileCount ?? 0)/\(a.trackCount ?? 0)"
        poster = ImageURL.cover(base, a.cover, size: "250")
        route = .album(a.id)
        albumId = a.id
    }
}

// MARK: Calendar

struct CalendarRow: View {
    let entry: CalendarEntry
    var body: some View {
        let route: Route? = entry.movieId.map { Route.movie($0) } ?? entry.seriesId.map { Route.series($0) }
        let content = HStack(spacing: 10) {
            Image(systemName: entry.kind == "episode" ? "tv" : "film").foregroundStyle(entry.hasFile == true ? Theme.success : Theme.textMuted).frame(width: 20)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: entry.title + (entry.kind == "episode" ? String(format: " S%02dE%02d", entry.seasonNumber ?? 0, entry.episodeNumber ?? 0) : ""))
                    .font(.callout.weight(.medium)).foregroundStyle(Theme.text).lineLimit(1)
                Text(verbatim: [Loc.render("calendar.kinds.\(entry.kind)", [:], fallback: entry.kind), entry.episodeTitle ?? "",
                                entry.airDateUtc.flatMap { Fmt.parse($0) }.map { $0.formatted(Date.FormatStyle(date: .omitted, time: .shortened).locale(Loc.locale)) } ?? "",
                                entry.network ?? ""]
                    .filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(.caption).foregroundStyle(Theme.textMuted).lineLimit(1)
            }
            Spacer()
            if entry.hasFile == true { Image(systemName: "checkmark.circle.fill").foregroundStyle(Theme.success).accessibilityLabel(Text(verbatim: L("library.status.downloaded"))) }
        }
        return Group {
            if let route { NavigationLink(value: route) { content }.buttonStyle(.plain) } else { content }
        }
        .accessibilityElement(children: .combine)
    }
}

struct AlbumCalendarRow: View {
    let entry: AlbumCalendarEntry
    var body: some View {
        NavigationLink(value: Route.album(entry.albumId)) {
            HStack(spacing: 10) {
                Image(systemName: "music.note").foregroundStyle(entry.hasFiles == true ? Theme.success : Theme.textMuted).frame(width: 20)
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: entry.title).font(.callout.weight(.medium)).foregroundStyle(Theme.text).lineLimit(1)
                    Text(verbatim: [L("calendar.kinds.album"), entry.artistName ?? ""].filter { !$0.isEmpty }.joined(separator: " · "))
                        .font(.caption).foregroundStyle(Theme.textMuted).lineLimit(1)
                }
                Spacer()
            }
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
    }
}

/// The next weeks as an agenda (day headers, releases and episodes under them).
struct CalendarAgendaView: View {
    @Environment(Session.self) private var session
    @State private var start = Calendar.current.startOfDay(for: Date())
    @State private var entries: [CalendarEntry] = []
    @State private var albums: [AlbumCalendarEntry] = []
    @State private var loading = true
    @State private var error: String?

    private let span: TimeInterval = 14 * 86_400

    var body: some View {
        List {
            ScreenHeaderRow()
            Section {
                HStack {
                    Button { start = start.addingTimeInterval(-span) } label: { Image(systemName: "chevron.left") }
                        .accessibilityLabel(Text(verbatim: L("calendar.previous")))
                    Spacer()
                    Text(verbatim: "\(Fmt.date(Fmt.dayString(start))) – \(Fmt.date(Fmt.dayString(start.addingTimeInterval(span - 86_400))))")
                        .font(.subheadline.weight(.semibold))
                    Spacer()
                    Button { start = start.addingTimeInterval(span) } label: { Image(systemName: "chevron.right") }
                        .accessibilityLabel(Text(verbatim: L("calendar.next")))
                }
                .buttonStyle(.borderless)
            }
            .listRowBackground(Theme.surface)
            if let error { ErrorState(message: error) { await load() }.listRowBackground(Color.clear) }
            let days = groupedDays
            if days.isEmpty && !loading && error == nil {
                EmptyState(text: L("calendar.empty"), systemImage: "calendar").listRowBackground(Color.clear)
            }
            ForEach(days, id: \.self) { day in
                Section {
                    ForEach(Array(entries.filter { $0.date.prefix(10) == day }.enumerated()), id: \.offset) { _, e in
                        CalendarRow(entry: e).listRowBackground(Theme.background)
                    }
                    ForEach(Array(albums.filter { $0.date.prefix(10) == day }.enumerated()), id: \.offset) { _, a in
                        AlbumCalendarRow(entry: a).listRowBackground(Theme.background)
                    }
                } header: {
                    Text(verbatim: Fmt.day(day).map(Fmt.weekday) ?? day)
                        .foregroundStyle(day == Fmt.dayString(Date()) ? Theme.accent : Theme.textMuted)
                }
            }
        }
        .listStyle(.plain)
        .refreshable { await load() }
        .task(id: start) { await load() }
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button(L("calendar.today")) { start = Calendar.current.startOfDay(for: Date()) }
            }
        }
        .screenTitle(L("nav.calendar"))
        .screenStyle()
    }

    private var groupedDays: [String] {
        Array(Set(entries.map { String($0.date.prefix(10)) } + albums.map { String($0.date.prefix(10)) })).sorted()
    }

    private func load() async {
        loading = true
        defer { loading = false }
        let client = session.client
        let s = Fmt.dayString(start), e = Fmt.dayString(start.addingTimeInterval(span - 86_400))
        do {
            entries = try await client.calendar(start: s, end: e)
            albums = (try? await client.musicCalendar(start: s, end: e)) ?? []
            error = nil
        } catch {
            self.error = error.userMessage
        }
    }
}

// MARK: History

struct HistoryView: View {
    @Environment(Session.self) private var session
    @State private var category = "all"
    @State private var text = ""
    @State private var pager: Pager<HistoryEntry>?

    private let categories = ["all", "grabbed", "imported", "upgraded", "failed", "deleted", "renamed", "subtitles", "notifications"]

    var body: some View {
        List {
            ScreenHeaderRow()
            ChipRow(options: categories, selection: $category) { $0 == "all" ? L("history.all") : L("history.categories.\($0)") }
                .segmentRow()
            if let pager {
                ForEach(pager.items) { h in
                    Group {
                        if let ref = h.itemRef, let route = DeepLink.forItemRef(ref).flatMap(Route.init) {
                            NavigationLink(value: route) { row(h) }
                        } else { row(h) }
                    }
                    .listRowBackground(Theme.background)
                    .task { await pager.loadMore(after: h) }
                }
                PagerFooter(pager: pager, emptyText: L("history.none")).listRowBackground(Color.clear)
            }
        }
        .listStyle(.plain)
        .searchable(text: $text, prompt: Text(verbatim: L("history.search")))
        .refreshable { await pager?.reload() }
        .task(id: "\(category)|\(text)") {
            let client = session.client, cat = category == "all" ? nil : category, q = text.isEmpty ? nil : text
            let p = Pager<HistoryEntry> { offset in
                let page = try await client.history(category: cat, text: q, offset: offset)
                return (page.items, nil, page.hasMore ?? false)
            }
            pager = p
            await p.reload()
        }
        .screenTitle(L("history.title"))
        .screenStyle()
    }

    private func row(_ h: HistoryEntry) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Pill(text: Loc.render("history.categories.\(h.category)", [:], fallback: h.category), tone: tone(h.category))
                Spacer()
                Text(verbatim: Fmt.relative(h.at)).font(.caption2).foregroundStyle(Theme.textFaint)
            }
            Text(verbatim: h.title).font(.callout.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(2)
            if let r = h.release, !r.isEmpty { Text(verbatim: r).font(.caption2.monospaced()).foregroundStyle(Theme.textMuted).lineLimit(2) }
            let extra = [h.indexer.map { L("history.indexer", ["name": $0]) } ?? "", h.client.map { L("history.client", ["name": $0]) } ?? ""]
                .filter { !$0.isEmpty }.joined(separator: " · ")
            if !extra.isEmpty { Text(verbatim: extra).font(.caption2).foregroundStyle(Theme.textFaint) }
            if h.category == "failed", let m = h.message, !m.isEmpty { Text(verbatim: m).font(.caption).foregroundStyle(Theme.danger) }
        }
        .accessibilityElement(children: .combine)
    }

    private func tone(_ c: String) -> String {
        switch c {
        case "imported", "upgraded": return "success"
        case "failed": return "danger"
        case "deleted": return "warning"
        case "grabbed": return "info"
        default: return "neutral"
        }
    }
}
