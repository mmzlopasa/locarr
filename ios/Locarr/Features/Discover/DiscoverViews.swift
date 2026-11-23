import SwiftUI

/// One title as a poster with its status; opens the title (or person).
struct PosterCard: View {
    let item: DiscoverItem
    var width: CGFloat? = 124
    @Environment(Session.self) private var session

    var body: some View {
        NavigationLink(value: route) {
            VStack(alignment: .leading, spacing: 6) {
                Poster(url: ImageURL.item(session.server.url, item), title: item.title, square: item.mediaType.isMusic, width: width)
                    .overlay(alignment: .topTrailing) { badge.padding(5) }
                Text(verbatim: item.title).font(.footnote.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(1)
                Text(verbatim: subtitle).font(.caption2).foregroundStyle(Theme.textMuted).lineLimit(1)
            }
            .frame(width: width)
            .contentShape(Rectangle())
        }
        .buttonStyle(PressStyle())
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(verbatim: [item.title, subtitle, statusText].filter { !$0.isEmpty }.joined(separator: ", ")))
        .accessibilityAddTraits(.isButton)
    }

    private var route: Route {
        if item.mediaType == .person, let id = item.tmdbId { return .person(id) }
        if let ref = TitleRef(item) { return .title(ref) }
        return .section(.discover)
    }

    private var subtitle: String {
        [item.year.map { $0 > 0 ? String($0) : "" } ?? "", item.subtitle ?? ""].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    private var statusText: String {
        guard let s = item.status, s != .none else { return "" }
        return Describe.discoverStatus(s)
    }

    @ViewBuilder private var badge: some View {
        if let s = item.status, s != .none {
            Pill(text: Describe.discoverStatus(s), tone: Theme.tone(for: s))
        } else if let s = item.status4k, s != .none {
            Pill(text: "4K", tone: Theme.tone(for: s))
        }
    }
}

/// A titled horizontal row of posters.
struct PosterRow: View {
    let title: String
    let items: [DiscoverItem]
    var seeAll: Route? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: title).font(.title3.weight(.bold)).foregroundStyle(Theme.text).accessibilityAddTraits(.isHeader)
                Spacer()
                if let seeAll {
                    NavigationLink(value: seeAll) { Text(verbatim: L("discover.seeAll")).font(.subheadline) }
                }
            }
            .padding(.horizontal)
            ScrollView(.horizontal, showsIndicators: false) {
                LazyHStack(alignment: .top, spacing: 12) {
                    ForEach(items) { PosterCard(item: $0) }
                }
                .padding(.horizontal)
            }
            .scrollClipDisabled()
        }
    }
}

/// Discover: trending, popular, upcoming… with the person's own requests on top.
struct DiscoverView: View {
    @Environment(Session.self) private var session
    @State private var home = Loader<DiscoverHome>()

    var body: some View {
        ScrollView {
            LoadView(loader: home, reload: load) { value in
                LazyVStack(alignment: .leading, spacing: 28) {
                    if value.metadataReady == false {
                        Callout(text: L("discover.noMetadata"), tone: "warning").padding(.horizontal)
                    }
                    if let hero = value.rows.first?.items.first(where: { $0.backdrop?.isEmpty == false }) {
                        DiscoverHero(item: hero)
                    }
                    if let mine = value.requests, !mine.isEmpty {
                        PosterRow(title: L("discover.rows.my_requests"), items: mine.map(Self.item), seeAll: .section(.requests))
                    }
                    ForEach(value.rows, id: \.key) { row in
                        if !row.items.isEmpty {
                            PosterRow(title: Loc.render("discover.rows.\(row.key)", [:], fallback: row.key), items: row.items,
                                      seeAll: Self.listable(row.key) ? .discoverList(key: row.key, title: Loc.render("discover.rows.\(row.key)", [:], fallback: row.key), id: nil) : nil)
                        }
                    }
                }
                .padding(.vertical)
            }
        }
        .refreshable { await load() }
        .task { if home.value == nil { await load() } }
        .navigationTitle(Text(verbatim: L("discover.title")))
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) { InboxButton() }
            ToolbarItem(placement: .topBarTrailing) {
                NavigationLink(value: Route.section(.search)) { Image(systemName: "magnifyingglass") }
                    .accessibilityLabel(Text(verbatim: L("discover.searchLabel")))
            }
        }
        .screenStyle()
    }

    private func load() async {
        let client = session.client
        await home.load { try await client.discover() }
    }

    static func listable(_ key: String) -> Bool { !["genre_movies", "genre_series", "studio", "network"].contains(key) }

    /// A request as a poster (title, poster, link).
    static func item(_ r: MediaRequest) -> DiscoverItem {
        let type: MediaType = r.kind == "movie" ? .movie : r.kind == "series" ? .series : r.kind == "album" ? .album : .artist
        let status: DiscoverStatus = switch r.status {
        case .available: .available
        case .partial: .partial
        case .processing, .approved: .processing
        case .pending: .pending
        default: .none
        }
        return DiscoverItem(mediaType: type, tmdbId: r.tmdbId, mbid: r.mbid, artistMbid: r.artistMbid, title: r.title, subtitle: r.subtitle,
                            year: r.year, poster: r.poster, status: status, requestId: r.id)
    }
}

/// The first trending title, full width.
struct DiscoverHero: View {
    let item: DiscoverItem
    @Environment(Session.self) private var session

    var body: some View {
        NavigationLink(value: TitleRef(item).map(Route.title) ?? .section(.discover)) {
            ZStack(alignment: .bottomLeading) {
                RemoteImage(url: ImageURL.tmdb(session.server.url, item.backdrop, size: "w1280"), title: item.title, maxPixel: 1400)
                    .frame(height: 240)
                LinearGradient(colors: [.clear, Theme.background.opacity(0.95)], startPoint: .center, endPoint: .bottom)
                VStack(alignment: .leading, spacing: 6) {
                    Text(verbatim: L("discover.rows.trending").uppercased()).font(.caption.weight(.bold)).tracking(1.2).foregroundStyle(Theme.accent)
                    Text(verbatim: item.title).font(.title.weight(.bold)).foregroundStyle(Theme.text).lineLimit(2)
                    if let o = item.overview, !o.isEmpty {
                        Text(verbatim: o).font(.footnote).foregroundStyle(Theme.textMuted).lineLimit(2)
                    }
                }
                .padding()
            }
            .frame(height: 240)
            .clipShape(RoundedRectangle(cornerRadius: Theme.radius + 4, style: .continuous))
            .padding(.horizontal)
        }
        .buttonStyle(PressStyle())
        .accessibilityLabel(Text(verbatim: item.title))
    }
}

/// A whole list, page by page, as a grid.
struct DiscoverListView: View {
    let key: String
    let title: String
    let id: Int64?
    @Environment(Session.self) private var session
    @State private var pager: Pager<DiscoverItem>?

    var body: some View {
        ScrollView {
            if let pager { PosterGrid(pager: pager, emptyText: L("discover.emptyRow")) }
        }
        .refreshable { await pager?.reload() }
        .task {
            if pager == nil {
                let client = session.client, key = key, id = id
                let p = Pager<DiscoverItem> { offset in
                    // Pages of 20; the page number follows from what is loaded.
                    let page = try await client.discoverList(key, page: offset / 20 + 1, id: id)
                    return (page.items, nil, (page.page ?? 1) < (page.totalPages ?? 1))
                }
                pager = p
                await p.reload()
            }
        }
        .navigationTitle(Text(verbatim: title))
        .screenStyle()
    }
}

/// Posters in an adaptive grid; loads more at the end.
struct PosterGrid: View {
    let pager: Pager<DiscoverItem>
    let emptyText: String

    var body: some View {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: 110, maximum: 160), spacing: 12, alignment: .top)], spacing: 18) {
            ForEach(pager.items) { item in
                PosterCard(item: item, width: nil)
                    .task { await pager.loadMore(after: item) }
            }
        }
        .padding()
        PagerFooter(pager: pager, emptyText: emptyText)
    }
}

/// Search movies, series, people and music.
struct DiscoverSearchView: View {
    @Environment(Session.self) private var session
    @State private var text = ""
    @State private var type = "all"
    @State private var results = Loader<DiscoverPage>()

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                SegmentPicker(options: ["all", "movie", "series", "person", "music"], selection: $type,
                              label: L("discover.searchTypes")) { L("discover.searchTab.\($0)") }

                if text.trimmingCharacters(in: .whitespaces).count < 2 {
                    EmptyState(text: L("discover.searchHint"), systemImage: "magnifyingglass")
                } else if let error = results.error {
                    ErrorState(message: error) { await search() }
                } else if let page = results.value {
                    if page.items.isEmpty {
                        EmptyState(text: L("discover.noResults", ["q": text]))
                    } else {
                        LazyVGrid(columns: [GridItem(.adaptive(minimum: 110, maximum: 160), spacing: 12, alignment: .top)], spacing: 18) {
                            ForEach(page.items) { PosterCard(item: $0, width: nil) }
                        }
                        .padding()
                    }
                } else {
                    LoadingState()
                }
            }
            .padding(.top, 8)
        }
        .searchable(text: $text, placement: .navigationBarDrawer(displayMode: .always), prompt: Text(verbatim: L("discover.searchPlaceholder")))
        .task(id: "\(text)|\(type)") {
            guard text.trimmingCharacters(in: .whitespaces).count >= 2 else { return }
            try? await Task.sleep(for: .milliseconds(350))
            guard !Task.isCancelled else { return }
            await search()
        }
        .navigationTitle(Text(verbatim: L("discover.searchLabel")))
        .screenStyle()
    }

    private func search() async {
        let client = session.client, q = text.trimmingCharacters(in: .whitespaces), type = type
        results.value = nil
        await results.load { try await client.discoverSearch(q, type: type) }
    }
}

/// A person and what they are known for.
struct PersonView: View {
    let id: Int64
    @Environment(Session.self) private var session
    @State private var person = Loader<PersonDetail>()

    var body: some View {
        ScrollView {
            LoadView(loader: person, reload: load) { p in
                VStack(alignment: .leading, spacing: 18) {
                    HStack(alignment: .top, spacing: 16) {
                        Poster(url: ImageURL.tmdb(session.server.url, p.profile, size: "w342"), title: p.name, width: 120)
                        VStack(alignment: .leading, spacing: 6) {
                            Text(verbatim: p.name).font(.title2.weight(.bold)).foregroundStyle(Theme.text)
                            if let d = p.department, !d.isEmpty { Text(verbatim: d).foregroundStyle(Theme.textMuted) }
                            if let b = p.birthday, !b.isEmpty { Text(verbatim: Fmt.date(b)).font(.footnote).foregroundStyle(Theme.textFaint) }
                        }
                    }
                    .padding(.horizontal)
                    if let bio = p.biography, !bio.isEmpty {
                        ExpandableText(text: bio).padding(.horizontal)
                    }
                    if let known = p.knownFor, !known.isEmpty {
                        PosterRow(title: L("title.knownFor"), items: known)
                    }
                }
                .padding(.vertical)
            }
        }
        .task { if person.value == nil { await load() } }
        .navigationTitle(Text(verbatim: person.value?.name ?? ""))
        .navigationBarTitleDisplayMode(.inline)
        .screenStyle()
    }

    private func load() async {
        let client = session.client, id = id
        await person.load { try await client.person(id) }
    }
}

/// Long text with "More".
struct ExpandableText: View {
    let text: String
    var lines = 4
    @State private var expanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(verbatim: text).font(.callout).foregroundStyle(Theme.textMuted).lineLimit(expanded ? nil : lines)
            if text.count > 220 {
                Button(L(expanded ? "app.less" : "app.more")) { withAnimation { expanded.toggle() } }
                    .font(.footnote.weight(.semibold))
            }
        }
    }
}
