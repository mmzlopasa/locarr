import Foundation

// Models for the parts of the Locarr API the app uses (api/openapi.yaml is the source of
// truth). Decoding is tolerant: unknown fields are ignored, enum-like values keep unknown
// strings (OpenEnum), dates stay strings, and fields that are not needed to identify an
// item are optional so an older or newer server never breaks a screen.

/// A string enum that accepts values this build does not know.
protocol OpenEnum: RawRepresentable, Codable, Hashable, Sendable, ExpressibleByStringLiteral where RawValue == String {
    init(rawValue: String)
}

extension OpenEnum {
    init(stringLiteral value: String) { self.init(rawValue: value) }
}

struct ErrorBody: Codable, Sendable {
    var code: String
    var message: String?
}

// MARK: System and auth

struct SystemInfo: Codable, Sendable {
    var product: String
    var instanceName: String?
    var version: String?
    var setupRequired: Bool?
}

struct Role: OpenEnum {
    let rawValue: String
    init(rawValue: String) { self.rawValue = rawValue }
    static let admin: Role = "admin"
    static let manager: Role = "manager"
    static let requester: Role = "requester"
}

struct Permissions: Codable, Sendable, Hashable {
    var requestMovies: Bool?
    var requestSeries: Bool?
    var requestMusic: Bool?
    var autoApprove: Bool?
    var request4k: Bool?
    var manageLibrary: Bool?
    var manageRequests: Bool?
}

struct User: Codable, Sendable, Hashable {
    var id: Int64
    var username: String
    var role: Role
    var permissions: Permissions?
    var language: String?
    var totpEnabled: Bool?

    var isAdmin: Bool { role == .admin }
    /// Library screens (movies, series, music, queue, board).
    var canManageLibrary: Bool { isAdmin || (role == .manager && permissions?.manageLibrary == true) }
    /// Approving and declining requests, resolving issues.
    var canManageRequests: Bool { isAdmin || (role == .manager && permissions?.manageRequests == true) }
}

struct AuthResult: Codable, Sendable {
    var user: User
    var token: String?
}

struct DeviceInfo: Codable, Sendable {
    var name: String
    var client: String
}

struct LoginRequest: Codable, Sendable {
    var username: String
    var password: String
    var totpCode: String?
    var recoveryCode: String?
    var device: DeviceInfo?
}

struct ProviderLoginRequest: Codable, Sendable {
    var username: String
    var password: String
    var totpCode: String?
    var device: DeviceInfo?
}

struct AuthProvider: Codable, Sendable, Hashable, Identifiable {
    var id: String
    var kind: String
    var name: String
}

// MARK: Notifications

struct AppNotification: Codable, Sendable, Hashable, Identifiable {
    var id: Int64
    var type: String
    var title: String
    var message: String?
    var poster: String?
    var mediaKind: String?
    var actor: UserRef?
    var requestId: Int64?
    var issueId: Int64?
    var createdAt: String?
    var read: Bool?
}

struct NotificationList: Codable, Sendable {
    var items: [AppNotification]
    var unread: Int?
    var latestId: Int64?
}

struct MarkReadRequest: Codable, Sendable {
    var ids: [Int64]?
    var all: Bool?
}

struct UnreadCount: Codable, Sendable { var unread: Int }

struct UserRef: Codable, Sendable, Hashable {
    var id: Int64
    var username: String
}

// MARK: Discover

struct DiscoverStatus: OpenEnum {
    let rawValue: String
    init(rawValue: String) { self.rawValue = rawValue }
    static let none: DiscoverStatus = "none"
    static let pending: DiscoverStatus = "pending"
    static let processing: DiscoverStatus = "processing"
    static let partial: DiscoverStatus = "partial"
    static let available: DiscoverStatus = "available"
    static let inLibrary: DiscoverStatus = "in_library"
}

struct MediaType: OpenEnum {
    let rawValue: String
    init(rawValue: String) { self.rawValue = rawValue }
    static let movie: MediaType = "movie"
    static let series: MediaType = "series"
    static let person: MediaType = "person"
    static let artist: MediaType = "artist"
    static let album: MediaType = "album"

    var isMusic: Bool { self == .artist || self == .album }
}

struct DiscoverItem: Codable, Sendable, Hashable {
    var mediaType: MediaType
    var tmdbId: Int64?
    var mbid: String?
    var artistMbid: String?
    var title: String
    var subtitle: String?
    var year: Int?
    var date: String?
    var overview: String?
    var poster: String?
    var backdrop: String?
    var rating: Double?
    var status: DiscoverStatus?
    var status4k: DiscoverStatus?
    var requestId: Int64?
}

extension DiscoverItem: Identifiable {
    var id: String { "\(mediaType.rawValue):\(tmdbId.map { String($0) } ?? mbid ?? title)" }
}

struct DiscoverRow: Codable, Sendable, Hashable {
    var key: String
    var items: [DiscoverItem]
}

struct DiscoverHome: Codable, Sendable {
    var rows: [DiscoverRow]
    var requests: [MediaRequest]?
    var metadataReady: Bool?
    var musicReady: Bool?
}

struct DiscoverPage: Codable, Sendable {
    var items: [DiscoverItem]
    var page: Int?
    var totalPages: Int?
}

struct CastMember: Codable, Sendable, Hashable {
    var personId: Int64?
    var name: String
    var character: String?
    var profile: String?
}

struct SeasonAvailability: Codable, Sendable, Hashable {
    var number: Int
    var title: String?
    var episodeCount: Int?
    var airDate: String?
    var poster: String?
    var status: DiscoverStatus?
    var status4k: DiscoverStatus?
    var episodesAvailable: Int?
}

struct RequestAbility: Codable, Sendable, Hashable {
    var canRequest: Bool?
    var canRequest4k: Bool?
    var autoApprove: Bool?
    var autoApprove4k: Bool?
    var canReportIssue: Bool?
}

struct TitleDetail: Codable, Sendable {
    var item: DiscoverItem
    var originalTitle: String?
    var tagline: String?
    var runtime: Int?
    var genres: [String]?
    var certification: String?
    var releaseDate: String?
    var studio: String?
    var network: String?
    var seriesStatus: String?
    var cast: [CastMember]?
    var seasons: [SeasonAvailability]?
    var albums: [DiscoverItem]?
    var requests: [MediaRequest]?
    var ability: RequestAbility?
    var libraryRef: String?
}

struct PersonDetail: Codable, Sendable {
    var tmdbId: Int64
    var name: String
    var biography: String?
    var profile: String?
    var birthday: String?
    var department: String?
    var knownFor: [DiscoverItem]?
}

// MARK: Requests and issues

struct RequestStatus: OpenEnum {
    let rawValue: String
    init(rawValue: String) { self.rawValue = rawValue }
    static let pending: RequestStatus = "pending"
    static let approved: RequestStatus = "approved"
    static let declined: RequestStatus = "declined"
    static let cancelled: RequestStatus = "cancelled"
    static let processing: RequestStatus = "processing"
    static let partial: RequestStatus = "partial"
    static let available: RequestStatus = "available"
    static let failed: RequestStatus = "failed"
}

struct RequestSeason: Codable, Sendable, Hashable {
    var number: Int
    var status: RequestStatus?
}

struct Explanation: Codable, Sendable, Hashable {
    var code: String
    var tone: String?
    var message: String?
    var params: JSONValue?
    var reasons: [ReasonCount]?
}

struct RequestProgress: Codable, Sendable, Hashable {
    var percent: Double?
    var downloads: Int?
    var state: String?
    var etaSeconds: Int64?
}

struct MediaRequest: Codable, Sendable, Hashable, Identifiable {
    var id: Int64
    var kind: String
    var is4k: Bool?
    var status: RequestStatus
    var tmdbId: Int64?
    var mbid: String?
    var artistMbid: String?
    var title: String
    var subtitle: String?
    var year: Int?
    var poster: String?
    var seasons: [RequestSeason]?
    var allSeasons: Bool?
    var futureSeasons: Bool?
    var requestedBy: UserRef?
    var decidedBy: UserRef?
    var autoApproved: Bool?
    var declineReason: String?
    var createdAt: String?
    var updatedAt: String?
    var availableAt: String?
    var explanation: Explanation?
    var progress: RequestProgress?
    var libraryRef: String?
    var commentCount: Int?
    var canCancel: Bool?

    var isMusic: Bool { kind == "album" || kind == "artist" }
}

struct RequestList: Codable, Sendable {
    var items: [MediaRequest]
    var total: Int?
}

struct CreateRequest: Codable, Sendable {
    var kind: String
    var tmdbId: Int64?
    var mbid: String?
    var is4k: Bool?
    var seasons: [Int]?
    var allSeasons: Bool?
    var futureSeasons: Bool?
    var comment: String?
}

struct QuotaStatus: Codable, Sendable, Hashable {
    var limit: Int?
    var days: Int?
    var used: Int?
    var remaining: Int?
    var unlimited: Bool?
    var nextSlotAt: String?
}

struct RequestSummary: Codable, Sendable {
    struct Quotas: Codable, Sendable { var movie: QuotaStatus?; var series: QuotaStatus?; var music: QuotaStatus? }
    var pending: Int?
    var processing: Int?
    var failed: Int?
    var openIssues: Int?
    var quotas: Quotas?
}

struct DeclineRequest: Codable, Sendable { var reason: String? }
struct EmptyBody: Codable, Sendable {}
struct RequestIds: Codable, Sendable { var ids: [Int64] }

struct BulkResult: Codable, Sendable {
    struct Failure: Codable, Sendable { var id: Int64; var code: String; var message: String? }
    var done: Int
    var errors: [Failure]?
}

struct Comment: Codable, Sendable, Hashable, Identifiable {
    var id: Int64
    var user: UserRef
    var body: String
    var createdAt: String?
}

struct CommentInput: Codable, Sendable { var body: String }

struct Issue: Codable, Sendable, Hashable, Identifiable {
    var id: Int64
    var kind: String
    var type: String
    var status: String
    var title: String
    var tmdbId: Int64?
    var mbid: String?
    var poster: String?
    var seasonNumber: Int?
    var episodeNumber: Int?
    var message: String
    var reportedBy: UserRef?
    var resolvedBy: UserRef?
    var resolvedAt: String?
    var replacementSearched: Bool?
    var createdAt: String?
    var libraryRef: String?
    var commentCount: Int?
}

struct IssueList: Codable, Sendable {
    var items: [Issue]
    var total: Int?
}

struct IssueInput: Codable, Sendable {
    var kind: String
    var tmdbId: Int64?
    var mbid: String?
    var type: String
    var seasonNumber: Int?
    var episodeNumber: Int?
    var message: String
}

struct ResolveIssueRequest: Codable, Sendable {
    var searchReplacement: Bool?
    var comment: String?
}

// MARK: Library

struct Quality: Codable, Sendable, Hashable {
    var id: String?
    var name: String
}

struct MediaInfo: Codable, Sendable, Hashable {
    var source: String?
    var resolution: Int?
    var videoCodec: String?
    var bitDepth: Int?
    var hdr: [String]?
    var audioCodec: String?
    var audioChannels: String?
    var languages: [String]?
    var subtitles: [String]?
}

struct MediaFile: Codable, Sendable, Hashable {
    var id: Int64
    var path: String
    var sizeBytes: Int64?
    var quality: Quality?
    var customFormats: [String]?
    var score: Int?
    var releaseGroup: String?
    var languages: [String]?
    var mediaInfo: MediaInfo?
    var sceneName: String?
    var addedAt: String?
}

struct ItemDownload: Codable, Sendable, Hashable {
    var id: Int64
    var title: String?
    var progress: Double?
    var state: String?
    var status: String?
}

struct Movie: Codable, Sendable, Hashable, Identifiable {
    var id: Int64
    var itemRef: String?
    var tmdbId: Int64?
    var title: String
    var year: Int?
    var overview: String?
    var runtime: Int?
    var studio: String?
    var genres: [String]?
    var certification: String?
    var rating: Double?
    var inCinemas: String?
    var digitalRelease: String?
    var physicalRelease: String?
    var poster: String?
    var backdrop: String?
    var monitored: Bool?
    var qualityProfileId: Int64?
    var rootFolderId: Int64?
    var path: String?
    var minimumAvailability: String?
    var tags: [String]?
    var addedAt: String?
    var available: Bool?
    var cutoffMet: Bool?
    var libraryStatus: String?
    var file: MediaFile?
    var download: ItemDownload?

    var ref: String { itemRef ?? "movie:\(id)" }
}

struct MovieList: Codable, Sendable {
    var total: Int?
    var items: [Movie]
}

struct MovieLookup: Codable, Sendable, Hashable, Identifiable {
    var tmdbId: Int64
    var title: String
    var year: Int?
    var overview: String?
    var poster: String?
    var movieId: Int64?
    var id: Int64 { tmdbId }
}

struct AddMovieRequest: Codable, Sendable {
    var tmdbId: Int64
    var rootFolderId: Int64
    var qualityProfileId: Int64
    var minimumAvailability: String?
    var monitored: Bool?
    var searchOnAdd: Bool?
}

struct MovieUpdate: Codable, Sendable {
    var monitored: Bool?
    var qualityProfileId: Int64?
}

struct SeriesStats: Codable, Sendable, Hashable {
    var seasonCount: Int?
    var episodeCount: Int?
    var episodeFileCount: Int?
    var monitoredEpisodeCount: Int?
    var missingCount: Int?
    var sizeOnDisk: Int64?
    var nextAiring: String?
    var previousAiring: String?
}

struct SeasonInfo: Codable, Sendable, Hashable, Identifiable {
    var id: Int64
    var itemRef: String?
    var number: Int
    var monitored: Bool?
    var title: String?
    var stats: SeriesStats?
}

struct Series: Codable, Sendable, Hashable, Identifiable {
    var id: Int64
    var itemRef: String?
    var tmdbId: Int64?
    var tvdbId: Int64?
    var title: String
    var year: Int?
    var overview: String?
    var status: String?
    var network: String?
    var runtime: Int?
    var genres: [String]?
    var rating: Double?
    var seriesType: String?
    var poster: String?
    var backdrop: String?
    var monitored: Bool?
    var qualityProfileId: Int64?
    var rootFolderId: Int64?
    var path: String?
    var tags: [String]?
    var stats: SeriesStats?
    var downloading: Int?
    var seasons: [SeasonInfo]?

    var ref: String { itemRef ?? "series:\(id)" }
}

struct SeriesList: Codable, Sendable {
    var total: Int?
    var items: [Series]
}

struct Episode: Codable, Sendable, Hashable, Identifiable {
    var id: Int64
    var itemRef: String?
    var seriesId: Int64?
    var seasonNumber: Int
    var episodeNumber: Int
    var absoluteNumber: Int?
    var title: String?
    var overview: String?
    var airDate: String?
    var airDateUtc: String?
    var monitored: Bool?
    var aired: Bool?
    var wanted: String?
    var cutoffMet: Bool?
    var file: MediaFile?
    var download: ItemDownload?
    // Wanted list extras.
    var seriesTitle: String?
    var poster: String?

    var ref: String { itemRef ?? "episode:\(id)" }
    var code: String { String(format: "S%02dE%02d", seasonNumber, episodeNumber) }
}

struct WantedEpisodeList: Codable, Sendable {
    var total: Int?
    var items: [Episode]
}

struct SeriesLookup: Codable, Sendable, Hashable, Identifiable {
    var tmdbId: Int64?
    var tvdbId: Int64?
    var source: String?
    var title: String
    var year: Int?
    var overview: String?
    var poster: String?
    var network: String?
    var seriesId: Int64?
    var id: String { "\(tmdbId ?? 0)-\(tvdbId ?? 0)" }
}

struct AddSeriesRequest: Codable, Sendable {
    var tmdbId: Int64?
    var tvdbId: Int64?
    var rootFolderId: Int64
    var qualityProfileId: Int64
    var monitor: String?
    var monitored: Bool?
    var seasonFolder: Bool?
    var searchOnAdd: Bool?
}

struct SeriesUpdate: Codable, Sendable {
    var monitored: Bool?
    var qualityProfileId: Int64?
}

struct SeasonUpdate: Codable, Sendable { var monitored: Bool }
struct EpisodeIds: Codable, Sendable { var episodeIds: [Int64] }
struct MovieIds: Codable, Sendable { var movieIds: [Int64] }
struct MonitorEpisodes: Codable, Sendable { var episodeIds: [Int64]; var monitored: Bool }

struct Artist: Codable, Sendable, Hashable, Identifiable {
    var id: Int64
    var itemRef: String?
    var mbid: String?
    var name: String
    var disambiguation: String?
    var type: String?
    var country: String?
    var genres: [String]?
    var monitored: Bool?
    var qualityProfileId: Int64?
    var metadataProfileId: Int64?
    var path: String?
    var albums: Int?
    var monitoredAlbums: Int?
    var albumsWithFiles: Int?
    var files: Int?
    var sizeBytes: Int64?
    var cover: String?
    var latestRelease: String?
}

struct ArtistList: Codable, Sendable {
    var total: Int?
    var items: [Artist]
}

struct ArtistLookup: Codable, Sendable, Hashable, Identifiable {
    var mbid: String
    var name: String
    var disambiguation: String?
    var type: String?
    var country: String?
    var artistId: Int64?
    var id: String { mbid }
}

struct AddArtistRequest: Codable, Sendable {
    var mbid: String
    var rootFolderId: Int64
    var qualityProfileId: Int64
    var metadataProfileId: Int64
    var monitored: Bool?
    var monitor: String?
    var searchOnAdd: Bool?
}

struct ArtistUpdate: Codable, Sendable { var monitored: Bool }

struct Album: Codable, Sendable, Hashable, Identifiable {
    var id: Int64
    var itemRef: String?
    var artistId: Int64?
    var artistName: String?
    var title: String
    var primaryType: String?
    var secondaryTypes: [String]?
    var releaseDate: String?
    var monitored: Bool?
    var trackCount: Int?
    var fileCount: Int?
    var sizeBytes: Int64?
    var quality: Quality?
    var cutoffMet: Bool?
    var libraryStatus: String?
    var cover: String?
    var download: ItemDownload?

    var ref: String { itemRef ?? "album:\(id)" }
}

struct AlbumList: Codable, Sendable {
    var total: Int?
    var items: [Album]
}

struct TrackFile: Codable, Sendable, Hashable {
    var id: Int64
    var path: String
    var sizeBytes: Int64?
    var quality: Quality?
    var format: String?
    var bitrate: Int?
}

struct AlbumTrack: Codable, Sendable, Hashable, Identifiable {
    var id: Int64
    var disc: Int?
    var position: Int?
    var number: String?
    var title: String
    var lengthMs: Int?
    var file: TrackFile?
}

struct AlbumDetail: Codable, Sendable {
    var album: Album
    var tracks: [AlbumTrack]?
    var tracksError: String?
}

struct AlbumUpdate: Codable, Sendable { var monitored: Bool }

struct QueuedCount: Codable, Sendable { var count: Int }

struct QualityProfile: Codable, Sendable, Hashable, Identifiable {
    var id: Int64
    var name: String
    var kind: String
}

struct MetadataProfile: Codable, Sendable, Hashable, Identifiable {
    var id: Int64
    var name: String
}

struct RootFolder: Codable, Sendable, Hashable, Identifiable {
    var id: Int64
    var path: String
    var kind: String
    var accessible: Bool?
    var writable: Bool?
    var notWritableReason: String?
    var notWritableDetail: String?
    var freeSpaceBytes: Int64?
    var totalSpaceBytes: Int64?
}

// MARK: Releases (interactive search)

struct ReasonCount: Codable, Sendable, Hashable {
    var code: String
    var count: Int?
    var example: String?
}

struct Rejection: Codable, Sendable, Hashable {
    var code: String
    var message: String?
    var temporary: Bool?
}

struct Candidate: Codable, Sendable, Hashable {
    var guid: String?
    var title: String
    var size: Int64?
    var `protocol`: String?
    var indexerId: Int64?
    var indexerName: String?
    var seeders: Int?
    var peers: Int?
    var publishDate: String?
}

struct MatchedFormat: Codable, Sendable, Hashable {
    var id: Int64?
    var name: String
    var score: Int?
}

struct Decision: Codable, Sendable, Hashable {
    var candidate: Candidate
    var quality: Quality?
    var customFormats: [MatchedFormat]?
    var score: Int?
    var status: String
    var rank: Int?
    var rejections: [Rejection]?
}

struct InteractiveDecision: Codable, Sendable, Hashable {
    var decision: Decision
    var downloadToken: String?
    var magnetUrl: String?
}

struct SearchIndexerResult: Codable, Sendable, Hashable {
    var indexerId: Int64?
    var indexerName: String
    var status: String
    var count: Int?
    var durationMs: Int64?
    var errorCode: String?
    var error: String?
}

struct InteractiveSearch: Codable, Sendable {
    var kind: String?
    var profileName: String?
    var accepted: Int?
    var pending: Int?
    var rejected: Int?
    var decisions: [InteractiveDecision]
    var indexers: [SearchIndexerResult]?
    var durationMs: Int64?
}

struct ManualGrabRequest: Codable, Sendable {
    var indexerId: Int64
    var downloadToken: String?
    var magnetUrl: String?
    var title: String
    var `protocol`: String
    var guid: String?
    var publishDate: String?
}

// MARK: Queue, board, timeline

struct QueueItemLink: Codable, Sendable, Hashable {
    var ref: String
    var kind: String?
    var id: Int64?
    var title: String
    var year: Int?
}

struct QueueItem: Codable, Sendable, Hashable, Identifiable {
    var id: Int64
    var unknown: Bool?
    var clientName: String?
    var `protocol`: String?
    var title: String
    var indexer: String?
    var status: String?
    var state: String?
    var progress: Double?
    var sizeBytes: Int64?
    var downloadedBytes: Int64?
    var downloadSpeed: Int64?
    var etaSeconds: Int64?
    var ratio: Double?
    var message: String?
    var item: QueueItemLink?
    var addedAt: String?
}

struct ClientStatus: Codable, Sendable, Hashable {
    var ok: Bool?
    var checkedAt: String?
    var errorCode: String?
    var error: String?
    var version: String?
    var freeSpaceBytes: Int64?
}

struct QueueClient: Codable, Sendable, Hashable, Identifiable {
    var id: Int64
    var name: String
    var `protocol`: String?
    var enabled: Bool?
    var canPrioritise: Bool?
    var status: ClientStatus?
}

struct Queue: Codable, Sendable {
    var items: [QueueItem]
    var clients: [QueueClient]?
}

struct PriorityRequest: Codable, Sendable { var priority: String }

struct BoardKindCounts: Codable, Sendable, Hashable {
    var movie: Int?
    var episode: Int?
    var album: Int?
    var subtitle: Int?
}

struct BoardGroup: Codable, Sendable, Hashable, Identifiable {
    var code: String
    var tone: String?
    var fix: String?
    var count: Int
    var kinds: BoardKindCounts?
    var searches: Int?
    var reasons: [ReasonCount]?
    var id: String { code }
}

struct BoardSummary: Codable, Sendable {
    var total: Int?
    var updatedAt: String?
    var groups: [BoardGroup]
}

struct BoardItem: Codable, Sendable, Hashable, Identifiable {
    var key: String
    var kind: String
    var itemRef: String
    var parentRef: String?
    var title: String
    var code: String
    var tone: String?
    var fix: String?
    var message: String?
    var params: JSONValue?
    var reasons: [ReasonCount]?
    var searches: Int?
    var updatedAt: String?
    var id: String { key }
}

struct BoardItemPage: Codable, Sendable {
    var total: Int?
    var items: [BoardItem]
}

struct BoardActionRequest: Codable, Sendable {
    var action: String
    var keys: [String]
    var qualityProfileId: Int64?
}

struct BoardActionResult: Codable, Sendable {
    struct Failure: Codable, Sendable { var key: String; var code: String; var message: String? }
    var done: Int
    var failed: [Failure]?
}

struct TimelineEvent: Codable, Sendable, Hashable, Identifiable {
    var id: Int64
    var at: String
    var type: String
    var source: String?
    var message: String?
    var data: JSONValue?
    var downloadId: Int64?
    var key: String { "\(source ?? "")-\(id)" }
}

struct Timeline: Codable, Sendable {
    var item: String
    var title: String?
    var explanation: Explanation
    var events: [TimelineEvent]
}

// MARK: Calendar, history, health, status

struct CalendarEntry: Codable, Sendable, Hashable {
    var date: String
    var kind: String
    var movieId: Int64?
    var seriesId: Int64?
    var episodeId: Int64?
    var seasonNumber: Int?
    var episodeNumber: Int?
    var episodeTitle: String?
    var airDateUtc: String?
    var network: String?
    var title: String
    var year: Int?
    var hasFile: Bool?
    var monitored: Bool?
    var poster: String?
}

struct AlbumCalendarEntry: Codable, Sendable, Hashable {
    var date: String
    var albumId: Int64
    var artistId: Int64?
    var artistName: String?
    var title: String
    var type: String?
    var monitored: Bool?
    var hasFiles: Bool?
    var cover: String?
}

struct HistoryEntry: Codable, Sendable, Hashable, Identifiable {
    var id: String
    var at: String
    var category: String
    var type: String?
    var kind: String?
    var itemRef: String?
    var title: String
    var release: String?
    var indexer: String?
    var client: String?
    var message: String?
}

struct HistoryPage: Codable, Sendable {
    var items: [HistoryEntry]
    var hasMore: Bool?
}

struct HealthIssue: Codable, Sendable, Hashable, Identifiable {
    var key: String
    var check: String?
    var severity: String
    var code: String
    var message: String?
    var params: JSONValue?
    var docs: String?
    var fixRoute: String?
    var since: String?
    var id: String { key }
}

struct HealthReport: Codable, Sendable {
    var checkedAt: String?
    var issues: [HealthIssue]
}

struct SystemStatus: Codable, Sendable {
    var version: String?
    var commit: String?
    var startedAt: String?
    var uptimeSeconds: Int64?
    var databaseSizeBytes: Int64?
}

struct IndexerHealth: Codable, Sendable, Hashable {
    var status: String?
    var failures: Int?
    var lastError: String?
    var lastErrorCode: String?
    var lastErrorAt: String?
    var lastSuccessAt: String?
    var retryAt: String?
}

struct Indexer: Codable, Sendable, Hashable, Identifiable {
    var id: Int64
    var name: String
    var kind: String?
    var `protocol`: String?
    var baseUrl: String?
    var enabled: Bool?
    var priority: Int?
    var health: IndexerHealth?
}

struct IndexerTestResult: Codable, Sendable {
    var ok: Bool
    var durationMs: Int64?
    var errorCode: String?
    var error: String?
}

struct DownloadClient: Codable, Sendable, Hashable, Identifiable {
    var id: Int64
    var name: String
    var implementation: String?
    var `protocol`: String?
    var enabled: Bool?
    var host: String?
    var port: Int?
    var status: ClientStatus?
}

struct DownloadClientTestResult: Codable, Sendable {
    var ok: Bool
    var errorCode: String?
    var message: String?
    var version: String?
    var missingCategories: [String]?
}
