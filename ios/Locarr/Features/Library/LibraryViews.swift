import SwiftUI

/// Movies, series and music under one tab on iPhone.
struct LibraryView: View {
    @State private var kind: AppSection = .movies

    var body: some View {
        Group {
            switch kind {
            case .series: SeriesListView()
            case .music: ArtistsView()
            default: MoviesView()
            }
        }
        .screenHeader(title: L("nav.library")) {
            SegmentPicker(options: [AppSection.movies, .series, .music], selection: $kind, label: L("nav.library")) { $0.title }
        }
    }
}

/// A library row: poster, title, facts and a status.
struct LibraryRow: View {
    let poster: URL?
    let title: String
    let subtitle: String
    let status: String
    let tone: String
    var square = false
    var progress: Double? = nil
    var monitored = true

    var body: some View {
        HStack(spacing: 12) {
            Poster(url: poster, title: title, square: square, width: 48)
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 6) {
                    if !monitored {
                        Image(systemName: "bookmark.slash").font(.caption).foregroundStyle(Theme.textFaint)
                            .accessibilityLabel(Text(verbatim: L("library.unmonitored")))
                    }
                    Text(verbatim: title).font(.callout.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(1)
                }
                Text(verbatim: subtitle).font(.caption).foregroundStyle(Theme.textMuted).lineLimit(1)
                if let progress { ProgressBar(value: progress, tint: Theme.info).frame(maxWidth: 180) }
            }
            Spacer(minLength: 6)
            Pill(text: status, tone: tone)
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
    }
}

enum LibraryStatus {
    static func movie(_ m: Movie) -> (String, String) {
        let s = m.libraryStatus ?? (m.file != nil ? "downloaded" : "missing")
        let tone: String
        switch s {
        case "downloaded": tone = "success"
        case "downloading": tone = "info"
        case "cutoff_unmet": tone = "warning"
        case "missing": tone = m.available == false ? "neutral" : "danger"
        default: tone = "neutral"
        }
        return (Loc.render("library.status.\(s)", [:], fallback: s), tone)
    }

    static func album(_ a: Album) -> (String, String) {
        let s = a.libraryStatus ?? "missing"
        let tone: String
        switch s {
        case "downloaded": tone = "success"
        case "downloading", "partial": tone = "info"
        case "missing": tone = "danger"
        default: tone = "neutral"
        }
        return (Loc.render("library.status.\(s)", [:], fallback: s), tone)
    }

    static func series(_ s: Series) -> (String, String) {
        let stats = s.stats ?? SeriesStats()
        if (s.downloading ?? 0) > 0 { return (L("library.status.downloading"), "info") }
        if (stats.missingCount ?? 0) > 0 { return (L("library.missingCount", ["count": String(stats.missingCount ?? 0)]), "danger") }
        if (stats.episodeFileCount ?? 0) > 0 { return (L("library.episodesCount", ["have": String(stats.episodeFileCount ?? 0), "total": String(stats.episodeCount ?? 0)]), "success") }
        return (Loc.render("library.seriesStatus.\(s.status ?? "")", [:], fallback: s.status ?? ""), "neutral")
    }
}

/// Sort choices of a library list.
struct SortMenu: View {
    let options: [String]
    @Binding var sort: String
    @Binding var desc: Bool

    var body: some View {
        Menu {
            Picker(selection: $sort) {
                ForEach(options, id: \.self) { Text(verbatim: L("library.sort.\($0)")).tag($0) }
            } label: { Text(verbatim: L("library.sortBy")) }
            Toggle(isOn: $desc) { Text(verbatim: L("library.descending")) }
        } label: {
            Image(systemName: "arrow.up.arrow.down")
        }
        .accessibilityLabel(Text(verbatim: L("library.sortBy")))
    }
}

struct MoviesView: View {
    @Environment(Session.self) private var session
    @State private var search = ""
    @State private var sort = "title"
    @State private var desc = false
    @State private var pager: Pager<Movie>?

    var body: some View {
        List {
            ScreenHeaderRow()
            if let pager {
                ForEach(pager.items) { m in
                    let st = LibraryStatus.movie(m)
                    NavigationLink(value: Route.movie(m.id)) {
                        LibraryRow(poster: ImageURL.tmdb(session.server.url, m.poster, size: "w185"), title: m.title,
                                   subtitle: [m.year.map { String($0) } ?? "", m.file?.quality?.name ?? "", m.file.map { Fmt.bytes($0.sizeBytes) } ?? ""]
                                    .filter { !$0.isEmpty && $0 != "0" }.joined(separator: " · "),
                                   status: st.0, tone: st.1, progress: m.download?.progress, monitored: m.monitored ?? true)
                    }
                    .listRowBackground(Theme.background)
                    .task { await pager.loadMore(after: m) }
                }
                PagerFooter(pager: pager, emptyText: L("library.noMovies")).listRowBackground(Theme.background)
            }
        }
        .listStyle(.plain)
        .searchable(text: $search, prompt: Text(verbatim: L("library.filter")))
        .refreshable { await pager?.reload() }
        .task(id: "\(search)|\(sort)|\(desc)") {
            if pager != nil { try? await Task.sleep(for: .milliseconds(300)) }
            guard !Task.isCancelled else { return }
            let client = session.client, q = search.isEmpty ? nil : search, sort = sort, desc = desc
            let p = Pager<Movie> { offset in
                let list = try await client.movies(q: q, sort: sort, desc: desc, offset: offset)
                return (list.items, list.total, nil)
            }
            pager = p
            await p.reload()
        }
        .screenTitle(L("mediaKinds.movies"))
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) { SortMenu(options: ["title", "year", "added", "rating", "size", "digital"], sort: $sort, desc: $desc) }
            ToolbarItem(placement: .topBarTrailing) {
                NavigationLink(value: Route.add(kind: "movies")) { Image(systemName: "plus") }
                    .accessibilityLabel(Text(verbatim: L("library.addMovie")))
            }
        }
        .screenStyle()
    }
}

struct SeriesListView: View {
    @Environment(Session.self) private var session
    @State private var search = ""
    @State private var sort = "title"
    @State private var desc = false
    @State private var pager: Pager<Series>?

    var body: some View {
        List {
            ScreenHeaderRow()
            if let pager {
                ForEach(pager.items) { s in
                    let st = LibraryStatus.series(s)
                    NavigationLink(value: Route.series(s.id)) {
                        LibraryRow(poster: ImageURL.tmdb(session.server.url, s.poster, size: "w185"), title: s.title,
                                   subtitle: [s.year.map { String($0) } ?? "", s.network ?? "", Fmt.bytes(s.stats?.sizeOnDisk)]
                                    .filter { !$0.isEmpty && $0 != "0" && $0 != "–" }.joined(separator: " · "),
                                   status: st.0, tone: st.1, monitored: s.monitored ?? true)
                    }
                    .listRowBackground(Theme.background)
                    .task { await pager.loadMore(after: s) }
                }
                PagerFooter(pager: pager, emptyText: L("library.noSeries")).listRowBackground(Theme.background)
            }
        }
        .listStyle(.plain)
        .searchable(text: $search, prompt: Text(verbatim: L("library.filter")))
        .refreshable { await pager?.reload() }
        .task(id: "\(search)|\(sort)|\(desc)") {
            if pager != nil { try? await Task.sleep(for: .milliseconds(300)) }
            guard !Task.isCancelled else { return }
            let client = session.client, q = search.isEmpty ? nil : search, sort = sort, desc = desc
            let p = Pager<Series> { offset in
                let list = try await client.series(q: q, sort: sort, desc: desc, offset: offset)
                return (list.items, list.total, nil)
            }
            pager = p
            await p.reload()
        }
        .screenTitle(L("mediaKinds.series"))
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) { SortMenu(options: ["title", "year", "added", "nextAiring", "missing", "size"], sort: $sort, desc: $desc) }
            ToolbarItem(placement: .topBarTrailing) {
                NavigationLink(value: Route.add(kind: "series")) { Image(systemName: "plus") }
                    .accessibilityLabel(Text(verbatim: L("library.addSeries")))
            }
        }
        .screenStyle()
    }
}

struct ArtistsView: View {
    @Environment(Session.self) private var session
    @State private var search = ""
    @State private var sort = "name"
    @State private var desc = false
    @State private var pager: Pager<Artist>?

    var body: some View {
        List {
            ScreenHeaderRow()
            if let pager {
                ForEach(pager.items) { a in
                    NavigationLink(value: Route.artist(a.id)) {
                        LibraryRow(poster: ImageURL.cover(session.server.url, a.cover, size: "250"), title: a.name,
                                   subtitle: L("library.albumsCount", ["have": String(a.albumsWithFiles ?? 0), "total": String(a.monitoredAlbums ?? a.albums ?? 0)]),
                                   status: Fmt.bytes(a.sizeBytes), tone: "neutral", square: true, monitored: a.monitored ?? true)
                    }
                    .listRowBackground(Theme.background)
                    .task { await pager.loadMore(after: a) }
                }
                PagerFooter(pager: pager, emptyText: L("library.noArtists")).listRowBackground(Theme.background)
            }
        }
        .listStyle(.plain)
        .searchable(text: $search, prompt: Text(verbatim: L("library.filter")))
        .refreshable { await pager?.reload() }
        .task(id: "\(search)|\(sort)|\(desc)") {
            if pager != nil { try? await Task.sleep(for: .milliseconds(300)) }
            guard !Task.isCancelled else { return }
            let client = session.client, q = search.isEmpty ? nil : search, sort = sort
            let p = Pager<Artist> { offset in
                let list = try await client.artists(q: q, sort: sort, offset: offset)
                return (list.items, list.total, nil)
            }
            pager = p
            await p.reload()
        }
        .screenTitle(L("mediaKinds.music"))
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) { SortMenu(options: ["name", "added", "albums", "size"], sort: $sort, desc: $desc) }
            ToolbarItem(placement: .topBarTrailing) {
                NavigationLink(value: Route.add(kind: "music")) { Image(systemName: "plus") }
                    .accessibilityLabel(Text(verbatim: L("library.addArtist")))
            }
        }
        .screenStyle()
    }
}
