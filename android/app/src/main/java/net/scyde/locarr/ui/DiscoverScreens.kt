package net.scyde.locarr.ui

import android.content.Context
import android.net.Uri
import androidx.browser.customtabs.CustomTabsIntent
import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.OpenInNew
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Flag
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.filled.VideoLibrary
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import net.scyde.locarr.R
import net.scyde.locarr.api.CreateRequest
import net.scyde.locarr.api.DiscoverItem
import net.scyde.locarr.api.IssueInput
import net.scyde.locarr.api.MediaRequest
import net.scyde.locarr.api.TitleDetail

fun openUrl(ctx: Context, url: String) {
    runCatching { CustomTabsIntent.Builder().setShowTitle(true).build().launchUrl(ctx, Uri.parse(url)) }
}

/** Where tapping a discover item goes. */
fun routeOf(i: DiscoverItem): String? = when (i.mediaType) {
    "movie" -> i.tmdbId?.let { "title/movie/$it" }
    "series" -> i.tmdbId?.let { "title/series/$it" }
    "person" -> i.tmdbId?.let { "person/$it" }
    "artist" -> i.mbid?.let { "title/artist/$it" }
    "album" -> i.artistMbid?.let { "title/artist/$it" }
    else -> null
}

/** A library reference ("movie:12") as an app route, for managers. */
fun libraryRoute(ref: String?): String? {
    val (kind, id) = ref?.split(':', limit = 2)?.takeIf { it.size == 2 } ?: return null
    if (id.toLongOrNull() == null) return null
    return when (kind) { "movie" -> "movie/$id"; "series" -> "series/$id"; "artist" -> "artist/$id"; "album" -> "album/$id"; else -> null }
}

@Composable
fun discoverStatusLabel(s: String): String? = when (s) {
    "pending" -> stringResource(R.string.dstatus_pending)
    "processing" -> stringResource(R.string.dstatus_processing)
    "partial" -> stringResource(R.string.dstatus_partial)
    "available" -> stringResource(R.string.dstatus_available)
    "in_library" -> stringResource(R.string.dstatus_in_library)
    else -> null
}

fun discoverStatusTone(s: String) = when (s) { "available" -> Tone.Success; "partial", "processing" -> Tone.Info; "pending" -> Tone.Warning; "in_library" -> Tone.Neutral; else -> Tone.Neutral }

@Composable
fun rowLabel(key: String): String = when (key) {
    "trending" -> stringResource(R.string.row_trending)
    "trending_movies" -> stringResource(R.string.row_trending_movies)
    "trending_series" -> stringResource(R.string.row_trending_series)
    "popular_movies" -> stringResource(R.string.row_popular_movies)
    "popular_series" -> stringResource(R.string.row_popular_series)
    "upcoming_movies" -> stringResource(R.string.row_upcoming_movies)
    "now_playing" -> stringResource(R.string.row_now_playing)
    "top_rated_movies" -> stringResource(R.string.row_top_rated_movies)
    "top_rated_series" -> stringResource(R.string.row_top_rated_series)
    "airing_series" -> stringResource(R.string.row_airing_series)
    "recently_added" -> stringResource(R.string.row_recently_added)
    else -> key.replace('_', ' ').replaceFirstChar { it.uppercase() }
}

@Composable
fun PosterCard(item: DiscoverItem, width: androidx.compose.ui.unit.Dp?, onClick: () -> Unit) {
    val a = api()
    val label = discoverStatusLabel(item.status)
    Column(
        (if (width != null) Modifier.width(width) else Modifier.fillMaxWidth()).clip(SmallShape).clickable(onClick = onClick)
            .semantics(mergeDescendants = true) {},
    ) {
        Box {
            Poster(a.imageUrl(item.poster, music = item.isMusic), item.title, Modifier.fillMaxWidth(), square = item.isMusic)
            if (label != null) Pill(label, discoverStatusTone(item.status), Modifier.align(Alignment.TopEnd).padding(6.dp))
        }
        Spacer(Modifier.height(6.dp))
        Text(item.title, color = Lc.text, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Text(listOfNotNull(item.subtitle, item.year.takeIf { it > 0 }?.toString()).joinToString(" · "), color = Lc.faint, style = MaterialTheme.typography.bodySmall, maxLines = 1)
    }
}

@Composable
fun DiscoverScreen() {
    val a = api()
    val nav = LocalNav.current
    val load = rememberLoad { a.discover() }
    val cardW = if (isWide()) 150.dp else 118.dp
    Screen(stringResource(R.string.tab_discover), back = false, actions = {
        IconButton(onClick = { nav.navigate("discover/search") }) { Icon(Icons.Default.Search, stringResource(R.string.search)) }
    }) {
        Loaded(load) { home ->
            if (!home.metadataReady && home.rows.isEmpty()) { EmptyBox(stringResource(R.string.discover_not_ready), detail = stringResource(R.string.discover_not_ready_hint)); return@Loaded }
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                val hero = home.rows.firstOrNull()?.items?.firstOrNull { !it.backdrop.isNullOrBlank() }
                if (hero != null) item { Hero(hero) { routeOf(hero)?.let(nav::navigate) } }
                if (home.requests.isNotEmpty()) {
                    item { SectionTitle(stringResource(R.string.discover_your_requests), action = { TextButton(onClick = { nav.navigate("requests") }) { Text(stringResource(R.string.see_all)) } }) }
                    item {
                        LazyRow(contentPadding = PaddingValues(horizontal = Edge), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                            items(home.requests, key = { it.id }) { r -> RequestChipCard(r) { nav.navigate("request/${r.id}") } }
                        }
                    }
                }
                home.rows.filter { it.items.isNotEmpty() }.forEach { row ->
                    item(key = "h-" + row.key) {
                        SectionTitle(rowLabel(row.key), Modifier.padding(top = 8.dp), action = {
                            TextButton(onClick = { nav.navigate("discover/list/${row.key}") }) { Text(stringResource(R.string.see_all)) }
                        })
                    }
                    item(key = "r-" + row.key) {
                        LazyRow(contentPadding = PaddingValues(horizontal = Edge), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                            items(row.items, key = { it.key }) { i -> PosterCard(i, cardW) { routeOf(i)?.let(nav::navigate) } }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun Hero(item: DiscoverItem, onClick: () -> Unit) {
    val a = api()
    Box(Modifier.fillMaxWidth().padding(horizontal = Edge).padding(bottom = 8.dp).aspectRatio(if (isWide()) 2.6f else 16f / 10f).clip(Shape).clickable(onClick = onClick)) {
        RemoteImage(a.imageUrl(item.backdrop, wide = true), Modifier.fillMaxSize())
        Box(Modifier.fillMaxSize().background(Brush.verticalGradient(listOf(Color.Transparent, Lc.bg.copy(alpha = 0.55f), Lc.bg.copy(alpha = 0.95f)))))
        Column(Modifier.align(Alignment.BottomStart).padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(stringResource(R.string.discover_featured).uppercase(), style = Eyebrow, color = Lc.accent)
            Text(item.title, style = MaterialTheme.typography.headlineSmall, color = Lc.text, maxLines = 2, overflow = TextOverflow.Ellipsis)
            item.overview?.takeIf { it.isNotBlank() }?.let { Text(it, color = Lc.muted, style = MaterialTheme.typography.bodySmall, maxLines = 2, overflow = TextOverflow.Ellipsis) }
            discoverStatusLabel(item.status)?.let { Pill(it, discoverStatusTone(item.status)) }
        }
    }
}

@Composable
fun requestStatusLabel(s: String): String = codeText(LocalContext.current, "requests.status.$s", fallback = s)

fun requestStatusTone(s: String) = when (s) {
    "available" -> Tone.Success; "processing", "approved", "partial" -> Tone.Info; "pending" -> Tone.Warning
    "declined", "failed" -> Tone.Danger; else -> Tone.Neutral
}

@Composable
private fun RequestChipCard(r: MediaRequest, onClick: () -> Unit) {
    val a = api()
    Row(Modifier.width(260.dp).clip(Shape).background(Lc.surface).clickable(onClick = onClick).padding(10.dp), verticalAlignment = Alignment.CenterVertically) {
        Poster(a.imageUrl(r.poster, music = r.kind == "album" || r.kind == "artist"), r.title, Modifier.width(44.dp), square = r.kind == "album" || r.kind == "artist")
        Spacer(Modifier.width(10.dp))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(r.title, color = Lc.text, style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Pill(requestStatusLabel(r.status), requestStatusTone(r.status))
            r.progress?.takeIf { r.status == "processing" && it.percent > 0 }?.let { ProgressBar(it.percent.toFloat()) }
        }
    }
}

@Composable
fun DiscoverListScreen(key: String) {
    val a = api()
    val nav = LocalNav.current
    val pager = rememberPager(key, pageSize = 20) { offset ->
        val page = offset / 20 + 1
        val res = a.discoverList(key, page)
        res.items to (res.page < res.totalPages)
    }
    Screen(rowLabel(key)) { PosterGrid(pager) { routeOf(it)?.let(nav::navigate) } }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PosterGrid(pager: Pager<DiscoverItem>, header: (@Composable () -> Unit)? = null, onClick: (DiscoverItem) -> Unit) {
    val grid = rememberLazyGridState()
    val near by remember { derivedStateOf { (grid.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0) >= grid.layoutInfo.totalItemsCount - 8 } }
    LaunchedEffect(near, pager.items.size) { if (near && pager.started) pager.loadMore() }
    androidx.compose.material3.pulltorefresh.PullToRefreshBox(pager.refreshing, { pager.refresh() }, Modifier.fillMaxSize()) {
        LazyVerticalGrid(GridCells.Adaptive(if (isWide()) 140.dp else 108.dp), state = grid, contentPadding = PaddingValues(Edge), horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
            if (header != null) item(span = { GridItemSpan(maxLineSpan) }) { header() }
            items(pager.items, key = { it.key }) { PosterCard(it, null) { onClick(it) } }
            item(span = { GridItemSpan(maxLineSpan) }) {
                when {
                    pager.error != null -> ErrorBox(pager.error!!, { pager.error = null; if (pager.items.isEmpty()) pager.refresh() else pager.loadMore() })
                    pager.loading -> Loading()
                    pager.started && pager.items.isEmpty() -> EmptyBox(stringResource(R.string.nothing_found))
                }
            }
        }
    }
}

@Composable
fun SearchScreen(tab: Boolean) {
    val a = api()
    val nav = LocalNav.current
    var q by rememberSaveable { mutableStateOf("") }
    var type by rememberSaveable { mutableStateOf("all") }
    var debounced by remember { mutableStateOf(q) }
    LaunchedEffect(q) { delay(400); debounced = q.trim() }
    val pager = rememberPager(debounced, type, pageSize = 1000) { _ ->
        if (debounced.length < 2) emptyList<DiscoverItem>() to false else a.discoverSearch(debounced, type).items to false
    }
    Screen(stringResource(R.string.search), back = !tab) {
        Column {
            SearchField(q, { q = it }, stringResource(R.string.search_hint))
            FilterChips(listOf("all" to stringResource(R.string.filter_all), "movie" to stringResource(R.string.kind_movies), "series" to stringResource(R.string.kind_series),
                "person" to stringResource(R.string.kind_people), "music" to stringResource(R.string.kind_music)), type, { type = it })
            if (debounced.length < 2) EmptyBox(stringResource(R.string.search_start), detail = stringResource(R.string.search_start_hint))
            else PosterGrid(pager) { routeOf(it)?.let(nav::navigate) }
        }
    }
}

@Composable
fun PersonScreen(id: Long) {
    val a = api()
    val nav = LocalNav.current
    val load = rememberLoad(id) { a.person(id) }
    Screen(load.data?.name ?: "") {
        Loaded(load) { p ->
            LazyVerticalGrid(GridCells.Adaptive(if (isWide()) 140.dp else 108.dp), contentPadding = PaddingValues(Edge), horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
                item(span = { GridItemSpan(maxLineSpan) }) {
                    Row(horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                        Poster(a.imageUrl(p.profile), p.name, Modifier.width(110.dp))
                        Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                            Text(p.name, style = MaterialTheme.typography.headlineSmall)
                            p.department?.let { Text(it, color = Lc.muted) }
                            p.birthday?.let { Text(formatDate(it), color = Lc.faint, style = MaterialTheme.typography.bodySmall) }
                        }
                    }
                }
                p.biography?.takeIf { it.isNotBlank() }?.let { bio -> item(span = { GridItemSpan(maxLineSpan) }) { ExpandableText(bio) } }
                item(span = { GridItemSpan(maxLineSpan) }) { Text(stringResource(R.string.known_for), style = MaterialTheme.typography.titleMedium) }
                items(p.knownFor, key = { it.key }) { PosterCard(it, null) { routeOf(it)?.let(nav::navigate) } }
            }
        }
    }
}

@Composable
fun ExpandableText(text: String, lines: Int = 4) {
    var open by remember { mutableStateOf(false) }
    Text(text, color = Lc.muted, style = MaterialTheme.typography.bodyMedium, maxLines = if (open) Int.MAX_VALUE else lines, overflow = TextOverflow.Ellipsis,
        modifier = Modifier.clickable { open = !open }.animateContentSizeCompat())
}

@Composable
fun Modifier.animateContentSizeCompat(): Modifier = this.then(Modifier.animateContentSize())

/** A title page: movie, series or artist, with requesting, 4K, reporting a problem and Streamero. */
@Composable
fun TitleScreen(kind: String, id: String) {
    val a = api()
    val load = rememberLoad(kind, id) {
        when (kind) {
            "series" -> a.seriesTitle(id.toLong())
            "artist" -> a.artistTitle(id)
            else -> a.movieTitle(id.toLong())
        }
    }
    Screen(load.data?.item?.title ?: "") {
        Loaded(load) { d -> TitleBody(kind, d) { load.reload() } }
    }
}

@Composable
private fun TitleBody(kind: String, d: TitleDetail, reload: () -> Unit) {
    val a = api()
    val nav = LocalNav.current
    val ctx = LocalContext.current
    val me = LocalState.current.me
    val music = kind == "artist"
    var requesting by remember { mutableStateOf<Boolean?>(null) } // false = normal, true = 4K
    var albumRequest by remember { mutableStateOf<DiscoverItem?>(null) }
    var reporting by remember { mutableStateOf(false) }
    val i = d.item
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
        Box(Modifier.fillMaxWidth().aspectRatio(if (isWide()) 2.8f else 16f / 9f)) {
            RemoteImage(a.imageUrl(i.backdrop ?: i.poster, music = music, wide = true), Modifier.fillMaxSize())
            Box(Modifier.fillMaxSize().background(Brush.verticalGradient(listOf(Color.Transparent, Lc.bg))))
        }
        Readable {
            Column(Modifier.padding(horizontal = Edge).offset(y = (-48).dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
                Row(horizontalArrangement = Arrangement.spacedBy(14.dp), verticalAlignment = Alignment.Bottom) {
                    Poster(a.imageUrl(i.poster, music = music), i.title, Modifier.width(if (isWide()) 150.dp else 112.dp), square = music)
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        Text(i.title, style = MaterialTheme.typography.headlineSmall, color = Lc.text)
                        val meta = listOfNotNull(i.year.takeIf { it > 0 }?.toString(), d.runtime?.takeIf { it > 0 }?.let { stringResource(R.string.minutes, it) },
                            d.certification?.ifBlank { null }, d.network ?: d.studio).joinToString(" · ")
                        if (meta.isNotBlank()) Text(meta, color = Lc.muted, style = MaterialTheme.typography.bodySmall)
                        i.rating?.takeIf { it > 0 }?.let {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Icon(Icons.Default.Star, null, tint = Lc.warning, modifier = Modifier.size(14.dp))
                                Text(" %.1f".format(it), color = Lc.muted, style = MaterialTheme.typography.bodySmall)
                            }
                        }
                    }
                }
                Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    discoverStatusLabel(i.status)?.let { Pill(it, discoverStatusTone(i.status)) }
                    discoverStatusLabel(i.status4k)?.let { Pill("4K · $it", discoverStatusTone(i.status4k)) }
                }
                // Actions
                FlowRowCompat {
                    val canAsk = d.ability.canRequest && i.status in setOf("none", "partial") && !music
                    if (canAsk) Button(onClick = { requesting = false }, shape = Shape) { Icon(Icons.Default.Add, null); Spacer(Modifier.width(6.dp)); Text(stringResource(R.string.request)) }
                    if (d.ability.canRequest4k && i.status4k in setOf("none", "partial") && !music) OutlinedButton(onClick = { requesting = true }, shape = Shape) { Text(stringResource(R.string.request_4k)) }
                    i.streameroUrl?.let { url -> OutlinedButton(onClick = { openUrl(ctx, url) }, shape = Shape) { Icon(Icons.Default.PlayArrow, null); Spacer(Modifier.width(6.dp)); Text(stringResource(R.string.watch_streamero)) } }
                    if (d.ability.canReportIssue) OutlinedButton(onClick = { reporting = true }, shape = Shape) { Icon(Icons.Default.Flag, null); Spacer(Modifier.width(6.dp)); Text(stringResource(R.string.report_issue)) }
                    if (me?.canManageLibrary == true) libraryRoute(d.libraryRef)?.let { r -> TextButton(onClick = { nav.navigate(r) }) { Icon(Icons.Default.VideoLibrary, null); Spacer(Modifier.width(6.dp)); Text(stringResource(R.string.open_in_library)) } }
                    d.trailerUrl?.let { t -> TextButton(onClick = { openUrl(ctx, t) }) { Icon(Icons.AutoMirrored.Filled.OpenInNew, null); Spacer(Modifier.width(6.dp)); Text(stringResource(R.string.trailer)) } }
                }
                d.tagline?.takeIf { it.isNotBlank() }?.let { Text(it, color = Lc.text, style = MaterialTheme.typography.titleSmall) }
                i.overview?.takeIf { it.isNotBlank() }?.let { ExpandableText(it) }
                if (d.genres.isNotEmpty()) Text(d.genres.joinToString(" · "), color = Lc.faint, style = MaterialTheme.typography.bodySmall)

                d.requests.forEach { r ->
                    Panel(Modifier.clickable { nav.navigate("request/${r.id}") }, tone = requestStatusTone(r.status)) {
                        Text(stringResource(R.string.request_by, r.requestedBy.username, formatDate(r.createdAt)) + if (r.is4k) " · 4K" else "", color = Lc.muted, style = MaterialTheme.typography.bodySmall)
                        Text(requestStatusLabel(r.status), color = Lc.text, style = MaterialTheme.typography.titleSmall)
                        r.explanation?.let { Text(explanationText(ctx, it), color = Lc.muted, style = MaterialTheme.typography.bodySmall) }
                        r.progress?.takeIf { it.percent > 0 && r.status != "available" }?.let { Spacer(Modifier.height(6.dp)); ProgressBar(it.percent.toFloat()) }
                    }
                }

                if (d.seasons.isNotEmpty()) {
                    Text(stringResource(R.string.seasons), style = MaterialTheme.typography.titleMedium)
                    d.seasons.forEach { s ->
                        Row(Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                            Column(Modifier.weight(1f)) {
                                Text(s.title.ifBlank { stringResource(R.string.season_n, s.number) }, color = Lc.text)
                                Text(listOfNotNull(stringResource(R.string.episodes_n, s.episodeCount), s.airDate?.take(4)).joinToString(" · "), color = Lc.faint, style = MaterialTheme.typography.bodySmall)
                            }
                            discoverStatusLabel(s.status)?.let { Pill(it, discoverStatusTone(s.status)) }
                        }
                    }
                }
                if (d.albums.isNotEmpty()) {
                    Text(stringResource(R.string.albums), style = MaterialTheme.typography.titleMedium)
                    d.albums.forEach { al ->
                        Row(Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                            Poster(a.imageUrl(al.poster, music = true), al.title, Modifier.width(48.dp), square = true)
                            Spacer(Modifier.width(12.dp))
                            Column(Modifier.weight(1f)) {
                                Text(al.title, color = Lc.text, maxLines = 2, overflow = TextOverflow.Ellipsis)
                                Text(listOfNotNull(al.subtitle, al.year.takeIf { it > 0 }?.toString()).joinToString(" · "), color = Lc.faint, style = MaterialTheme.typography.bodySmall)
                            }
                            val st = discoverStatusLabel(al.status)
                            if (st != null) Pill(st, discoverStatusTone(al.status))
                            else if (d.ability.canRequest) TextButton(onClick = { albumRequest = al }) { Text(stringResource(R.string.request)) }
                        }
                    }
                }
                if (d.cast.isNotEmpty()) {
                    Text(stringResource(R.string.cast), style = MaterialTheme.typography.titleMedium)
                    LazyRow(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        items(d.cast.take(30)) { c ->
                            Column(Modifier.width(84.dp).clickable(enabled = c.personId != null) { nav.navigate("person/${c.personId}") }, horizontalAlignment = Alignment.CenterHorizontally) {
                                RemoteImage(a.imageUrl(c.profile), Modifier.size(64.dp).clip(CircleShape))
                                Text(c.name, style = MaterialTheme.typography.labelMedium, maxLines = 2, overflow = TextOverflow.Ellipsis)
                                c.character?.let { Text(it, color = Lc.faint, style = MaterialTheme.typography.labelSmall, maxLines = 1, overflow = TextOverflow.Ellipsis) }
                            }
                        }
                    }
                }
            }
        }
    }
    requesting?.let { fourK ->
        RequestSheet(kind, d, fourK, onDismiss = { requesting = null }, onDone = { requesting = null; reload() })
    }
    albumRequest?.let { al ->
        RequestAlbumDialog(al, onDismiss = { albumRequest = null }, onDone = { albumRequest = null; reload() })
    }
    if (reporting) ReportIssueDialog(kind, d, onDismiss = { reporting = false })
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun FlowRowCompat(content: @Composable () -> Unit) {
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) { content() }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun RequestSheet(kind: String, d: TitleDetail, fourK: Boolean, onDismiss: () -> Unit, onDone: () -> Unit) {
    val a = api()
    val act = rememberActions()
    val series = kind == "series"
    val requestable = d.seasons.filter { (if (fourK) it.status4k else it.status) in setOf("none", "partial") && it.number > 0 }
    var picked by remember { mutableStateOf(requestable.map { it.number }.toSet()) }
    var future by remember { mutableStateOf(true) }
    var comment by remember { mutableStateOf("") }
    val sent = stringResource(R.string.request_sent)
    ModalBottomSheet(onDismissRequest = onDismiss, containerColor = Lc.surface) {
        Column(Modifier.padding(horizontal = 20.dp).padding(bottom = 24.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text(stringResource(R.string.request_title, d.item.title) + if (fourK) " · 4K" else "", style = MaterialTheme.typography.titleLarge)
            if (series) {
                Text(stringResource(R.string.seasons), color = Lc.muted, style = MaterialTheme.typography.labelLarge)
                requestable.forEach { s ->
                    Row(Modifier.fillMaxWidth().clickable { picked = if (s.number in picked) picked - s.number else picked + s.number }, verticalAlignment = Alignment.CenterVertically) {
                        Checkbox(s.number in picked, { picked = if (it) picked + s.number else picked - s.number })
                        Text(s.title.ifBlank { stringResource(R.string.season_n, s.number) }, Modifier.weight(1f))
                        Text(stringResource(R.string.episodes_n, s.episodeCount), color = Lc.faint, style = MaterialTheme.typography.bodySmall)
                    }
                }
                Row(Modifier.fillMaxWidth().clickable { future = !future }, verticalAlignment = Alignment.CenterVertically) {
                    Switch(future, { future = it }); Spacer(Modifier.width(10.dp)); Text(stringResource(R.string.request_future))
                }
            }
            OutlinedTextField(comment, { comment = it }, label = { Text(stringResource(R.string.request_comment)) }, modifier = Modifier.fillMaxWidth(), colors = fieldColors(), minLines = 2)
            Button(
                onClick = {
                    act.run(sent, after = onDone) {
                        val all = series && picked.size == requestable.size
                        a.createRequest(CreateRequest(
                            kind = if (series) "series" else "movie", tmdbId = d.item.tmdbId, is4k = fourK,
                            seasons = if (series && !all) picked.sorted() else null, allSeasons = if (series) all else null,
                            futureSeasons = if (series) future else null, comment = comment.ifBlank { null },
                        ))
                    }
                },
                enabled = !act.busy && (!series || picked.isNotEmpty() || future), modifier = Modifier.fillMaxWidth().heightIn(min = 50.dp), shape = Shape,
            ) { Text(stringResource(R.string.request_send)) }
            if (d.ability.autoApprove && !fourK || d.ability.autoApprove4k && fourK) Text(stringResource(R.string.request_auto_hint), color = Lc.faint, style = MaterialTheme.typography.bodySmall)
        }
    }
}

@Composable
private fun RequestAlbumDialog(al: DiscoverItem, onDismiss: () -> Unit, onDone: () -> Unit) {
    val a = api()
    val act = rememberActions()
    val sent = stringResource(R.string.request_sent)
    ConfirmDialog(stringResource(R.string.request_title, al.title), al.subtitle, stringResource(R.string.request_send), onDismiss) {
        act.run(sent, after = onDone) { a.createRequest(CreateRequest(kind = "album", mbid = al.mbid)) }
    }
}

@Composable
private fun ReportIssueDialog(kind: String, d: TitleDetail, onDismiss: () -> Unit) {
    val a = api()
    val act = rememberActions()
    var type by remember { mutableStateOf("video") }
    var message by remember { mutableStateOf("") }
    var season by remember { mutableStateOf("") }
    var episode by remember { mutableStateOf("") }
    val done = stringResource(R.string.issue_sent)
    AlertDialog(
        onDismissRequest = onDismiss, containerColor = Lc.surface2,
        title = { Text(stringResource(R.string.report_issue)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                listOf("video" to R.string.issue_video, "audio" to R.string.issue_audio, "subtitles" to R.string.issue_subtitles, "other" to R.string.issue_other).forEach { (k, label) ->
                    Row(Modifier.fillMaxWidth().clickable { type = k }, verticalAlignment = Alignment.CenterVertically) {
                        RadioButton(type == k, { type = k }); Text(stringResource(label))
                    }
                }
                if (kind == "series") Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedTextField(season, { season = it.filter(Char::isDigit) }, label = { Text(stringResource(R.string.issue_season)) }, singleLine = true, modifier = Modifier.weight(1f), colors = fieldColors(),
                        keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(keyboardType = androidx.compose.ui.text.input.KeyboardType.Number))
                    OutlinedTextField(episode, { episode = it.filter(Char::isDigit) }, label = { Text(stringResource(R.string.issue_episode)) }, singleLine = true, modifier = Modifier.weight(1f), colors = fieldColors(),
                        keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(keyboardType = androidx.compose.ui.text.input.KeyboardType.Number))
                }
                OutlinedTextField(message, { message = it }, label = { Text(stringResource(R.string.issue_message)) }, minLines = 3, modifier = Modifier.fillMaxWidth(), colors = fieldColors())
            }
        },
        confirmButton = {
            TextButton(enabled = message.isNotBlank() && !act.busy, onClick = {
                act.run(done, after = onDismiss) {
                    a.reportIssue(IssueInput(
                        kind = if (kind == "artist") "album" else kind, tmdbId = d.item.tmdbId, mbid = d.item.mbid, type = type,
                        seasonNumber = season.toIntOrNull(), episodeNumber = episode.toIntOrNull(), message = message.trim(),
                    ))
                }
            }) { Text(stringResource(R.string.send)) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.cancel)) } },
    )
}

