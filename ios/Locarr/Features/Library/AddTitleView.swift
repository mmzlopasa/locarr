import SwiftUI

/// Add a movie, a series or an artist: look it up, choose profile and folder, add (and search).
struct AddTitleView: View {
    /// "movies", "series" or "music".
    let kind: String
    @Environment(Session.self) private var session
    @State private var term = ""
    @State private var movies: [MovieLookup] = []
    @State private var series: [SeriesLookup] = []
    @State private var artists: [ArtistLookup] = []
    @State private var searching = false
    @State private var error: String?
    @State private var adding: AddCandidate?
    @State private var added: Route?

    var body: some View {
        List {
            if let error { Callout(text: error, tone: "danger").listRowBackground(Color.clear) }
            if term.trimmingCharacters(in: .whitespaces).count < 2 {
                EmptyState(text: L("add.hint"), systemImage: "magnifyingglass").listRowBackground(Color.clear)
            } else if searching && movies.isEmpty && series.isEmpty && artists.isEmpty {
                LoadingState().listRowBackground(Color.clear)
            }
            switch kind {
            case "series":
                ForEach(series) { s in
                    candidateRow(title: s.title, sub: [s.year.map { String($0) } ?? "", s.network ?? ""].filter { !$0.isEmpty && $0 != "0" }.joined(separator: " · "),
                                 overview: s.overview, poster: ImageURL.tmdb(session.server.url, s.poster, size: "w185"),
                                 existing: s.seriesId.map { Route.series($0) }) {
                        adding = AddCandidate(kind: kind, title: s.title, tmdbId: s.tmdbId, tvdbId: s.tvdbId, mbid: nil)
                    }
                }
            case "music":
                ForEach(artists) { a in
                    candidateRow(title: a.name, sub: [a.type ?? "", a.country ?? "", a.disambiguation ?? ""].filter { !$0.isEmpty }.joined(separator: " · "),
                                 overview: nil, poster: nil, square: true, existing: a.artistId.map { Route.artist($0) }) {
                        adding = AddCandidate(kind: kind, title: a.name, tmdbId: nil, tvdbId: nil, mbid: a.mbid)
                    }
                }
            default:
                ForEach(movies) { m in
                    candidateRow(title: m.title, sub: m.year.map { String($0) } ?? "", overview: m.overview,
                                 poster: ImageURL.tmdb(session.server.url, m.poster, size: "w185"), existing: m.movieId.map { Route.movie($0) }) {
                        adding = AddCandidate(kind: kind, title: m.title, tmdbId: m.tmdbId, tvdbId: nil, mbid: nil)
                    }
                }
            }
        }
        .listStyle(.plain)
        .searchable(text: $term, placement: .navigationBarDrawer(displayMode: .always), prompt: Text(verbatim: L("add.prompt")))
        .task(id: term) {
            let t = term.trimmingCharacters(in: .whitespaces)
            guard t.count >= 2 else { return }
            try? await Task.sleep(for: .milliseconds(450))
            guard !Task.isCancelled else { return }
            await lookup(t)
        }
        .navigationTitle(Text(verbatim: L(kind == "series" ? "library.addSeries" : kind == "music" ? "library.addArtist" : "library.addMovie")))
        .navigationBarTitleDisplayMode(.inline)
        .screenStyle()
        .sheet(item: $adding) { c in
            AddOptionsSheet(candidate: c) { route in added = route }.environment(session)
        }
        .navigationDestination(item: $added) { RouteView(route: $0) }
    }

    private func candidateRow(title: String, sub: String, overview: String?, poster: URL?, square: Bool = false,
                              existing: Route?, add: @escaping () -> Void) -> some View {
        HStack(alignment: .top, spacing: 12) {
            Poster(url: poster, title: title, square: square, width: 54)
            VStack(alignment: .leading, spacing: 4) {
                Text(verbatim: title).font(.callout.weight(.semibold)).foregroundStyle(Theme.text)
                if !sub.isEmpty { Text(verbatim: sub).font(.caption).foregroundStyle(Theme.textMuted) }
                if let overview, !overview.isEmpty { Text(verbatim: overview).font(.caption).foregroundStyle(Theme.textFaint).lineLimit(3) }
            }
            Spacer()
            if let existing {
                NavigationLink(value: existing) { Text(verbatim: L("add.inLibrary")).font(.footnote) }.fixedSize()
            } else {
                Button(action: add) { Image(systemName: "plus.circle.fill").font(.title2) }
                    .buttonStyle(.borderless)
                    .accessibilityLabel(Text(verbatim: L("add.add", ["title": title])))
            }
        }
        .listRowBackground(Theme.background)
    }

    private func lookup(_ t: String) async {
        searching = true
        defer { searching = false }
        do {
            switch kind {
            case "series": series = try await session.client.lookupSeries(t)
            case "music": artists = try await session.client.lookupArtists(t)
            default: movies = try await session.client.lookupMovies(t)
            }
            error = nil
        } catch {
            self.error = error.userMessage
        }
    }
}

struct AddCandidate: Identifiable {
    var id = UUID()
    var kind: String
    var title: String
    var tmdbId: Int64?
    var tvdbId: Int64?
    var mbid: String?
}

/// Profile, folder and monitoring for a new title.
struct AddOptionsSheet: View {
    let candidate: AddCandidate
    var onAdded: (Route) -> Void
    @Environment(Session.self) private var session
    @Environment(\.dismiss) private var dismiss
    @State private var profiles: [QualityProfile] = []
    @State private var metadataProfiles: [MetadataProfile] = []
    @State private var folders: [RootFolder] = []
    @State private var profileId: Int64?
    @State private var metadataId: Int64?
    @State private var folderId: Int64?
    @State private var monitor = "all"
    @State private var availability = "released"
    @State private var searchNow = true
    @State private var busy = false
    @State private var error: String?

    private var mediaKind: String { candidate.kind }

    var body: some View {
        NavigationStack {
            Form {
                Section { Text(verbatim: candidate.title).font(.headline) }
                Section {
                    Picker(selection: $profileId) {
                        ForEach(profiles.filter { $0.kind == mediaKind }) { Text(verbatim: $0.name).tag(Optional($0.id)) }
                    } label: { Text(verbatim: L("request.profile")) }
                    if mediaKind == "music" {
                        Picker(selection: $metadataId) {
                            ForEach(metadataProfiles) { Text(verbatim: $0.name).tag(Optional($0.id)) }
                        } label: { Text(verbatim: L("add.metadataProfile")) }
                    }
                    Picker(selection: $folderId) {
                        ForEach(folders.filter { $0.kind == mediaKind }) { Text(verbatim: $0.path).tag(Optional($0.id)) }
                    } label: { Text(verbatim: L("request.rootFolder")) }
                    if mediaKind == "movies" {
                        Picker(selection: $availability) {
                            ForEach(["announced", "in_cinemas", "released"], id: \.self) { Text(verbatim: Loc.render("availability.\($0)", [:], fallback: $0)).tag($0) }
                        } label: { Text(verbatim: L("library.minimumAvailability")) }
                    } else {
                        Picker(selection: $monitor) {
                            ForEach(mediaKind == "series" ? ["all", "future", "missing", "existing", "first_season", "latest_season", "none"] : ["all", "future", "latest", "none"], id: \.self) {
                                Text(verbatim: Loc.render("add.monitor.\($0)", [:], fallback: $0)).tag($0)
                            }
                        } label: { Text(verbatim: L("add.monitorLabel")) }
                    }
                    Toggle(isOn: $searchNow) { Text(verbatim: L("add.searchNow")) }
                }
                if folders.filter({ $0.kind == mediaKind }).isEmpty && !folders.isEmpty {
                    Section { Callout(text: L("add.noFolder"), tone: "warning") }
                }
                if let error { Section { Callout(text: error, tone: "danger") } }
            }
            .scrollContentBackground(.hidden)
            .background(Theme.background)
            .navigationTitle(Text(verbatim: L("common.add")))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(L("common.cancel")) { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    if busy { ProgressView() } else {
                        Button(L("common.add")) { Task { await add() } }
                            .disabled(profileId == nil || folderId == nil || (mediaKind == "music" && metadataId == nil))
                    }
                }
            }
            .task { await loadOptions() }
        }
    }

    private func loadOptions() async {
        let client = session.client
        do {
            profiles = try await client.qualityProfiles()
            folders = try await client.rootFolders()
            if mediaKind == "music" { metadataProfiles = try await client.metadataProfiles() }
            profileId = profiles.first { $0.kind == mediaKind }?.id
            folderId = folders.first { $0.kind == mediaKind }?.id
            metadataId = metadataProfiles.first?.id
        } catch {
            self.error = error.userMessage
        }
    }

    private func add() async {
        guard let profileId, let folderId else { return }
        busy = true
        defer { busy = false }
        let client = session.client
        do {
            let route: Route
            switch mediaKind {
            case "series":
                let s = try await client.addSeries(AddSeriesRequest(tmdbId: candidate.tmdbId == 0 ? nil : candidate.tmdbId, tvdbId: candidate.tvdbId == 0 ? nil : candidate.tvdbId,
                                                                    rootFolderId: folderId, qualityProfileId: profileId, monitor: monitor,
                                                                    monitored: monitor != "none", seasonFolder: true, searchOnAdd: searchNow))
                route = .series(s.id)
            case "music":
                let a = try await client.addArtist(AddArtistRequest(mbid: candidate.mbid ?? "", rootFolderId: folderId, qualityProfileId: profileId,
                                                                    metadataProfileId: metadataId ?? 0, monitored: monitor != "none", monitor: monitor, searchOnAdd: searchNow))
                route = .artist(a.id)
            default:
                let m = try await client.addMovie(AddMovieRequest(tmdbId: candidate.tmdbId ?? 0, rootFolderId: folderId, qualityProfileId: profileId,
                                                                  minimumAvailability: availability, monitored: true, searchOnAdd: searchNow))
                route = .movie(m.id)
            }
            dismiss()
            onAdded(route)
        } catch {
            self.error = error.userMessage
        }
    }
}
