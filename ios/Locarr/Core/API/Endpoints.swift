import Foundation

private func q(_ v: Int?) -> String? { v.map { String($0) } }
private func q(_ v: Int64?) -> String? { v.map { String($0) } }
private func q(_ v: Bool?) -> String? { v.map { $0 ? "true" : "false" } }

// MARK: Auth and system

extension APIClient {
    func systemInfo() async throws -> SystemInfo { try await get("/system/info") }
    func me() async throws -> User { try await get("/auth/me") }
    func providers() async throws -> [AuthProvider] { try await get("/auth/providers") }

    func login(_ body: LoginRequest) async throws -> AuthResult { try await send("POST", "/auth/login", body: body) }

    func loginWithProvider(_ id: String, _ body: ProviderLoginRequest) async throws -> AuthResult {
        try await send("POST", "/auth/providers/\(seg(id))/login", body: body)
    }

    func logout() async throws { try await sendVoid("POST", "/auth/logout") }
    func systemStatus() async throws -> SystemStatus { try await get("/system/status") }
}

// MARK: Notifications

extension APIClient {
    func notifications(since: Int64? = nil, limit: Int? = nil) async throws -> NotificationList {
        try await get("/notifications", query: ["since": q(since), "limit": q(limit)])
    }

    @discardableResult
    func markRead(ids: [Int64]? = nil, all: Bool? = nil) async throws -> UnreadCount {
        try await send("POST", "/notifications/read", body: MarkReadRequest(ids: ids, all: all))
    }
}

// MARK: Discover, requests, issues

extension APIClient {
    func discover() async throws -> DiscoverHome { try await get("/discover") }

    func discoverList(_ key: String, page: Int, id: Int64? = nil) async throws -> DiscoverPage {
        try await get("/discover/lists/\(seg(key))", query: ["page": String(page), "id": q(id)])
    }

    func discoverSearch(_ text: String, type: String) async throws -> DiscoverPage {
        try await get("/discover/search", query: ["q": text, "type": type])
    }

    func title(_ ref: TitleRef) async throws -> TitleDetail {
        switch ref {
        case .movie(let id): return try await get("/discover/movies/\(id)")
        case .series(let id): return try await get("/discover/series/\(id)")
        case .artist(let mbid): return try await get("/discover/artists/\(seg(mbid))")
        }
    }

    func person(_ id: Int64) async throws -> PersonDetail { try await get("/discover/people/\(id)") }

    func requests(status: String? = nil, kind: String? = nil, mine: Bool? = nil, offset: Int = 0, limit: Int = 30) async throws -> RequestList {
        try await get("/requests", query: ["status": status, "kind": kind, "mine": q(mine), "offset": String(offset), "limit": String(limit)])
    }

    func request(_ id: Int64) async throws -> MediaRequest { try await get("/requests/\(id)") }
    func requestSummary() async throws -> RequestSummary { try await get("/requests/summary") }

    func createRequest(_ body: CreateRequest) async throws -> MediaRequest { try await send("POST", "/requests", body: body) }
    func approve(_ id: Int64) async throws -> MediaRequest { try await send("POST", "/requests/\(id)/approve", body: EmptyBody()) }
    func approve(ids: [Int64]) async throws -> BulkResult { try await send("POST", "/requests/approve", body: RequestIds(ids: ids)) }

    func decline(_ id: Int64, reason: String?) async throws -> MediaRequest {
        try await send("POST", "/requests/\(id)/decline", body: DeclineRequest(reason: reason))
    }

    func cancelRequest(_ id: Int64) async throws -> MediaRequest { try await send("POST", "/requests/\(id)/cancel") }
    func retryRequest(_ id: Int64) async throws -> MediaRequest { try await send("POST", "/requests/\(id)/retry") }
    func requestComments(_ id: Int64) async throws -> [Comment] { try await get("/requests/\(id)/comments") }

    func addRequestComment(_ id: Int64, _ text: String) async throws -> Comment {
        try await send("POST", "/requests/\(id)/comments", body: CommentInput(body: text))
    }

    func issues(status: String?, offset: Int = 0, limit: Int = 30) async throws -> IssueList {
        try await get("/issues", query: ["status": status, "offset": String(offset), "limit": String(limit)])
    }

    func issue(_ id: Int64) async throws -> Issue { try await get("/issues/\(id)") }
    func createIssue(_ body: IssueInput) async throws -> Issue { try await send("POST", "/issues", body: body) }

    func resolveIssue(_ id: Int64, searchReplacement: Bool, comment: String?) async throws -> Issue {
        try await send("POST", "/issues/\(id)/resolve", body: ResolveIssueRequest(searchReplacement: searchReplacement, comment: comment))
    }

    func reopenIssue(_ id: Int64) async throws -> Issue { try await send("POST", "/issues/\(id)/reopen") }
    func issueComments(_ id: Int64) async throws -> [Comment] { try await get("/issues/\(id)/comments") }

    func addIssueComment(_ id: Int64, _ text: String) async throws -> Comment {
        try await send("POST", "/issues/\(id)/comments", body: CommentInput(body: text))
    }
}

// MARK: Library

extension APIClient {
    func movies(q text: String? = nil, status: String? = nil, sort: String = "title", desc: Bool = false, offset: Int, limit: Int = 60) async throws -> MovieList {
        try await get("/movies", query: ["q": text, "status": status, "sort": sort, "desc": q(desc), "offset": String(offset), "limit": String(limit)])
    }

    func movie(_ id: Int64) async throws -> Movie { try await get("/movies/\(id)") }
    func updateMovie(_ id: Int64, _ body: MovieUpdate) async throws -> Movie { try await send("PATCH", "/movies/\(id)", body: body) }
    func searchMovie(_ id: Int64) async throws -> QueuedCount { try await send("POST", "/movies/\(id)/search") }
    func searchMovies(_ ids: [Int64]) async throws -> QueuedCount { try await send("POST", "/movies/search", body: MovieIds(movieIds: ids)) }
    func refreshMovie(_ id: Int64) async throws -> Movie { try await send("POST", "/movies/\(id)/refresh") }
    func lookupMovies(_ term: String) async throws -> [MovieLookup] { try await get("/movies/lookup", query: ["term": term]) }
    func addMovie(_ body: AddMovieRequest) async throws -> Movie { try await send("POST", "/movies", body: body) }

    func series(q text: String? = nil, sort: String = "title", desc: Bool = false, missing: Bool? = nil, offset: Int, limit: Int = 60) async throws -> SeriesList {
        try await get("/series", query: ["q": text, "sort": sort, "desc": q(desc), "missing": q(missing), "offset": String(offset), "limit": String(limit)])
    }

    func seriesDetail(_ id: Int64) async throws -> Series { try await get("/series/\(id)") }
    func episodes(series id: Int64) async throws -> [Episode] { try await get("/series/\(id)/episodes") }
    func updateSeries(_ id: Int64, _ body: SeriesUpdate) async throws -> Series { try await send("PATCH", "/series/\(id)", body: body) }
    func updateSeason(_ id: Int64, number: Int, monitored: Bool) async throws {
        try await sendVoid("PATCH", "/series/\(id)/seasons/\(number)", body: SeasonUpdate(monitored: monitored))
    }
    func searchSeries(_ id: Int64) async throws -> QueuedCount { try await send("POST", "/series/\(id)/search") }
    func searchSeason(_ id: Int64, number: Int) async throws -> QueuedCount { try await send("POST", "/series/\(id)/seasons/\(number)/search") }
    func searchEpisodes(_ ids: [Int64]) async throws -> QueuedCount { try await send("POST", "/episodes/search", body: EpisodeIds(episodeIds: ids)) }
    func monitorEpisodes(_ ids: [Int64], monitored: Bool) async throws -> QueuedCount {
        try await send("POST", "/episodes/monitor", body: MonitorEpisodes(episodeIds: ids, monitored: monitored))
    }
    func refreshSeries(_ id: Int64) async throws -> Series { try await send("POST", "/series/\(id)/refresh") }
    func lookupSeries(_ term: String) async throws -> [SeriesLookup] { try await get("/series/lookup", query: ["term": term]) }
    func addSeries(_ body: AddSeriesRequest) async throws -> Series { try await send("POST", "/series", body: body) }

    func wantedEpisodes(kind: String, offset: Int, limit: Int = 50) async throws -> WantedEpisodeList {
        try await get("/episodes/wanted", query: ["kind": kind, "offset": String(offset), "limit": String(limit)])
    }

    func artists(q text: String? = nil, sort: String = "name", offset: Int, limit: Int = 60) async throws -> ArtistList {
        try await get("/music/artists", query: ["q": text, "sort": sort, "offset": String(offset), "limit": String(limit)])
    }

    func artist(_ id: Int64) async throws -> Artist { try await get("/music/artists/\(id)") }
    func albums(artist id: Int64) async throws -> [Album] { try await get("/music/artists/\(id)/albums") }
    func updateArtist(_ id: Int64, monitored: Bool) async throws -> Artist {
        try await send("PATCH", "/music/artists/\(id)", body: ArtistUpdate(monitored: monitored))
    }
    func searchArtist(_ id: Int64) async throws -> QueuedCount { try await send("POST", "/music/artists/\(id)/search") }
    func refreshArtist(_ id: Int64) async throws -> Artist { try await send("POST", "/music/artists/\(id)/refresh") }
    func album(_ id: Int64) async throws -> AlbumDetail { try await get("/music/albums/\(id)") }
    func updateAlbum(_ id: Int64, monitored: Bool) async throws -> AlbumDetail {
        try await send("PATCH", "/music/albums/\(id)", body: AlbumUpdate(monitored: monitored))
    }
    func searchAlbum(_ id: Int64) async throws -> QueuedCount { try await send("POST", "/music/albums/\(id)/search") }
    func wantedAlbums(status: String, offset: Int, limit: Int = 50) async throws -> AlbumList {
        try await get("/music/albums", query: ["status": status, "offset": String(offset), "limit": String(limit)])
    }
    func lookupArtists(_ term: String) async throws -> [ArtistLookup] { try await get("/music/artists/lookup", query: ["term": term]) }
    func addArtist(_ body: AddArtistRequest) async throws -> Artist { try await send("POST", "/music/artists", body: body) }

    func qualityProfiles() async throws -> [QualityProfile] { try await get("/qualityprofiles") }
    func metadataProfiles() async throws -> [MetadataProfile] { try await get("/music/metadataprofiles") }
    func rootFolders() async throws -> [RootFolder] { try await get("/rootfolders") }
}

// MARK: Releases

extension APIClient {
    func releases(_ target: ReleaseTarget) async throws -> InteractiveSearch { try await get(target.path) }

    func grab(_ target: ReleaseTarget, _ d: InteractiveDecision) async throws -> QueueItem {
        let c = d.decision.candidate
        let body = ManualGrabRequest(indexerId: c.indexerId ?? 0, downloadToken: d.downloadToken, magnetUrl: d.magnetUrl,
                                     title: c.title, protocol: c.protocol ?? "torrent", guid: c.guid, publishDate: c.publishDate)
        return try await send("POST", target.path, body: body)
    }
}

// MARK: Activity and system

extension APIClient {
    func queue() async throws -> Queue { try await get("/queue") }
    func refreshQueue() async throws { try await sendVoid("POST", "/queue/refresh") }
    func pause(_ id: Int64) async throws { try await sendVoid("POST", "/queue/\(id)/pause") }
    func resume(_ id: Int64) async throws { try await sendVoid("POST", "/queue/\(id)/resume") }
    func setPriority(_ id: Int64, _ priority: String) async throws {
        try await sendVoid("POST", "/queue/\(id)/priority", body: PriorityRequest(priority: priority))
    }
    func removeQueueItem(_ id: Int64, deleteData: Bool, blocklist: Bool) async throws {
        try await sendVoid("DELETE", "/queue/\(id)", query: ["deleteData": q(deleteData), "blocklist": q(blocklist)])
    }

    func board(kind: String? = nil) async throws -> BoardSummary { try await get("/board", query: ["kind": kind]) }
    func boardItems(code: String?, kind: String? = nil, text: String? = nil, offset: Int, limit: Int = 50) async throws -> BoardItemPage {
        try await get("/board/items", query: ["code": code, "kind": kind, "q": text, "offset": String(offset), "limit": String(limit)])
    }
    func boardAction(_ action: String, keys: [String]) async throws -> BoardActionResult {
        try await send("POST", "/board/actions", body: BoardActionRequest(action: action, keys: keys, qualityProfileId: nil))
    }
    func refreshBoard() async throws { try await sendVoid("POST", "/board/refresh") }

    func timeline(_ item: String) async throws -> Timeline { try await get("/timeline", query: ["item": item, "limit": "200"]) }
    func explain(_ item: String) async throws -> Explanation { try await get("/timeline/explain", query: ["item": item]) }
    func retry(_ item: String) async throws -> QueuedCount { try await send("POST", "/timeline/retry", query: ["item": item]) }

    func calendar(start: String, end: String) async throws -> [CalendarEntry] { try await get("/calendar", query: ["start": start, "end": end]) }
    func musicCalendar(start: String, end: String) async throws -> [AlbumCalendarEntry] {
        try await get("/music/calendar", query: ["start": start, "end": end])
    }

    func history(category: String?, text: String?, offset: Int, limit: Int = 50) async throws -> HistoryPage {
        try await get("/history", query: ["category": category, "q": text, "offset": String(offset), "limit": String(limit)])
    }

    func health() async throws -> HealthReport { try await get("/system/health") }
    func runHealth() async throws -> HealthReport { try await send("POST", "/system/health/run") }

    func indexers() async throws -> [Indexer] { try await get("/indexers") }
    func indexerCatalog() async throws -> IndexerCatalog { try await get("/indexers/definitions") }
    func testIndexer(_ id: Int64) async throws -> IndexerTestResult { try await send("POST", "/indexers/\(id)/test") }

    /// Clients as returned (the raw form is needed to test a stored client).
    func downloadClients() async throws -> [JSONValue] { try await get("/downloadclients") }

    /// Tests a stored client: its settings are sent back; secrets left out come from the server.
    func testDownloadClient(_ raw: JSONValue) async throws -> DownloadClientTestResult {
        let readOnly: Set<String> = ["id", "password", "apiKey", "status", "createdAt", "updatedAt", "protocol"]
        let input = JSONValue.object(raw.object.filter { !readOnly.contains($0.key) })
        let body = JSONValue.object(["id": raw["id"], "client": input])
        return try await send("POST", "/downloadclients/test", body: body)
    }
}

/// A title on the discover side.
enum TitleRef: Hashable, Sendable {
    case movie(Int64)
    case series(Int64)
    case artist(String)

    init?(_ item: DiscoverItem) {
        switch item.mediaType {
        case .movie: if let id = item.tmdbId { self = .movie(id) } else { return nil }
        case .series: if let id = item.tmdbId { self = .series(id) } else { return nil }
        case .artist: if let id = item.mbid { self = .artist(id) } else { return nil }
        case .album: if let id = item.artistMbid { self = .artist(id) } else { return nil }
        default: return nil
        }
    }
}

/// What an interactive search is for.
enum ReleaseTarget: Hashable, Sendable {
    case movie(Int64)
    case season(series: Int64, number: Int)
    case episode(Int64)
    case album(Int64)

    var path: String {
        switch self {
        case .movie(let id): return "/movies/\(id)/releases"
        case .season(let s, let n): return "/series/\(s)/seasons/\(n)/releases"
        case .episode(let id): return "/episodes/\(id)/releases"
        case .album(let id): return "/music/albums/\(id)/releases"
        }
    }
}
