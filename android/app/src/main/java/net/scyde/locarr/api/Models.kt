package net.scyde.locarr.api

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject

// Wire models for the parts of api/openapi.yaml the app uses. Every field has a default and
// enums are plain strings, so a newer server (new fields, new enum values) never breaks parsing.

@Serializable data class ApiErrorBody(val code: String = "generic", val message: String = "")

@Serializable data class SystemInfo(
    val product: String = "", val instanceName: String = "", val version: String = "",
    val commit: String = "", val setupRequired: Boolean = false,
)

@Serializable data class DeviceInfo(val name: String? = null, val client: String? = null)

@Serializable data class LoginRequest(
    val username: String, val password: String, val totpCode: String? = null,
    val recoveryCode: String? = null, val device: DeviceInfo? = null,
)

@Serializable data class ProviderLoginRequest(
    val username: String, val password: String, val totpCode: String? = null, val device: DeviceInfo? = null,
)

@Serializable data class AuthResult(val user: User = User(), val token: String? = null)

@Serializable data class AuthProvider(val id: String = "", val kind: String = "", val name: String = "")

@Serializable data class Permissions(
    val requestMovies: Boolean = false, val requestSeries: Boolean = false, val requestMusic: Boolean = false,
    val autoApprove: Boolean = false, val request4k: Boolean = false,
    val manageLibrary: Boolean = false, val manageRequests: Boolean = false,
)

@Serializable data class User(
    val id: Long = 0, val username: String = "", val role: String = "requester",
    val permissions: Permissions = Permissions(), val language: String = "en",
    val disabled: Boolean = false, val totpEnabled: Boolean = false,
) {
    val isAdmin get() = role == "admin"
    val canManageLibrary get() = isAdmin || permissions.manageLibrary
    val canManageRequests get() = isAdmin || permissions.manageRequests
}

@Serializable data class UserRef(val id: Long = 0, val username: String = "")

// Notifications

@Serializable data class Notification(
    val id: Long = 0, val type: String = "", val title: String = "", val message: String? = null,
    val poster: String? = null, val mediaKind: String? = null, val actor: UserRef? = null,
    val requestId: Long? = null, val issueId: Long? = null, val params: JsonObject? = null,
    val createdAt: String = "", val read: Boolean = false,
)

@Serializable data class NotificationList(val items: List<Notification> = emptyList(), val unread: Int = 0, val latestId: Long = 0)
@Serializable data class MarkReadRequest(val ids: List<Long>? = null, val all: Boolean? = null)
@Serializable data class UnreadCount(val unread: Int = 0)

// Discover and requests

@Serializable data class DiscoverItem(
    val mediaType: String = "movie", val tmdbId: Long? = null, val mbid: String? = null, val artistMbid: String? = null,
    val title: String = "", val subtitle: String? = null, val year: Int = 0, val date: String? = null,
    val overview: String? = null, val poster: String? = null, val backdrop: String? = null, val rating: Double? = null,
    val status: String = "none", val status4k: String = "none", val requestId: Long? = null, val streameroUrl: String? = null,
) {
    /** A stable key for lists. */
    val key get() = "$mediaType:${tmdbId ?: mbid ?: title}"
    val isMusic get() = mediaType == "album" || mediaType == "artist"
}

@Serializable data class DiscoverRow(val key: String = "", val items: List<DiscoverItem> = emptyList())
@Serializable data class DiscoverHome(
    val rows: List<DiscoverRow> = emptyList(), val requests: List<MediaRequest> = emptyList(),
    val metadataReady: Boolean = true, val musicReady: Boolean = true,
)
@Serializable data class DiscoverPage(val items: List<DiscoverItem> = emptyList(), val page: Int = 1, val totalPages: Int = 1)

@Serializable data class CastMember(val personId: Long? = null, val name: String = "", val character: String? = null, val profile: String? = null)
@Serializable data class SeasonAvailability(
    val number: Int = 0, val title: String = "", val episodeCount: Int = 0, val airDate: String? = null,
    val poster: String? = null, val status: String = "none", val status4k: String = "none", val episodesAvailable: Int? = null,
)
@Serializable data class RequestAbility(
    val canRequest: Boolean = false, val canRequest4k: Boolean = false, val autoApprove: Boolean = false,
    val autoApprove4k: Boolean = false, val canSetOptions: Boolean = false, val canReportIssue: Boolean = false,
)
@Serializable data class TitleDetail(
    val item: DiscoverItem = DiscoverItem(), val originalTitle: String? = null, val tagline: String? = null,
    val runtime: Int? = null, val genres: List<String> = emptyList(), val certification: String? = null,
    val releaseDate: String? = null, val studio: String? = null, val network: String? = null, val seriesStatus: String? = null,
    val trailerUrl: String? = null, val cast: List<CastMember> = emptyList(), val seasons: List<SeasonAvailability> = emptyList(),
    val albums: List<DiscoverItem> = emptyList(), val requests: List<MediaRequest> = emptyList(),
    val ability: RequestAbility = RequestAbility(), val libraryRef: String? = null,
)
@Serializable data class PersonDetail(
    val tmdbId: Long = 0, val name: String = "", val biography: String? = null, val profile: String? = null,
    val birthday: String? = null, val department: String? = null, val knownFor: List<DiscoverItem> = emptyList(),
)

@Serializable data class RequestSeason(val number: Int = 0, val status: String = "")
@Serializable data class RequestExplanation(val code: String = "", val tone: String = "info", val message: String = "", val params: JsonObject? = null)
@Serializable data class RequestProgress(val percent: Double = 0.0, val downloads: Int = 0, val state: String? = null, val etaSeconds: Int? = null)

@Serializable data class MediaRequest(
    val id: Long = 0, val kind: String = "movie", val is4k: Boolean = false, val status: String = "pending",
    val tmdbId: Long? = null, val mbid: String? = null, val artistMbid: String? = null,
    val title: String = "", val subtitle: String? = null, val year: Int = 0, val poster: String? = null,
    val seasons: List<RequestSeason> = emptyList(), val allSeasons: Boolean = false, val futureSeasons: Boolean = false,
    val requestedBy: UserRef = UserRef(), val decidedBy: UserRef? = null, val autoApproved: Boolean = false,
    val decidedAt: String? = null, val declineReason: String? = null, val createdAt: String = "", val updatedAt: String = "",
    val availableAt: String? = null, val explanation: RequestExplanation? = null, val progress: RequestProgress? = null,
    val libraryRef: String? = null, val commentCount: Int = 0, val canCancel: Boolean = false,
)
@Serializable data class RequestList(val items: List<MediaRequest> = emptyList(), val total: Int = 0)
@Serializable data class QuotaStatus(val limit: Int = 0, val days: Int = 0, val used: Int = 0, val remaining: Int? = null, val unlimited: Boolean = true)
@Serializable data class Quotas(val movie: QuotaStatus = QuotaStatus(), val series: QuotaStatus = QuotaStatus(), val music: QuotaStatus = QuotaStatus())
@Serializable data class RequestSummary(val pending: Int = 0, val processing: Int = 0, val failed: Int = 0, val openIssues: Int = 0, val quotas: Quotas = Quotas())

@Serializable data class CreateRequest(
    val kind: String, val tmdbId: Long? = null, val mbid: String? = null, val is4k: Boolean = false,
    val seasons: List<Int>? = null, val allSeasons: Boolean? = null, val futureSeasons: Boolean? = null, val comment: String? = null,
)
@Serializable data class DeclineRequest(val reason: String? = null)
@Serializable data class RequestIds(val ids: List<Long>)
@Serializable data class BulkResult(val done: Int = 0)
@Serializable class Empty

@Serializable data class Comment(val id: Long = 0, val body: String = "", val author: UserRef? = null, val user: UserRef? = null, val createdAt: String = "") {
    val who get() = author ?: user
}
@Serializable data class CommentInput(val body: String)

@Serializable data class IssueInput(
    val kind: String, val tmdbId: Long? = null, val mbid: String? = null, val type: String,
    val seasonNumber: Int? = null, val episodeNumber: Int? = null, val message: String,
)
@Serializable data class Issue(
    val id: Long = 0, val kind: String = "movie", val type: String = "other", val status: String = "open",
    val title: String = "", val tmdbId: Long? = null, val mbid: String? = null, val poster: String? = null,
    val seasonNumber: Int? = null, val episodeNumber: Int? = null, val message: String = "",
    val reportedBy: UserRef = UserRef(), val resolvedBy: UserRef? = null, val createdAt: String = "",
    val libraryRef: String? = null, val commentCount: Int = 0,
)
@Serializable data class IssueList(val items: List<Issue> = emptyList(), val total: Int = 0)

// Library

@Serializable data class Quality(val id: String = "", val name: String = "", val resolution: Int? = null)
@Serializable data class MediaInfo(
    val source: String = "", val resolution: Int? = null, val videoCodec: String? = null, val hdr: List<String>? = null,
    val audioCodec: String? = null, val audioChannels: String? = null, val languages: List<String>? = null, val subtitles: List<String>? = null,
)
@Serializable data class MediaFile(
    val id: Long = 0, val path: String = "", val sizeBytes: Long = 0, val quality: Quality = Quality(),
    val customFormats: List<String> = emptyList(), val score: Int = 0, val releaseGroup: String = "",
    val languages: List<String> = emptyList(), val mediaInfo: MediaInfo = MediaInfo(), val addedAt: String = "",
)
@Serializable data class ItemDownload(val id: Long = 0, val title: String = "", val progress: Double = 0.0, val state: String = "", val status: String = "")

@Serializable data class Movie(
    val id: Long = 0, val itemRef: String? = null, val tmdbId: Long = 0, val title: String = "", val year: Int = 0,
    val overview: String = "", val runtime: Int = 0, val studio: String = "", val genres: List<String> = emptyList(),
    val rating: Double = 0.0, val inCinemas: String = "", val digitalRelease: String = "", val physicalRelease: String = "",
    val poster: String = "", val backdrop: String = "", val monitored: Boolean = false, val qualityProfileId: Long = 0,
    val path: String = "", val minimumAvailability: String = "released", val tags: List<String> = emptyList(),
    val available: Boolean = false, val cutoffMet: Boolean = false, val libraryStatus: String = "missing",
    val file: MediaFile? = null, val download: ItemDownload? = null,
) {
    val ref get() = itemRef ?: "movie:$id"
}
@Serializable data class MovieList(val total: Int = 0, val items: List<Movie> = emptyList())
@Serializable data class MovieUpdate(val monitored: Boolean? = null, val qualityProfileId: Long? = null)

@Serializable data class SeriesStats(
    val seasonCount: Int = 0, val episodeCount: Int = 0, val episodeFileCount: Int = 0,
    val monitoredEpisodeCount: Int = 0, val missingCount: Int = 0, val sizeOnDisk: Long = 0,
    val nextAiring: String? = null, val previousAiring: String? = null,
)
@Serializable data class SeasonInfo(
    val id: Long = 0, val itemRef: String = "", val number: Int = 0, val monitored: Boolean = false,
    val title: String = "", val poster: String = "", val airDate: String = "", val stats: SeriesStats = SeriesStats(),
)
@Serializable data class Series(
    val id: Long = 0, val itemRef: String = "", val tmdbId: Long = 0, val title: String = "", val year: Int = 0,
    val overview: String = "", val status: String = "", val network: String = "", val genres: List<String> = emptyList(),
    val rating: Double = 0.0, val seriesType: String = "standard", val poster: String = "", val backdrop: String = "",
    val monitored: Boolean = false, val qualityProfileId: Long = 0, val path: String = "", val tags: List<String> = emptyList(),
    val stats: SeriesStats = SeriesStats(), val downloading: Int = 0, val seasons: List<SeasonInfo> = emptyList(),
) {
    val ref get() = itemRef.ifEmpty { "series:$id" }
}
@Serializable data class SeriesList(val total: Int = 0, val items: List<Series> = emptyList())
@Serializable data class SeriesUpdate(val monitored: Boolean? = null)

@Serializable data class Episode(
    val id: Long = 0, val itemRef: String = "", val seriesId: Long = 0, val seasonNumber: Int = 0, val episodeNumber: Int = 0,
    val absoluteNumber: Int = 0, val title: String = "", val overview: String = "", val airDate: String = "",
    val airDateUtc: String? = null, val monitored: Boolean = false, val aired: Boolean = false, val wanted: String = "none",
    val cutoffMet: Boolean = false, val file: MediaFile? = null, val download: ItemDownload? = null,
    // Only in the wanted list
    val seriesTitle: String = "", val poster: String = "",
) {
    val ref get() = itemRef.ifEmpty { "episode:$id" }
}
@Serializable data class WantedEpisodeList(val total: Int = 0, val items: List<Episode> = emptyList())
@Serializable data class MonitorEpisodesRequest(val episodeIds: List<Long>, val monitored: Boolean)

@Serializable data class Artist(
    val id: Long = 0, val itemRef: String = "", val mbid: String = "", val name: String = "", val disambiguation: String = "",
    val type: String = "", val country: String = "", val genres: List<String> = emptyList(), val monitored: Boolean = false,
    val qualityProfileId: Long = 0, val path: String = "", val albums: Int = 0, val monitoredAlbums: Int = 0,
    val albumsWithFiles: Int = 0, val files: Int = 0, val sizeBytes: Long = 0, val cover: String = "",
) {
    val ref get() = itemRef.ifEmpty { "artist:$id" }
}
@Serializable data class ArtistList(val total: Int = 0, val items: List<Artist> = emptyList())
@Serializable data class AlbumRelease(val id: Long = 0, val title: String = "", val date: String = "", val country: String = "", val format: String = "", val trackCount: Int = 0)
@Serializable data class Album(
    val id: Long = 0, val itemRef: String = "", val artistId: Long = 0, val artistName: String = "", val mbid: String = "",
    val title: String = "", val primaryType: String = "", val releaseDate: String = "", val monitored: Boolean = false,
    val path: String = "", val release: AlbumRelease? = null, val trackCount: Int = 0, val fileCount: Int = 0,
    val sizeBytes: Long = 0, val quality: Quality? = null, val cutoffMet: Boolean = false, val libraryStatus: String = "missing",
    val cover: String = "", val download: ItemDownload? = null,
) {
    val ref get() = itemRef.ifEmpty { "album:$id" }
}
@Serializable data class TrackFile(val id: Long = 0, val path: String = "", val sizeBytes: Long = 0, val quality: Quality? = null)
@Serializable data class AlbumTrack(val id: Long = 0, val disc: Int = 1, val position: Int = 0, val number: String = "", val title: String = "", val lengthMs: Int = 0, val file: TrackFile? = null)
@Serializable data class AlbumDetail(val album: Album = Album(), val tracks: List<AlbumTrack> = emptyList(), val tracksError: String? = null)
@Serializable data class AlbumMonitorRequest(val albumIds: List<Long>, val monitored: Boolean)

@Serializable data class MovieLookup(
    val tmdbId: Long = 0, val title: String = "", val year: Int = 0, val overview: String = "",
    val poster: String = "", val movieId: Long? = null,
)
@Serializable data class SeriesLookup(
    val tmdbId: Long = 0, val tvdbId: Long = 0, val source: String = "tmdb", val title: String = "", val year: Int = 0,
    val overview: String = "", val poster: String = "", val network: String = "", val seriesId: Long? = null,
)
@Serializable data class ArtistLookup(
    val mbid: String = "", val name: String = "", val disambiguation: String = "", val type: String = "",
    val country: String = "", val artistId: Long? = null,
)
@Serializable data class AddMovieRequest(
    val tmdbId: Long, val rootFolderId: Long, val qualityProfileId: Long, val monitored: Boolean = true,
    val minimumAvailability: String = "released", val searchOnAdd: Boolean = true,
)
@Serializable data class AddSeriesRequest(
    val tmdbId: Long? = null, val tvdbId: Long? = null, val rootFolderId: Long, val qualityProfileId: Long,
    val monitor: String = "all", val seriesType: String = "standard", val monitored: Boolean = true, val searchOnAdd: Boolean = true,
)
@Serializable data class AddArtistRequest(
    val mbid: String, val rootFolderId: Long, val qualityProfileId: Long, val metadataProfileId: Long,
    val monitor: String = "all", val monitored: Boolean = true, val searchOnAdd: Boolean = true,
)

@Serializable data class QualityProfile(val id: Long = 0, val name: String = "", val kind: String = "movies")
@Serializable data class MetadataProfile(val id: Long = 0, val name: String = "")
@Serializable data class RootFolder(
    val id: Long = 0, val path: String = "", val kind: String = "movies", val accessible: Boolean = true,
    val writable: Boolean = true, val notWritableReason: String? = null, val notWritableDetail: String? = null,
    val freeSpaceBytes: Long? = null, val totalSpaceBytes: Long? = null,
)

// Timeline

@Serializable data class ReasonCount(val code: String = "", val count: Int = 0, val example: String = "")
@Serializable data class Explanation(
    val code: String = "", val tone: String = "info", val message: String = "",
    val params: JsonObject? = null, val reasons: List<ReasonCount> = emptyList(),
)
@Serializable data class TimelineEvent(
    val id: Long = 0, val at: String = "", val type: String = "", val source: String = "",
    val message: String = "", val data: JsonObject? = null,
)
@Serializable data class Timeline(val item: String = "", val title: String? = null, val explanation: Explanation = Explanation(), val events: List<TimelineEvent> = emptyList())
@Serializable data class QueuedCount(val count: Int = 0)

// Interactive search

@Serializable data class Candidate(
    val guid: String? = null, val title: String = "", val size: Long? = null, val protocol: String? = null,
    val indexerId: Long? = null, val indexerName: String? = null, val seeders: Int? = null, val peers: Int? = null,
    val publishDate: String? = null,
)
@Serializable data class MatchedFormat(val id: Long = 0, val name: String = "", val score: Int = 0)
@Serializable data class Rejection(val code: String = "", val message: String = "", val temporary: Boolean = false)
@Serializable data class ParsedLanguages(val languages: List<String>? = null)
@Serializable data class Decision(
    val candidate: Candidate = Candidate(), val quality: Quality = Quality(), val customFormats: List<MatchedFormat> = emptyList(),
    val score: Int = 0, val status: String = "rejected", val rank: Int? = null, val rejections: List<Rejection> = emptyList(),
)
@Serializable data class InteractiveDecision(val decision: Decision = Decision(), val downloadToken: String? = null, val magnetUrl: String? = null)
@Serializable data class SearchIndexerResult(
    val indexerId: Long = 0, val indexerName: String = "", val status: String = "ok", val count: Int = 0, val error: String? = null,
)
@Serializable data class InteractiveSearch(
    val profileName: String = "", val accepted: Int = 0, val pending: Int = 0, val rejected: Int = 0,
    val decisions: List<InteractiveDecision> = emptyList(), val indexers: List<SearchIndexerResult> = emptyList(), val durationMs: Long = 0,
)
@Serializable data class ManualGrabRequest(
    val indexerId: Long, val downloadToken: String? = null, val magnetUrl: String? = null, val title: String,
    val protocol: String, val guid: String? = null, val publishDate: String? = null,
)

// Activity

@Serializable data class QueueItemLink(val ref: String = "", val kind: String = "", val id: Long = 0, val title: String = "", val year: Int? = null)
@Serializable data class QueueItem(
    val id: Long = 0, val unknown: Boolean = false, val clientId: Long? = null, val clientName: String = "", val protocol: String = "torrent",
    val title: String = "", val indexer: String? = null, val status: String = "active", val state: String = "queued",
    val progress: Double = 0.0, val sizeBytes: Long = 0, val downloadedBytes: Long = 0, val downloadSpeed: Long = 0,
    val etaSeconds: Long? = null, val message: String = "", val item: QueueItemLink? = null, val addedAt: String = "",
)
@Serializable data class DownloadClientStatus(val ok: Boolean = false, val checkedAt: String? = null, val errorCode: String? = null, val error: String? = null, val version: String? = null, val freeSpaceBytes: Long? = null)
@Serializable data class QueueClient(val id: Long = 0, val name: String = "", val protocol: String = "", val enabled: Boolean = true, val canPrioritise: Boolean = false, val status: DownloadClientStatus = DownloadClientStatus())
@Serializable data class Queue(val items: List<QueueItem> = emptyList(), val clients: List<QueueClient> = emptyList())
@Serializable data class PriorityRequest(val priority: String)

@Serializable data class CalendarEntry(
    val date: String = "", val kind: String = "", val movieId: Long? = null, val seriesId: Long? = null, val episodeId: Long? = null,
    val seasonNumber: Int? = null, val episodeNumber: Int? = null, val episodeTitle: String? = null, val airDateUtc: String? = null,
    val network: String? = null, val title: String = "", val year: Int = 0, val hasFile: Boolean = false,
    val monitored: Boolean = true, val poster: String = "",
)

@Serializable data class BoardKindCounts(val movie: Int = 0, val episode: Int = 0, val album: Int = 0, val subtitle: Int = 0)
@Serializable data class BoardGroup(
    val code: String = "", val tone: String = "info", val fix: String = "", val count: Int = 0,
    val kinds: BoardKindCounts = BoardKindCounts(), val searches: Int = 0, val reasons: List<ReasonCount> = emptyList(),
)
@Serializable data class BoardSummary(val total: Int = 0, val updatedAt: String? = null, val groups: List<BoardGroup> = emptyList())
@Serializable data class BoardItem(
    val key: String = "", val kind: String = "", val itemRef: String = "", val parentRef: String = "", val title: String = "",
    val code: String = "", val tone: String = "info", val fix: String = "", val message: String = "",
    val params: JsonObject? = null, val reasons: List<ReasonCount> = emptyList(), val searches: Int = 0,
)
@Serializable data class BoardItemPage(val total: Int = 0, val items: List<BoardItem> = emptyList())
@Serializable data class BoardActionRequest(val action: String, val keys: List<String>)
@Serializable data class BoardActionResult(val done: Int = 0)

@Serializable data class HealthIssue(
    val key: String = "", val check: String = "", val severity: String = "warning", val code: String = "",
    val message: String = "", val params: JsonObject? = null, val docs: String = "", val fixRoute: String? = null, val since: String = "",
)
@Serializable data class HealthReport(val checkedAt: String? = null, val issues: List<HealthIssue> = emptyList())

@Serializable data class HistoryEntry(
    val id: String = "", val at: String = "", val category: String = "", val type: String = "", val kind: String? = null,
    val itemRef: String = "", val title: String = "", val release: String? = null, val indexer: String? = null,
    val client: String? = null, val message: String = "",
)
@Serializable data class HistoryPage(val items: List<HistoryEntry> = emptyList(), val hasMore: Boolean = false)

@Serializable data class IndexerHealth(val status: String = "unknown", val failures: Int = 0, val lastError: String? = null, val lastErrorCode: String? = null, val lastSuccessAt: String? = null, val retryAt: String? = null)
@Serializable data class Indexer(
    val id: Long = 0, val name: String = "", val kind: String = "", val protocol: String = "", val enabled: Boolean = true,
    val priority: Int = 0, val health: IndexerHealth = IndexerHealth(),
)
@Serializable data class IndexerDefinition(
    val id: String = "", val name: String = "", val description: String = "", val builtin: Boolean = true,
    val protocol: String = "torrent", val language: String? = null, val links: List<String> = emptyList(),
    val flaresolverr: String = "never", val rss: Boolean = false, val indexerIds: List<Long> = emptyList(),
)
@Serializable data class UsenetPreset(val id: String = "", val name: String = "", val siteUrl: String = "")
@Serializable data class IndexerDefinitionError(val file: String = "", val message: String = "")
@Serializable data class IndexerCatalog(
    val definitions: List<IndexerDefinition> = emptyList(), val presets: List<UsenetPreset> = emptyList(),
    val errors: List<IndexerDefinitionError> = emptyList(),
)
@Serializable data class IndexerTestResult(val ok: Boolean = false, val durationMs: Long = 0, val errorCode: String? = null, val error: String? = null)

@Serializable data class DownloadClientCategories(val movies: String = "", val series: String = "", val music: String = "")
@Serializable data class DownloadClient(
    val id: Long = 0, val name: String = "", val implementation: String = "", val protocol: String = "", val enabled: Boolean = true,
    val priority: Int = 1, val host: String = "", val port: Int? = null, val useTls: Boolean = false, val allowSelfSigned: Boolean = false,
    val urlBase: String = "", val username: String = "", val directory: String = "",
    val categories: DownloadClientCategories = DownloadClientCategories(), val status: DownloadClientStatus = DownloadClientStatus(),
)
@Serializable data class DownloadClientInput(
    val name: String, val implementation: String, val enabled: Boolean, val priority: Int, val host: String, val port: Int? = null,
    val useTls: Boolean, val allowSelfSigned: Boolean, val urlBase: String, val username: String, val directory: String,
    val categories: DownloadClientCategories,
)
@Serializable data class DownloadClientTestRequest(val id: Long, val client: DownloadClientInput)
@Serializable data class DownloadClientTestResult(val ok: Boolean = false, val errorCode: String? = null, val message: String? = null, val version: String? = null)
