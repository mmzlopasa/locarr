import SwiftUI

struct MovieDetailView: View {
    let id: Int64
    @Environment(Session.self) private var session
    @State private var movie = Loader<Movie>()
    @State private var toast: String?
    @State private var tick = 0

    var body: some View {
        ScrollView {
            LoadView(loader: movie, reload: load) { m in
                let st = LibraryStatus.movie(m)
                VStack(alignment: .leading, spacing: 16) {
                    LibraryHeader(backdrop: ImageURL.tmdb(session.server.url, m.backdrop, size: "w1280"),
                                  poster: ImageURL.tmdb(session.server.url, m.poster, size: "w500"), title: m.title,
                                  facts: [m.year.map { String($0) } ?? "", m.runtime.map { L("title.minutes", ["n": String($0)]) } ?? "", m.studio ?? ""]
                                    .filter { !$0.isEmpty && $0 != "0" }.joined(separator: " · "),
                                  pills: [st, (L(m.monitored == true ? "library.monitored" : "library.unmonitored"), m.monitored == true ? "info" : "neutral")])
                    VStack(alignment: .leading, spacing: 16) {
                        actionBar(m)
                        if let d = m.download {
                            VStack(alignment: .leading, spacing: 6) {
                                Text(verbatim: d.title ?? "").font(.footnote).foregroundStyle(Theme.text).lineLimit(2)
                                ProgressBar(value: d.progress ?? 0, tint: Theme.info)
                                Text(verbatim: [Fmt.percent(d.progress ?? 0), Describe.downloadState(d.state)].joined(separator: " · "))
                                    .font(.caption).foregroundStyle(Theme.textMuted)
                            }
                            .card()
                        }
                        WhyCard(itemRef: m.ref, title: m.title, reloadTick: tick)
                        if let f = m.file { FileCard(file: f) }
                        if let o = m.overview, !o.isEmpty { ExpandableText(text: o) }
                        VStack(alignment: .leading, spacing: 8) {
                            InfoRow(label: L("library.path"), value: m.path ?? "–", mono: true)
                            if let d = m.inCinemas, !d.isEmpty { InfoRow(label: L("calendar.kinds.in_cinemas"), value: Fmt.date(d)) }
                            if let d = m.digitalRelease, !d.isEmpty { InfoRow(label: L("calendar.kinds.digital"), value: Fmt.date(d)) }
                            if let d = m.physicalRelease, !d.isEmpty { InfoRow(label: L("calendar.kinds.physical"), value: Fmt.date(d)) }
                            if let a = m.minimumAvailability { InfoRow(label: L("library.minimumAvailability"), value: Loc.render("availability.\(a)", [:], fallback: a)) }
                        }
                        .card()
                    }
                    .padding(.horizontal)
                }
                .padding(.bottom, 32)
            }
        }
        .ignoresSafeArea(edges: .top)
        .refreshable { await load(); tick += 1 }
        .task { if movie.value == nil { await load() } }
        .navigationTitle(Text(verbatim: movie.value?.title ?? ""))
        .navigationBarTitleDisplayMode(.inline)
        .toolbarBackground(.hidden, for: .navigationBar)
        .screenStyle()
        .toast($toast)
    }

    private func actionBar(_ m: Movie) -> some View {
        HStack(spacing: 10) {
            Button {
                Task { await runAction($toast, success: L("library.searchStarted")) { _ = try await session.client.searchMovie(id) } }
            } label: { Label { Text(verbatim: L("library.search")) } icon: { Image(systemName: "magnifyingglass") } }
            .buttonStyle(PrimaryButtonStyle())
            NavigationLink(value: Route.releases(.movie(id), title: m.title)) {
                Label { Text(verbatim: L("library.interactive")) } icon: { Image(systemName: "person.crop.rectangle.stack") }
            }
            .buttonStyle(SecondaryButtonStyle())
            Menu {
                Button {
                    Task {
                        await runAction($toast, success: L("common.saved")) {
                            movie.value = try await session.client.updateMovie(id, MovieUpdate(monitored: !(m.monitored ?? false)))
                        }
                    }
                } label: { Text(verbatim: L(m.monitored == true ? "library.unmonitor" : "library.monitor")) }
                Button {
                    Task { await runAction($toast, success: L("library.refreshed")) { movie.value = try await session.client.refreshMovie(id) } }
                } label: { Text(verbatim: L("library.refresh")) }
                if let tmdb = m.tmdbId {
                    NavigationLink(value: Route.title(.movie(tmdb))) { Text(verbatim: L("library.discoverPage")) }
                }
            } label: {
                Image(systemName: "ellipsis").frame(width: 44, height: 44)
                    .background(Theme.surface3, in: RoundedRectangle(cornerRadius: Theme.radius))
            }
            .accessibilityLabel(Text(verbatim: L("library.more")))
        }
    }

    private func load() async {
        let client = session.client, id = id
        await movie.load { try await client.movie(id) }
    }
}

struct SeriesDetailView: View {
    let id: Int64
    @Environment(Session.self) private var session
    @State private var series = Loader<Series>()
    @State private var episodes: [Episode] = []
    @State private var expanded: Set<Int> = []
    @State private var toast: String?
    @State private var tick = 0

    var body: some View {
        ScrollView {
            LoadView(loader: series, reload: load) { s in
                let st = LibraryStatus.series(s)
                VStack(alignment: .leading, spacing: 16) {
                    LibraryHeader(backdrop: ImageURL.tmdb(session.server.url, s.backdrop, size: "w1280"),
                                  poster: ImageURL.tmdb(session.server.url, s.poster, size: "w500"), title: s.title,
                                  facts: [s.year.map { String($0) } ?? "", s.network ?? "", Loc.render("library.seriesStatus.\(s.status ?? "")", [:], fallback: s.status ?? "")]
                                    .filter { !$0.isEmpty && $0 != "0" }.joined(separator: " · "),
                                  pills: [st, (L(s.monitored == true ? "library.monitored" : "library.unmonitored"), s.monitored == true ? "info" : "neutral")])
                    VStack(alignment: .leading, spacing: 16) {
                        HStack(spacing: 10) {
                            Button {
                                Task { await runAction($toast, success: L("library.searchStarted")) { _ = try await session.client.searchSeries(id) } }
                            } label: { Label { Text(verbatim: L("library.searchMissing")) } icon: { Image(systemName: "magnifyingglass") } }
                            .buttonStyle(PrimaryButtonStyle())
                            Menu {
                                Button {
                                    Task {
                                        await runAction($toast, success: L("common.saved")) {
                                            _ = try await session.client.updateSeries(id, SeriesUpdate(monitored: !(s.monitored ?? false)))
                                            await load()
                                        }
                                    }
                                } label: { Text(verbatim: L(s.monitored == true ? "library.unmonitor" : "library.monitor")) }
                                Button {
                                    Task { await runAction($toast, success: L("library.refreshed")) { _ = try await session.client.refreshSeries(id); await load() } }
                                } label: { Text(verbatim: L("library.refresh")) }
                                if let tmdb = s.tmdbId {
                                    NavigationLink(value: Route.title(.series(tmdb))) { Text(verbatim: L("library.discoverPage")) }
                                }
                            } label: {
                                Image(systemName: "ellipsis").frame(width: 44, height: 44)
                                    .background(Theme.surface3, in: RoundedRectangle(cornerRadius: Theme.radius))
                            }
                            .accessibilityLabel(Text(verbatim: L("library.more")))
                        }
                        if let o = s.overview, !o.isEmpty { ExpandableText(text: o) }
                        if let stats = s.stats {
                            VStack(alignment: .leading, spacing: 8) {
                                InfoRow(label: L("library.episodes"), value: "\(stats.episodeFileCount ?? 0) / \(stats.episodeCount ?? 0)")
                                InfoRow(label: L("library.size"), value: Fmt.bytes(stats.sizeOnDisk))
                                if let next = stats.nextAiring { InfoRow(label: L("library.nextAiring"), value: Fmt.dateTime(next)) }
                                InfoRow(label: L("library.path"), value: s.path ?? "–", mono: true)
                            }
                            .card()
                        }
                        ForEach((s.seasons ?? []).sorted { $0.number > $1.number }) { season in
                            seasonBlock(s, season)
                        }
                    }
                    .padding(.horizontal)
                }
                .padding(.bottom, 32)
            }
        }
        .ignoresSafeArea(edges: .top)
        .refreshable { await load() }
        .task { if series.value == nil { await load() } }
        .navigationTitle(Text(verbatim: series.value?.title ?? ""))
        .navigationBarTitleDisplayMode(.inline)
        .toolbarBackground(.hidden, for: .navigationBar)
        .screenStyle()
        .toast($toast)
    }

    private func seasonBlock(_ s: Series, _ season: SeasonInfo) -> some View {
        let eps = episodes.filter { $0.seasonNumber == season.number }.sorted { $0.episodeNumber > $1.episodeNumber }
        let open = expanded.contains(season.number)
        return VStack(alignment: .leading, spacing: 10) {
            Button {
                withAnimation(.snappy) { if open { expanded.remove(season.number) } else { expanded.insert(season.number) } }
            } label: {
                HStack {
                    Image(systemName: season.monitored == true ? "bookmark.fill" : "bookmark.slash").foregroundStyle(season.monitored == true ? Theme.accent : Theme.textFaint)
                    Text(verbatim: season.number == 0 ? L("request.specials") : L("request.season", ["n": String(season.number)]))
                        .font(.headline).foregroundStyle(Theme.text)
                    Spacer()
                    Text(verbatim: "\(season.stats?.episodeFileCount ?? 0)/\(season.stats?.episodeCount ?? 0)")
                        .font(.footnote.monospacedDigit()).foregroundStyle((season.stats?.missingCount ?? 0) > 0 ? Theme.danger : Theme.textMuted)
                    Image(systemName: "chevron.down").rotationEffect(.degrees(open ? 180 : 0)).foregroundStyle(Theme.textFaint)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityHint(Text(verbatim: L(open ? "library.collapse" : "library.expand")))
            if open {
                HStack(spacing: 8) {
                    Button {
                        Task { await runAction($toast, success: L("library.searchStarted")) { _ = try await session.client.searchSeason(id, number: season.number) } }
                    } label: { Label { Text(verbatim: L("library.search")) } icon: { Image(systemName: "magnifyingglass") } }
                    .buttonStyle(SecondaryButtonStyle())
                    NavigationLink(value: Route.releases(.season(series: id, number: season.number), title: "\(s.title) · " + L("request.season", ["n": String(season.number)]))) {
                        Label { Text(verbatim: L("library.interactive")) } icon: { Image(systemName: "person.crop.rectangle.stack") }
                    }
                    .buttonStyle(SecondaryButtonStyle())
                    Button {
                        Task {
                            await runAction($toast, success: L("common.saved")) {
                                try await session.client.updateSeason(id, number: season.number, monitored: !(season.monitored ?? false))
                                await load()
                            }
                        }
                    } label: { Image(systemName: season.monitored == true ? "bookmark.slash" : "bookmark") }
                    .buttonStyle(SecondaryButtonStyle())
                    .accessibilityLabel(Text(verbatim: L(season.monitored == true ? "library.unmonitor" : "library.monitor")))
                }
                ForEach(eps) { e in episodeRow(s, e) }
            }
        }
        .card(padding: 12)
    }

    private func episodeRow(_ s: Series, _ e: Episode) -> some View {
        let tone: String = e.file != nil ? (e.cutoffMet == false ? "warning" : "success") : e.download != nil ? "info" : e.aired == true && e.monitored == true ? "danger" : "neutral"
        let status: String = e.file?.quality?.name ?? (e.download != nil ? L("library.status.downloading") : e.aired == true ? L("library.status.missing") : Fmt.date(e.airDate))
        return NavigationLink(value: Route.timeline(ref: e.ref, title: "\(s.title) \(e.code)")) {
            HStack(spacing: 10) {
                Text(verbatim: String(e.episodeNumber)).font(.footnote.monospacedDigit()).foregroundStyle(Theme.textMuted).frame(width: 26, alignment: .trailing)
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: e.title ?? "").font(.callout).foregroundStyle(e.monitored == false ? Theme.textMuted : Theme.text).lineLimit(1)
                    Text(verbatim: Fmt.date(e.airDate)).font(.caption2).foregroundStyle(Theme.textFaint)
                }
                Spacer()
                Pill(text: status, tone: tone)
            }
        }
        .buttonStyle(.plain)
        .contextMenu {
            Button {
                Task { await runAction($toast, success: L("library.searchStarted")) { _ = try await session.client.searchEpisodes([e.id]) } }
            } label: { Label { Text(verbatim: L("library.search")) } icon: { Image(systemName: "magnifyingglass") } }
            NavigationLink(value: Route.releases(.episode(e.id), title: "\(s.title) \(e.code)")) {
                Label { Text(verbatim: L("library.interactive")) } icon: { Image(systemName: "person.crop.rectangle.stack") }
            }
            Button {
                Task {
                    await runAction($toast, success: L("common.saved")) {
                        _ = try await session.client.monitorEpisodes([e.id], monitored: !(e.monitored ?? false))
                        await load()
                    }
                }
            } label: { Text(verbatim: L(e.monitored == true ? "library.unmonitor" : "library.monitor")) }
        }
        .accessibilityHint(Text(verbatim: L("library.episodeHint")))
    }

    private func load() async {
        let client = session.client, id = id
        await series.load { try await client.seriesDetail(id) }
        if let eps = try? await client.episodes(series: id) { episodes = eps }
        if expanded.isEmpty, let latest = series.value?.seasons?.map(\.number).max() { expanded = [latest] }
    }
}

struct ArtistDetailView: View {
    let id: Int64
    @Environment(Session.self) private var session
    @State private var artist = Loader<Artist>()
    @State private var albums: [Album] = []
    @State private var toast: String?

    var body: some View {
        ScrollView {
            LoadView(loader: artist, reload: load) { a in
                VStack(alignment: .leading, spacing: 16) {
                    LibraryHeader(backdrop: nil, poster: ImageURL.cover(session.server.url, a.cover), title: a.name,
                                  facts: [a.type ?? "", a.country ?? "", a.disambiguation ?? ""].filter { !$0.isEmpty }.joined(separator: " · "),
                                  square: true, pills: [(L(a.monitored == true ? "library.monitored" : "library.unmonitored"), a.monitored == true ? "info" : "neutral")])
                    VStack(alignment: .leading, spacing: 14) {
                        HStack(spacing: 10) {
                            Button {
                                Task { await runAction($toast, success: L("library.searchStarted")) { _ = try await session.client.searchArtist(id) } }
                            } label: { Label { Text(verbatim: L("library.searchMissing")) } icon: { Image(systemName: "magnifyingglass") } }
                            .buttonStyle(PrimaryButtonStyle())
                            Button {
                                Task {
                                    await runAction($toast, success: L("common.saved")) {
                                        artist.value = try await session.client.updateArtist(id, monitored: !(a.monitored ?? false))
                                    }
                                }
                            } label: { Image(systemName: a.monitored == true ? "bookmark.slash" : "bookmark").frame(width: 24) }
                            .buttonStyle(SecondaryButtonStyle())
                            .accessibilityLabel(Text(verbatim: L(a.monitored == true ? "library.unmonitor" : "library.monitor")))
                        }
                        InfoRow(label: L("library.size"), value: Fmt.bytes(a.sizeBytes))
                        InfoRow(label: L("library.path"), value: a.path ?? "–", mono: true)
                        SectionTitle(text: L("title.albums"))
                        ForEach(albums.sorted { ($0.releaseDate ?? "") > ($1.releaseDate ?? "") }) { album in
                            let st = LibraryStatus.album(album)
                            NavigationLink(value: Route.album(album.id)) {
                                LibraryRow(poster: ImageURL.cover(session.server.url, album.cover, size: "250"), title: album.title,
                                           subtitle: [String((album.releaseDate ?? "").prefix(4)), Loc.render("music.types.\(album.primaryType ?? "")", [:], fallback: album.primaryType ?? ""),
                                                      "\(album.fileCount ?? 0)/\(album.trackCount ?? 0)"].filter { !$0.isEmpty }.joined(separator: " · "),
                                           status: st.0, tone: st.1, square: true, progress: album.download?.progress, monitored: album.monitored ?? true)
                            }
                            .buttonStyle(.plain)
                        }
                    }
                    .padding(.horizontal)
                }
                .padding(.bottom, 32)
            }
        }
        .ignoresSafeArea(edges: .top)
        .refreshable { await load() }
        .task { if artist.value == nil { await load() } }
        .navigationTitle(Text(verbatim: artist.value?.name ?? ""))
        .navigationBarTitleDisplayMode(.inline)
        .screenStyle()
        .toast($toast)
    }

    private func load() async {
        let client = session.client, id = id
        await artist.load { try await client.artist(id) }
        if let list = try? await client.albums(artist: id) { albums = list }
    }
}

struct AlbumDetailView: View {
    let id: Int64
    @Environment(Session.self) private var session
    @State private var detail = Loader<AlbumDetail>()
    @State private var toast: String?

    var body: some View {
        ScrollView {
            LoadView(loader: detail, reload: load) { d in
                let a = d.album
                let st = LibraryStatus.album(a)
                VStack(alignment: .leading, spacing: 16) {
                    LibraryHeader(backdrop: nil, poster: ImageURL.cover(session.server.url, a.cover), title: a.title,
                                  facts: [a.artistName ?? "", String((a.releaseDate ?? "").prefix(4)), a.quality?.name ?? ""].filter { !$0.isEmpty }.joined(separator: " · "),
                                  square: true, pills: [st])
                    VStack(alignment: .leading, spacing: 14) {
                        HStack(spacing: 10) {
                            Button {
                                Task { await runAction($toast, success: L("library.searchStarted")) { _ = try await session.client.searchAlbum(id) } }
                            } label: { Label { Text(verbatim: L("library.search")) } icon: { Image(systemName: "magnifyingglass") } }
                            .buttonStyle(PrimaryButtonStyle())
                            NavigationLink(value: Route.releases(.album(id), title: a.title)) {
                                Label { Text(verbatim: L("library.interactive")) } icon: { Image(systemName: "person.crop.rectangle.stack") }
                            }
                            .buttonStyle(SecondaryButtonStyle())
                            Button {
                                Task {
                                    await runAction($toast, success: L("common.saved")) {
                                        detail.value = try await session.client.updateAlbum(id, monitored: !(a.monitored ?? false))
                                    }
                                }
                            } label: { Image(systemName: a.monitored == true ? "bookmark.slash" : "bookmark").frame(width: 24) }
                            .buttonStyle(SecondaryButtonStyle())
                            .accessibilityLabel(Text(verbatim: L(a.monitored == true ? "library.unmonitor" : "library.monitor")))
                        }
                        WhyCard(itemRef: a.ref, title: a.title)
                        if let err = d.tracksError, !err.isEmpty { Callout(text: err, tone: "warning") }
                        VStack(alignment: .leading, spacing: 0) {
                            ForEach(d.tracks ?? []) { t in
                                HStack(spacing: 10) {
                                    Text(verbatim: t.number ?? String(t.position ?? 0)).font(.footnote.monospacedDigit()).foregroundStyle(Theme.textMuted).frame(width: 30, alignment: .trailing)
                                    Text(verbatim: t.title).font(.callout).foregroundStyle(Theme.text).lineLimit(1)
                                    Spacer()
                                    if let f = t.file {
                                        Text(verbatim: f.quality?.name ?? f.format ?? "").font(.caption2).foregroundStyle(Theme.success)
                                    } else {
                                        Image(systemName: "minus.circle").font(.caption).foregroundStyle(Theme.textFaint)
                                            .accessibilityLabel(Text(verbatim: L("library.status.missing")))
                                    }
                                    Text(verbatim: duration(t.lengthMs)).font(.caption.monospacedDigit()).foregroundStyle(Theme.textFaint)
                                }
                                .padding(.vertical, 8)
                                .accessibilityElement(children: .combine)
                                Divider().overlay(Theme.border)
                            }
                        }
                        .card(padding: 12)
                        if let artistId = a.artistId {
                            NavigationLink(value: Route.artist(artistId)) { Text(verbatim: a.artistName ?? "") }.buttonStyle(SecondaryButtonStyle())
                        }
                    }
                    .padding(.horizontal)
                }
                .padding(.bottom, 32)
            }
        }
        .ignoresSafeArea(edges: .top)
        .refreshable { await load() }
        .task { if detail.value == nil { await load() } }
        .navigationTitle(Text(verbatim: detail.value?.album.title ?? ""))
        .navigationBarTitleDisplayMode(.inline)
        .screenStyle()
        .toast($toast)
    }

    private func duration(_ ms: Int?) -> String {
        guard let ms, ms > 0 else { return "" }
        let s = ms / 1000
        return String(format: "%d:%02d", s / 60, s % 60)
    }

    private func load() async {
        let client = session.client, id = id
        await detail.load { try await client.album(id) }
    }
}
