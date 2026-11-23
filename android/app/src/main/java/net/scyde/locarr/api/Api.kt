package net.scyde.locarr.api

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.KSerializer
import kotlinx.serialization.json.Json
import kotlinx.serialization.serializer
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import java.util.concurrent.TimeUnit

/** An error answered by the server, with its stable machine-readable code. */
class ApiException(val status: Int, val code: String, message: String) : Exception(message)

val json = Json { ignoreUnknownKeys = true; explicitNulls = false; encodeDefaults = true; coerceInputValues = true; isLenient = true }

private val JSON_TYPE = "application/json".toMediaType()
private val EMPTY = "{}".toRequestBody(JSON_TYPE)

/** Codes that mean the session token itself is no longer accepted (not a wrong password). */
fun isSessionRejected(e: ApiException) = e.status == 401 && e.code !in setOf("invalid_credentials", "totp_required", "invalid_totp", "invalid_recovery_code")

/**
 * Normalises what the user typed as the server address: adds http:// when no scheme is
 * given (LAN servers usually have no TLS) and drops a trailing slash or a pasted /api/v1.
 */
fun normalizeServerUrl(input: String): String? {
    var s = input.trim()
    if (s.isEmpty()) return null
    if (!s.startsWith("http://", true) && !s.startsWith("https://", true)) s = "http://$s"
    s = s.trimEnd('/').removeSuffix("/api/v1").trimEnd('/')
    return runCatching { s.toHttpUrl() }.getOrNull()?.toString()?.trimEnd('/')
}

/** Talks to one Locarr server with the Bearer session token from sign-in. */
class Api(baseUrl: String, @Volatile var token: String? = null, client: OkHttpClient? = null) {
    val base: HttpUrl = baseUrl.trimEnd('/').toHttpUrl()

    /** Called (on a background thread) when the server stops accepting the token. */
    @Volatile var onUnauthorized: (() -> Unit)? = null

    val http: OkHttpClient = (client?.newBuilder() ?: OkHttpClient.Builder())
        .connectTimeout(10, TimeUnit.SECONDS)
        .readTimeout(60, TimeUnit.SECONDS)
        .addInterceptor { chain ->
            val t = token
            val req = if (t != null && chain.request().url.host == base.host && chain.request().url.port == base.port) {
                chain.request().newBuilder().header("Authorization", "Bearer $t").build()
            } else chain.request()
            chain.proceed(req)
        }
        .build()

    fun url(path: String, query: Map<String, Any?> = emptyMap()): HttpUrl {
        val b = base.newBuilder()
        path.trimStart('/').split('/').forEach { if (it.isNotEmpty()) b.addPathSegment(it) }
        query.forEach { (k, v) -> if (v != null) b.addQueryParameter(k, v.toString()) }
        return b.build()
    }

    private fun api(path: String, query: Map<String, Any?> = emptyMap()) = url("api/v1/$path", query)

    /** A web page of this server (settings that are edited in the browser). */
    fun webUrl(route: String) = base.toString().trimEnd('/') + "/" + route.trimStart('/')

    /** TMDb poster/backdrop or Cover Art Archive cover through the server's image cache. */
    fun imageUrl(file: String?, music: Boolean = false, wide: Boolean = false): String? {
        if (file.isNullOrBlank()) return null
        if (file.startsWith("http")) return file
        val b = base.newBuilder().addPathSegments("api/v1/images")
        if (music) b.addPathSegment("coverart").addPathSegment("500") else b.addPathSegment("tmdb").addPathSegment(if (wide) "w780" else "w342")
        return b.addPathSegment(file.trimStart('/')).build().toString()
    }

    suspend fun <T> call(method: String, path: String, body: RequestBody?, query: Map<String, Any?>, out: KSerializer<T>?): T? =
        withContext(Dispatchers.IO) {
            val req = Request.Builder().url(api(path, query)).method(method, body).header("Accept", "application/json").build()
            http.newCall(req).execute().use { resp ->
                val text = resp.body?.string().orEmpty()
                if (!resp.isSuccessful) {
                    val err = runCatching { json.decodeFromString(ApiErrorBody.serializer(), text) }.getOrNull()
                    val e = ApiException(resp.code, err?.code ?: "http_${resp.code}", err?.message?.ifBlank { null } ?: "HTTP ${resp.code}")
                    if (isSessionRejected(e) && token != null) onUnauthorized?.invoke()
                    throw e
                }
                if (out == null || text.isBlank()) null else json.decodeFromString(out, text)
            }
        }

    inline fun <reified B> jsonBody(b: B): RequestBody = json.encodeToString(serializer<B>(), b).toRequestBody("application/json".toMediaType())

    suspend inline fun <reified T> get(path: String, query: Map<String, Any?> = emptyMap()): T = call("GET", path, null, query, serializer<T>())!!
    suspend inline fun <reified B, reified T> post(path: String, body: B): T = call("POST", path, jsonBody(body), emptyMap(), serializer<T>())!!
    suspend inline fun <reified B> send(method: String, path: String, body: B, query: Map<String, Any?> = emptyMap()) {
        call(method, path, jsonBody(body), query, null as KSerializer<Unit>?)
    }
    suspend fun send(method: String, path: String, query: Map<String, Any?> = emptyMap()) {
        call(method, path, if (method == "GET" || method == "DELETE") null else EMPTY, query, null as KSerializer<Unit>?)
    }

    // System and sign-in
    suspend fun info(): SystemInfo = get("system/info")
    suspend fun providers(): List<AuthProvider> = get("auth/providers")
    suspend fun login(username: String, password: String, totp: String?, recovery: String?, device: DeviceInfo): AuthResult =
        post("auth/login", LoginRequest(username, password, totp?.ifBlank { null }, recovery?.ifBlank { null }, device))
    suspend fun providerLogin(provider: String, username: String, password: String, totp: String?, device: DeviceInfo): AuthResult =
        post("auth/providers/$provider/login", ProviderLoginRequest(username, password, totp?.ifBlank { null }, device))
    suspend fun me(): User = get("auth/me")
    suspend fun logout() = send("POST", "auth/logout")

    // Notifications
    suspend fun notifications(since: Long? = null, limit: Int = 50): NotificationList = get("notifications", mapOf("since" to since, "limit" to limit))
    suspend fun markRead(ids: List<Long>? = null, all: Boolean = false): UnreadCount =
        post("notifications/read", MarkReadRequest(ids, if (all) true else null))

    // Discover
    suspend fun discover(): DiscoverHome = get("discover")
    suspend fun discoverList(key: String, page: Int): DiscoverPage = get("discover/lists/$key", mapOf("page" to page))
    suspend fun discoverSearch(q: String, type: String = "all"): DiscoverPage = get("discover/search", mapOf("q" to q, "type" to type))
    suspend fun movieTitle(tmdbId: Long): TitleDetail = get("discover/movies/$tmdbId")
    suspend fun seriesTitle(tmdbId: Long): TitleDetail = get("discover/series/$tmdbId")
    suspend fun artistTitle(mbid: String): TitleDetail = get("discover/artists/$mbid")
    suspend fun person(tmdbId: Long): PersonDetail = get("discover/people/$tmdbId")

    // Requests and issues
    suspend fun createRequest(r: CreateRequest): MediaRequest = post("requests", r)
    suspend fun requests(status: String, mine: Boolean, offset: Int, limit: Int = 30): RequestList =
        get("requests", mapOf("status" to status, "mine" to mine, "offset" to offset, "limit" to limit))
    suspend fun request(id: Long): MediaRequest = get("requests/$id")
    suspend fun requestSummary(): RequestSummary = get("requests/summary")
    suspend fun approve(id: Long): MediaRequest = post("requests/$id/approve", Empty())
    suspend fun approveAll(ids: List<Long>): BulkResult = post("requests/approve", RequestIds(ids))
    suspend fun decline(id: Long, reason: String?): MediaRequest = post("requests/$id/decline", DeclineRequest(reason?.ifBlank { null }))
    suspend fun cancelRequest(id: Long): MediaRequest = post("requests/$id/cancel", Empty())
    suspend fun retryRequest(id: Long): MediaRequest = post("requests/$id/retry", Empty())
    suspend fun requestComments(id: Long): List<Comment> = get("requests/$id/comments")
    suspend fun commentRequest(id: Long, body: String): Comment = post("requests/$id/comments", CommentInput(body))
    suspend fun reportIssue(i: IssueInput): Issue = post("issues", i)
    suspend fun issues(status: String?, offset: Int, limit: Int = 30): IssueList = get("issues", mapOf("status" to status, "offset" to offset, "limit" to limit))
    suspend fun issue(id: Long): Issue = get("issues/$id")
    suspend fun issueComments(id: Long): List<Comment> = get("issues/$id/comments")
    suspend fun commentIssue(id: Long, body: String): Comment = post("issues/$id/comments", CommentInput(body))
    suspend fun resolveIssue(id: Long, searchReplacement: Boolean) = send("POST", "issues/$id/resolve", mapOf("searchReplacement" to searchReplacement))
    suspend fun reopenIssue(id: Long) = send("POST", "issues/$id/reopen")

    // Library
    suspend fun movies(q: String?, status: String?, offset: Int, limit: Int = 50): MovieList =
        get("movies", mapOf("q" to q?.ifBlank { null }, "status" to status, "sort" to "title", "offset" to offset, "limit" to limit))
    suspend fun movie(id: Long): Movie = get("movies/$id")
    suspend fun updateMovie(id: Long, u: MovieUpdate): Movie = call("PATCH", "movies/$id", jsonBody(u), emptyMap(), Movie.serializer())!!
    suspend fun searchMovie(id: Long) = send("POST", "movies/$id/search")
    suspend fun series(q: String?, missing: Boolean?, offset: Int, limit: Int = 50): SeriesList =
        get("series", mapOf("q" to q?.ifBlank { null }, "missing" to missing, "sort" to "title", "offset" to offset, "limit" to limit))
    suspend fun seriesDetail(id: Long): Series = get("series/$id")
    suspend fun updateSeries(id: Long, u: SeriesUpdate) { call("PATCH", "series/$id", jsonBody(u), emptyMap(), null as KSerializer<Unit>?) }
    suspend fun episodes(seriesId: Long): List<Episode> = get("series/$seriesId/episodes")
    suspend fun searchSeries(id: Long) = send("POST", "series/$id/search")
    suspend fun searchSeason(id: Long, season: Int) = send("POST", "series/$id/seasons/$season/search")
    suspend fun searchEpisodes(ids: List<Long>) = send("POST", "episodes/search", mapOf("episodeIds" to ids))
    suspend fun monitorEpisodes(ids: List<Long>, monitored: Boolean) { post<MonitorEpisodesRequest, QueuedCount>("episodes/monitor", MonitorEpisodesRequest(ids, monitored)) }
    suspend fun wantedEpisodes(kind: String, offset: Int, limit: Int = 50): WantedEpisodeList = get("episodes/wanted", mapOf("kind" to kind, "offset" to offset, "limit" to limit))
    suspend fun artists(q: String?, offset: Int, limit: Int = 50): ArtistList = get("music/artists", mapOf("q" to q?.ifBlank { null }, "sort" to "name", "offset" to offset, "limit" to limit))
    suspend fun artist(id: Long): Artist = get("music/artists/$id")
    suspend fun artistAlbums(id: Long): List<Album> = get("music/artists/$id/albums")
    suspend fun album(id: Long): AlbumDetail = get("music/albums/$id")
    suspend fun searchAlbum(id: Long) = send("POST", "music/albums/$id/search")
    suspend fun searchArtist(id: Long) = send("POST", "music/artists/$id/search")
    suspend fun monitorAlbums(ids: List<Long>, monitored: Boolean) { call("PUT", "music/albums/monitor", jsonBody(AlbumMonitorRequest(ids, monitored)), emptyMap(), null as KSerializer<Unit>?) }

    // Adding
    suspend fun lookupMovies(term: String): List<MovieLookup> = get("movies/lookup", mapOf("term" to term))
    suspend fun lookupSeries(term: String): List<SeriesLookup> = get("series/lookup", mapOf("term" to term))
    suspend fun lookupArtists(term: String): List<ArtistLookup> = get("music/artists/lookup", mapOf("term" to term))
    suspend fun addMovie(r: AddMovieRequest): Movie = post("movies", r)
    suspend fun addSeries(r: AddSeriesRequest): Series = post("series", r)
    suspend fun addArtist(r: AddArtistRequest): Artist = post("music/artists", r)
    suspend fun qualityProfiles(): List<QualityProfile> = get("qualityprofiles")
    suspend fun metadataProfiles(): List<MetadataProfile> = get("music/metadataprofiles")
    suspend fun rootFolders(): List<RootFolder> = get("rootfolders")

    // Timeline and interactive search
    suspend fun timeline(ref: String, limit: Int = 100): Timeline = get("timeline", mapOf("item" to ref, "limit" to limit))
    suspend fun retryNow(ref: String): QueuedCount = call("POST", "timeline/retry", EMPTY, mapOf("item" to ref), QueuedCount.serializer()) ?: QueuedCount()
    suspend fun releases(target: ReleaseTarget): InteractiveSearch = get(target.path)
    suspend fun grab(target: ReleaseTarget, d: InteractiveDecision) {
        val c = d.decision.candidate
        send("POST", target.path, ManualGrabRequest(c.indexerId ?: 0, d.downloadToken, d.magnetUrl, c.title, c.protocol ?: "torrent", c.guid, c.publishDate))
    }

    // Activity
    suspend fun queue(): Queue = get("queue")
    suspend fun pause(id: Long) = send("POST", "queue/$id/pause")
    suspend fun resume(id: Long) = send("POST", "queue/$id/resume")
    suspend fun priority(id: Long, p: String) = send("POST", "queue/$id/priority", PriorityRequest(p))
    suspend fun removeDownload(id: Long, deleteData: Boolean, blocklist: Boolean) =
        send("DELETE", "queue/$id", mapOf("deleteData" to deleteData, "blocklist" to blocklist))
    suspend fun calendar(start: String, end: String): List<CalendarEntry> = get("calendar", mapOf("start" to start, "end" to end))
    suspend fun board(): BoardSummary = get("board")
    suspend fun boardItems(code: String?, offset: Int, limit: Int = 50): BoardItemPage = get("board/items", mapOf("code" to code, "offset" to offset, "limit" to limit))
    suspend fun boardAction(action: String, keys: List<String>): BoardActionResult = post("board/actions", BoardActionRequest(action, keys))
    suspend fun health(): HealthReport = get("system/health")
    suspend fun runHealth(): HealthReport = post("system/health/run", Empty())
    suspend fun history(category: String?, offset: Int, limit: Int = 50): HistoryPage = get("history", mapOf("category" to category, "offset" to offset, "limit" to limit))
    suspend fun indexers(): List<Indexer> = get("indexers")
    suspend fun indexerCatalog(): IndexerCatalog = get("indexers/definitions")
    suspend fun testIndexer(id: Long): IndexerTestResult = post("indexers/$id/test", Empty())
    suspend fun downloadClients(): List<DownloadClient> = get("downloadclients")
    suspend fun testDownloadClient(c: DownloadClient): DownloadClientTestResult = post("downloadclients/test", DownloadClientTestRequest(c.id, DownloadClientInput(
        c.name, c.implementation, c.enabled, c.priority.coerceIn(1, 50), c.host, c.port, c.useTls, c.allowSelfSigned, c.urlBase, c.username, c.directory, c.categories,
    )))
}

/** Where an interactive search runs: a movie, an episode, a season or an album. */
sealed class ReleaseTarget(val path: String) {
    class Movie(id: Long) : ReleaseTarget("movies/$id/releases")
    class Episode(id: Long) : ReleaseTarget("episodes/$id/releases")
    class Season(seriesId: Long, number: Int) : ReleaseTarget("series/$seriesId/seasons/$number/releases")
    class Album(id: Long) : ReleaseTarget("music/albums/$id/releases")
}
