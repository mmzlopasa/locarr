package net.scyde.locarr.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.ManageSearch
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.*
import androidx.compose.material3.TabRowDefaults.tabIndicatorOffset
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import net.scyde.locarr.R
import net.scyde.locarr.api.*

// ---------- Library lists ----------

@Composable
fun LibraryScreen() {
    val nav = LocalNav.current
    var tab by rememberSaveable { mutableIntStateOf(0) }
    var q by rememberSaveable { mutableStateOf("") }
    var debounced by remember { mutableStateOf(q) }
    LaunchedEffect(q) { delay(350); debounced = q.trim() }
    val kinds = listOf("movie", "series", "artist")
    Screen(stringResource(R.string.tab_library), back = false, fab = {
        FloatingActionButton(onClick = { nav.navigate("add/${kinds[tab]}") }, containerColor = Lc.accent, contentColor = Lc.accentInk) {
            Icon(Icons.Default.Add, stringResource(R.string.add))
        }
    }) {
        Column {
            TabRow(tab, containerColor = Lc.bg, contentColor = Lc.text, indicator = { pos -> TabRowDefaults.SecondaryIndicator(Modifier.tabIndicatorOffset(pos[tab]), color = Lc.accent) }) {
                listOf(R.string.kind_movies, R.string.kind_series, R.string.kind_music).forEachIndexed { i, l -> Tab(tab == i, { tab = i }, text = { Text(stringResource(l)) }) }
            }
            SearchField(q, { q = it }, stringResource(R.string.library_filter))
            when (tab) {
                0 -> MovieList(debounced)
                1 -> SeriesList(debounced)
                else -> ArtistList(debounced)
            }
        }
    }
}

@Composable
fun movieStatusLabel(s: String) = stringResource(when (s) {
    "downloaded" -> R.string.mstatus_downloaded; "downloading" -> R.string.mstatus_downloading; "cutoff_unmet" -> R.string.mstatus_cutoff_unmet
    "unavailable" -> R.string.mstatus_unavailable; "unmonitored" -> R.string.mstatus_unmonitored; "partial" -> R.string.mstatus_partial
    "unreleased" -> R.string.mstatus_unreleased; else -> R.string.mstatus_missing
})

fun movieStatusTone(s: String) = when (s) {
    "downloaded" -> Tone.Success; "downloading" -> Tone.Info; "cutoff_unmet", "partial" -> Tone.Warning
    "missing" -> Tone.Danger; else -> Tone.Neutral
}

@Composable
private fun LibraryRow(poster: String?, title: String, subtitle: String, status: String?, square: Boolean = false, progress: Double? = null, onClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = Edge, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Poster(poster, title, Modifier.width(46.dp), square = square)
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Text(title, color = Lc.text, style = MaterialTheme.typography.bodyLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(subtitle, color = Lc.faint, style = MaterialTheme.typography.bodySmall, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (progress != null && progress > 0) ProgressBar(progress.toFloat())
        }
        if (status != null) { Spacer(Modifier.width(8.dp)); Pill(movieStatusLabel(status), movieStatusTone(status)) }
    }
}

@Composable
private fun MovieList(q: String) {
    val a = api(); val nav = LocalNav.current
    val pager = rememberPager("m", q) { off -> a.movies(q, null, off).let { it.items to (off + it.items.size < it.total) } }
    PagedList(pager, { it.id }, stringResource(R.string.library_empty)) { m ->
        LibraryRow(a.imageUrl(m.poster), m.title, listOfNotNull(m.year.takeIf { it > 0 }?.toString(), m.file?.quality?.name, m.file?.let { formatBytes(it.sizeBytes) }).joinToString(" · "),
            m.libraryStatus, progress = m.download?.progress) { nav.navigate("movie/${m.id}") }
    }
}

@Composable
private fun SeriesList(q: String) {
    val a = api(); val nav = LocalNav.current
    val pager = rememberPager("s", q) { off -> a.series(q, null, off).let { it.items to (off + it.items.size < it.total) } }
    PagedList(pager, { it.id }, stringResource(R.string.library_empty)) { s ->
        val st = when { !s.monitored -> "unmonitored"; s.downloading > 0 -> "downloading"; s.stats.missingCount > 0 -> "missing"; else -> "downloaded" }
        LibraryRow(a.imageUrl(s.poster), s.title, listOfNotNull(s.year.takeIf { it > 0 }?.toString(), s.network.ifBlank { null },
            stringResource(R.string.episodes_of, s.stats.episodeFileCount, s.stats.episodeCount)).joinToString(" · "), st) { nav.navigate("series/${s.id}") }
    }
}

@Composable
private fun ArtistList(q: String) {
    val a = api(); val nav = LocalNav.current
    val pager = rememberPager("a", q) { off -> a.artists(q, off).let { it.items to (off + it.items.size < it.total) } }
    PagedList(pager, { it.id }, stringResource(R.string.library_empty)) { ar ->
        LibraryRow(a.imageUrl(ar.cover, music = true), ar.name, listOfNotNull(ar.disambiguation.ifBlank { null }, stringResource(R.string.albums_of, ar.albumsWithFiles, ar.monitoredAlbums)).joinToString(" · "),
            if (ar.monitored) null else "unmonitored", square = true) { nav.navigate("artist/${ar.id}") }
    }
}

// ---------- "Why isn't it here?" and the timeline ----------

/** The answer to "why isn't it here?" for a library item, with Retry now and its timeline. */
@Composable
fun ExplainPanel(ref: String, retry: Boolean = true) {
    val a = api()
    val ctx = LocalContext.current
    val load = rememberLoad(ref) { a.timeline(ref, 60) }
    val act = rememberActions()
    var showAll by remember { mutableStateOf(false) }
    val t = load.data
    when {
        t == null && load.error != null -> ErrorBox(load.error!!, { load.reload() })
        t == null -> Loading()
        else -> {
            val ex = t.explanation
            val tone = Tone.of(ex.tone)
            Panel(tone = tone) {
                Text(stringResource(R.string.why_not_here).uppercase(), style = Eyebrow, color = tone.fg)
                Spacer(Modifier.height(4.dp))
                Text(explanationText(ctx, ex), color = Lc.text)
                if (ex.reasons.isNotEmpty()) {
                    Spacer(Modifier.height(8.dp))
                    ex.reasons.take(4).forEach { r -> Text("• " + codeText(ctx, "rejectionCodes.${r.code}", fallback = r.example) + " × ${r.count}", color = Lc.muted, style = MaterialTheme.typography.bodySmall) }
                }
                if (retry && ex.code != "available") {
                    Spacer(Modifier.height(8.dp))
                    val queued = stringResource(R.string.retry_queued)
                    Button(onClick = { act.run(queued, after = { load.reload() }) { a.retryNow(ref) } }, enabled = !act.busy, shape = Shape) {
                        Icon(Icons.Default.Refresh, null); Spacer(Modifier.width(6.dp)); Text(stringResource(R.string.retry_now))
                    }
                    Text(stringResource(R.string.retry_hint), color = Lc.faint, style = MaterialTheme.typography.bodySmall)
                }
            }
            if (t.events.isNotEmpty()) {
                Text(stringResource(R.string.timeline), style = MaterialTheme.typography.titleMedium, modifier = Modifier.padding(top = 6.dp))
                val shown = if (showAll) t.events else t.events.take(8)
                shown.forEach { e -> TimelineRow(e) }
                if (!showAll && t.events.size > 8) TextButton(onClick = { showAll = true }) { Text(stringResource(R.string.show_more, t.events.size - 8)) }
            }
        }
    }
}

fun eventTone(type: String): Tone = when (type) {
    "file_imported", "download_imported", "upgraded", "download_completed", "file_found", "release_matched", "cover_saved", "subtitle_downloaded" -> Tone.Success
    "import_failed", "import_skipped", "download_import_failed", "download_failed", "grab_failed" -> Tone.Danger
    "pending", "retry", "download_blocklisted", "recycled", "deleted" -> Tone.Warning
    "search", "decision", "download_grabbed", "download_progress" -> Tone.Info
    else -> Tone.Neutral
}

@Composable
private fun TimelineRow(e: TimelineEvent) {
    Row(Modifier.fillMaxWidth().padding(vertical = 6.dp)) {
        Dot(eventTone(e.type), Modifier.padding(top = 6.dp))
        Spacer(Modifier.width(10.dp))
        Column(Modifier.weight(1f)) {
            Text(e.message.ifBlank { e.type }, color = Lc.text, style = MaterialTheme.typography.bodySmall)
            Text(formatDateTime(e.at), color = Lc.faint, style = MaterialTheme.typography.labelSmall)
        }
    }
}

@Composable
fun FileInfo(f: MediaFile) {
    Panel {
        Text(stringResource(R.string.file).uppercase(), style = Eyebrow, color = Lc.muted)
        Spacer(Modifier.height(4.dp))
        Text(f.path, color = Lc.text, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
        Spacer(Modifier.height(6.dp))
        KeyValue(stringResource(R.string.quality), f.quality.name)
        KeyValue(stringResource(R.string.size), formatBytes(f.sizeBytes))
        KeyValue(stringResource(R.string.video), listOfNotNull(f.mediaInfo.resolution?.let { "${it}p" }, f.mediaInfo.videoCodec, f.mediaInfo.hdr?.joinToString("/")?.ifBlank { null }).joinToString(" · "))
        KeyValue(stringResource(R.string.audio), listOfNotNull(f.mediaInfo.audioCodec, f.mediaInfo.audioChannels).joinToString(" · "))
        KeyValue(stringResource(R.string.languages), f.languages.joinToString(", "))
        KeyValue(stringResource(R.string.subtitles), f.mediaInfo.subtitles?.joinToString(", "))
        KeyValue(stringResource(R.string.release_group), f.releaseGroup)
        if (f.customFormats.isNotEmpty()) KeyValue(stringResource(R.string.custom_formats), f.customFormats.joinToString(", ") + " (${f.score})")
        KeyValue(stringResource(R.string.added), formatDateTime(f.addedAt))
    }
}

@Composable
private fun DetailHeader(backdrop: String?, poster: String?, title: String, meta: String, square: Boolean = false, pills: @Composable () -> Unit) {
    Box(Modifier.fillMaxWidth().aspectRatio(if (isWide()) 3.2f else 2.2f)) {
        RemoteImage(backdrop, Modifier.fillMaxSize())
        Box(Modifier.fillMaxSize().background(Brush.verticalGradient(listOf(Color.Transparent, Lc.bg))))
    }
    Row(Modifier.padding(horizontal = Edge).offset(y = (-36).dp), horizontalArrangement = Arrangement.spacedBy(14.dp), verticalAlignment = Alignment.Bottom) {
        Poster(poster, title, Modifier.width(96.dp), square = square)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(title, style = MaterialTheme.typography.titleLarge, color = Lc.text)
            if (meta.isNotBlank()) Text(meta, color = Lc.muted, style = MaterialTheme.typography.bodySmall)
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) { pills() }
        }
    }
}

@Composable
private fun MonitorRow(monitored: Boolean, onChange: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth().clip(Shape).clickable { onChange(!monitored) }.padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(stringResource(R.string.monitored), color = Lc.text)
            Text(stringResource(R.string.monitored_hint), color = Lc.faint, style = MaterialTheme.typography.bodySmall)
        }
        Switch(monitored, onChange, colors = SwitchDefaults.colors(checkedTrackColor = Lc.accent, checkedThumbColor = Lc.accentInk))
    }
}

// ---------- Movie ----------

@Composable
fun MovieScreen(id: Long) {
    val a = api(); val nav = LocalNav.current
    val load = rememberLoad(id) { a.movie(id) }
    val act = rememberActions()
    val searching = stringResource(R.string.search_started)
    Screen(load.data?.title ?: "", actions = {
        IconButton(onClick = { act.run(searching) { a.searchMovie(id) } }) { Icon(Icons.Default.Search, stringResource(R.string.search_now)) }
        IconButton(onClick = { nav.navigate("releases/movie/$id/0") }) { Icon(Icons.Default.ManageSearch, stringResource(R.string.interactive_search)) }
    }) {
        Loaded(load) { m ->
            Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                DetailHeader(a.imageUrl(m.backdrop, wide = true), a.imageUrl(m.poster), m.title,
                    listOfNotNull(m.year.takeIf { it > 0 }?.toString(), m.runtime.takeIf { it > 0 }?.let { stringResource(R.string.minutes, it) }, m.studio.ifBlank { null }).joinToString(" · ")) {
                    Pill(movieStatusLabel(m.libraryStatus), movieStatusTone(m.libraryStatus))
                }
                Readable {
                    Column(Modifier.padding(horizontal = Edge).offset(y = (-20).dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                        MonitorRow(m.monitored) { v -> act.run(after = { load.reload() }) { a.updateMovie(m.id, MovieUpdate(monitored = v)) } }
                        m.download?.let { d ->
                            Panel(tone = Tone.Info) {
                                Text(d.title, color = Lc.text, style = MaterialTheme.typography.bodySmall)
                                Spacer(Modifier.height(6.dp)); ProgressBar(d.progress.toFloat())
                                Text(percent(d.progress) + " · " + codeText(LocalContext.current, "downloadStates.${d.state}", fallback = d.state), color = Lc.muted, style = MaterialTheme.typography.bodySmall)
                            }
                        }
                        ExplainPanel(m.ref)
                        m.file?.let { FileInfo(it) }
                        if (m.overview.isNotBlank()) ExpandableText(m.overview)
                        Panel {
                            KeyValue(stringResource(R.string.in_cinemas), formatDate(m.inCinemas))
                            KeyValue(stringResource(R.string.digital_release), formatDate(m.digitalRelease))
                            KeyValue(stringResource(R.string.physical_release), formatDate(m.physicalRelease))
                            KeyValue(stringResource(R.string.path), m.path)
                            KeyValue(stringResource(R.string.tags), m.tags.joinToString(", "))
                        }
                    }
                }
            }
        }
    }
}

// ---------- Series ----------

@Composable
fun SeriesScreen(id: Long) {
    val a = api(); val nav = LocalNav.current
    val load = rememberLoad(id) { a.seriesDetail(id) }
    val act = rememberActions()
    val searching = stringResource(R.string.search_started)
    Screen(load.data?.title ?: "", actions = {
        IconButton(onClick = { act.run(searching) { a.searchSeries(id) } }) { Icon(Icons.Default.Search, stringResource(R.string.search_missing)) }
    }) {
        Loaded(load) { s ->
            Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                DetailHeader(a.imageUrl(s.backdrop, wide = true), a.imageUrl(s.poster), s.title,
                    listOfNotNull(s.year.takeIf { it > 0 }?.toString(), s.network.ifBlank { null }, s.status.ifBlank { null }).joinToString(" · ")) {
                    Pill(stringResource(R.string.episodes_of, s.stats.episodeFileCount, s.stats.episodeCount), if (s.stats.missingCount > 0) Tone.Warning else Tone.Success)
                    if (s.downloading > 0) Pill(stringResource(R.string.downloading_n, s.downloading), Tone.Info)
                }
                Readable {
                    Column(Modifier.padding(horizontal = Edge).offset(y = (-20).dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                        MonitorRow(s.monitored) { v -> act.run(after = { load.reload() }) { a.updateSeries(s.id, SeriesUpdate(monitored = v)) } }
                        s.stats.nextAiring?.let { Text(stringResource(R.string.next_airing, formatDateTime(it)), color = Lc.muted, style = MaterialTheme.typography.bodySmall) }
                        if (s.overview.isNotBlank()) ExpandableText(s.overview, 3)
                        Text(stringResource(R.string.seasons), style = MaterialTheme.typography.titleMedium)
                        s.seasons.sortedByDescending { it.number }.forEach { se ->
                            val done = se.stats.episodeFileCount; val total = se.stats.episodeCount
                            Row(Modifier.fillMaxWidth().clip(Shape).background(Lc.surface).clickable { nav.navigate("season/${s.id}/${se.number}") }.padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
                                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                                    Text(if (se.number == 0) stringResource(R.string.specials) else stringResource(R.string.season_n, se.number), color = Lc.text)
                                    ProgressBar(if (total > 0) done / total.toFloat() else 0f, color = if (done >= total) Lc.success else Lc.warning)
                                }
                                Spacer(Modifier.width(12.dp))
                                Text("$done / $total", color = Lc.muted, style = MaterialTheme.typography.bodySmall)
                                if (!se.monitored) { Spacer(Modifier.width(8.dp)); Pill(stringResource(R.string.mstatus_unmonitored)) }
                            }
                        }
                        Panel {
                            KeyValue(stringResource(R.string.path), s.path)
                            KeyValue(stringResource(R.string.size), formatBytes(s.stats.sizeOnDisk))
                            KeyValue(stringResource(R.string.tags), s.tags.joinToString(", "))
                        }
                    }
                }
            }
        }
    }
}

@Composable
fun SeasonScreen(seriesId: Long, number: Int) {
    val a = api(); val nav = LocalNav.current
    val load = rememberLoad(seriesId, number) { a.episodes(seriesId).filter { it.seasonNumber == number } }
    val act = rememberActions()
    var open by remember { mutableStateOf<Long?>(null) }
    val searching = stringResource(R.string.search_started)
    Screen(if (number == 0) stringResource(R.string.specials) else stringResource(R.string.season_n, number), actions = {
        IconButton(onClick = { act.run(searching) { a.searchSeason(seriesId, number) } }) { Icon(Icons.Default.Search, stringResource(R.string.search_season)) }
        IconButton(onClick = { nav.navigate("releases/season/$seriesId/$number") }) { Icon(Icons.Default.ManageSearch, stringResource(R.string.interactive_search)) }
    }) {
        Loaded(load) { eps ->
            if (eps.isEmpty()) { EmptyBox(stringResource(R.string.no_episodes)); return@Loaded }
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                items(eps, key = { it.id }) { e ->
                    Readable {
                        Column {
                            val st = when { e.file != null && e.cutoffMet -> "downloaded"; e.file != null -> "cutoff_unmet"; e.download != null -> "downloading"; !e.monitored -> "unmonitored"; !e.aired -> "unreleased"; else -> "missing" }
                            ListRow("${e.episodeNumber}. ${e.title.ifBlank { "–" }}", subtitle = listOfNotNull(formatDate(e.airDate).ifBlank { null }, e.file?.quality?.name).joinToString(" · "),
                                trailing = { Pill(movieStatusLabel(st), movieStatusTone(st)) }, onClick = { open = if (open == e.id) null else e.id })
                            if (open == e.id) Column(Modifier.padding(horizontal = Edge).padding(bottom = 12.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                                FlowRowCompat {
                                    OutlinedButton(onClick = { act.run(searching) { a.searchEpisodes(listOf(e.id)) } }, shape = Shape) { Text(stringResource(R.string.search_now)) }
                                    OutlinedButton(onClick = { nav.navigate("releases/episode/${e.id}/0") }, shape = Shape) { Text(stringResource(R.string.interactive_search)) }
                                    OutlinedButton(onClick = { act.run(after = { load.reload() }) { a.monitorEpisodes(listOf(e.id), !e.monitored) } }, shape = Shape) {
                                        Text(stringResource(if (e.monitored) R.string.unmonitor else R.string.monitor))
                                    }
                                }
                                if (e.overview.isNotBlank()) Text(e.overview, color = Lc.muted, style = MaterialTheme.typography.bodySmall)
                                e.file?.let { FileInfo(it) }
                                ExplainPanel(e.ref)
                            }
                        }
                    }
                }
            }
        }
    }
}

// ---------- Music ----------

@Composable
fun ArtistScreen(id: Long) {
    val a = api(); val nav = LocalNav.current
    val load = rememberLoad(id) { a.artist(id) to a.artistAlbums(id) }
    val act = rememberActions()
    val searching = stringResource(R.string.search_started)
    Screen(load.data?.first?.name ?: "", actions = {
        IconButton(onClick = { act.run(searching) { a.searchArtist(id) } }) { Icon(Icons.Default.Search, stringResource(R.string.search_missing)) }
    }) {
        Loaded(load) { (ar, albums) ->
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                item {
                    Readable {
                        Row(Modifier.padding(Edge), horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                            Poster(a.imageUrl(ar.cover, music = true), ar.name, Modifier.width(100.dp), square = true)
                            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                                Text(ar.name, style = MaterialTheme.typography.headlineSmall)
                                Text(listOf(ar.disambiguation, ar.type, ar.country).filter { it.isNotBlank() }.joinToString(" · "), color = Lc.muted, style = MaterialTheme.typography.bodySmall)
                                Text(stringResource(R.string.albums_of, ar.albumsWithFiles, ar.monitoredAlbums) + " · " + formatBytes(ar.sizeBytes), color = Lc.faint, style = MaterialTheme.typography.bodySmall)
                            }
                        }
                    }
                }
                item { SectionTitle(stringResource(R.string.albums)) }
                items(albums, key = { it.id }) { al ->
                    Readable {
                        LibraryRow(a.imageUrl(al.cover, music = true), al.title, listOfNotNull(al.releaseDate.take(4).ifBlank { null }, al.primaryType.ifBlank { null }, al.quality?.name).joinToString(" · "),
                            al.libraryStatus, square = true, progress = al.download?.progress) { nav.navigate("album/${al.id}") }
                    }
                }
            }
        }
    }
}

@Composable
fun AlbumScreen(id: Long) {
    val a = api(); val nav = LocalNav.current
    val load = rememberLoad(id) { a.album(id) }
    val act = rememberActions()
    val searching = stringResource(R.string.search_started)
    Screen(load.data?.album?.title ?: "", actions = {
        IconButton(onClick = { act.run(searching) { a.searchAlbum(id) } }) { Icon(Icons.Default.Search, stringResource(R.string.search_now)) }
        IconButton(onClick = { nav.navigate("releases/album/$id/0") }) { Icon(Icons.Default.ManageSearch, stringResource(R.string.interactive_search)) }
    }) {
        Loaded(load) { d ->
            val al = d.album
            Readable {
                Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(Edge), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    Row(horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                        Poster(a.imageUrl(al.cover, music = true), al.title, Modifier.width(120.dp), square = true)
                        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            Text(al.title, style = MaterialTheme.typography.titleLarge)
                            Text(al.artistName, color = Lc.muted, modifier = Modifier.clickable { nav.navigate("artist/${al.artistId}") })
                            Text(listOfNotNull(formatDate(al.releaseDate).ifBlank { null }, al.release?.format?.ifBlank { null }).joinToString(" · "), color = Lc.faint, style = MaterialTheme.typography.bodySmall)
                            Pill(movieStatusLabel(al.libraryStatus), movieStatusTone(al.libraryStatus))
                        }
                    }
                    MonitorRow(al.monitored) { v -> act.run(after = { load.reload() }) { a.monitorAlbums(listOf(al.id), v) } }
                    ExplainPanel(al.ref)
                    Text(stringResource(R.string.tracks), style = MaterialTheme.typography.titleMedium)
                    d.tracksError?.let { Text(it, color = Lc.warning, style = MaterialTheme.typography.bodySmall) }
                    d.tracks.forEach { t ->
                        Row(Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                            Text(t.number.ifBlank { t.position.toString() }, color = Lc.faint, modifier = Modifier.width(32.dp), style = MaterialTheme.typography.bodySmall)
                            Text(t.title, color = Lc.text, modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Text(formatDuration(t.lengthMs / 1000L).takeIf { t.lengthMs > 0 } ?: "", color = Lc.faint, style = MaterialTheme.typography.bodySmall)
                            Spacer(Modifier.width(8.dp))
                            Dot(if (t.file != null) Tone.Success else Tone.Neutral)
                        }
                    }
                }
            }
        }
    }
}

// ---------- Interactive search ----------

@Composable
fun ReleasesScreen(kind: String, id: Long, n: Int) {
    val a = api()
    val ctx = LocalContext.current
    val target = when (kind) {
        "episode" -> ReleaseTarget.Episode(id); "season" -> ReleaseTarget.Season(id, n); "album" -> ReleaseTarget.Album(id); else -> ReleaseTarget.Movie(id)
    }
    val load = rememberLoad(kind, id, n) { a.releases(target) }
    val act = rememberActions()
    var filter by rememberSaveable { mutableStateOf("all") }
    var grabbed by remember { mutableStateOf(setOf<String>()) }
    var confirm by remember { mutableStateOf<InteractiveDecision?>(null) }
    val sent = stringResource(R.string.grab_sent)
    val grab: (InteractiveDecision) -> Unit = { d ->
        val key = d.decision.candidate.guid ?: d.decision.candidate.title
        act.run(sent, after = { grabbed = grabbed + key }) { a.grab(target, d) }
    }
    Screen(stringResource(R.string.interactive_search)) {
        val s = load.data
        if (s == null) {
            if (load.error != null) ErrorBox(load.error!!, { load.reload() })
            else MessageBox(Icons.Default.ManageSearch, stringResource(R.string.asking_indexers), detail = stringResource(R.string.asking_indexers_hint)) { CircularProgressIndicator(color = Lc.accent) }
            return@Screen
        }
        val list = s.decisions.filter { filter == "all" || it.decision.status == filter }
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
            item {
                Readable {
                    Column(Modifier.padding(Edge), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text(stringResource(R.string.decision_summary, s.accepted, s.pending, s.rejected, s.profileName), color = Lc.muted, style = MaterialTheme.typography.bodySmall)
                        FlowRowCompat { s.indexers.forEach { i -> Pill(i.indexerName + ": " + if (i.status == "ok") i.count.toString() else codeText(ctx, "timeline.indexerStatus.${i.status}", fallback = i.status), if (i.status == "ok") Tone.Neutral else Tone.Warning) } }
                    }
                }
            }
            item {
                FilterChips(listOf("all" to stringResource(R.string.filter_all), "accepted" to stringResource(R.string.accepted), "pending" to stringResource(R.string.held_back), "rejected" to stringResource(R.string.rejected)), filter, { filter = it })
            }
            if (list.isEmpty()) item { EmptyBox(stringResource(R.string.no_releases)) }
            items(list, key = { (it.decision.candidate.guid ?: "") + it.decision.candidate.title + it.decision.candidate.indexerId }) { d ->
                Readable { ReleaseRow(d, grabbed.contains(d.decision.candidate.guid ?: d.decision.candidate.title), act.busy) { if (d.decision.status == "rejected") confirm = d else grab(d) } }
            }
        }
        confirm?.let { d ->
            ConfirmDialog(stringResource(R.string.grab_anyway), stringResource(R.string.grab_anyway_hint), stringResource(R.string.grab), { confirm = null }) { grab(d) }
        }
    }
}

@Composable
private fun ReleaseRow(d: InteractiveDecision, grabbed: Boolean, busy: Boolean, onGrab: () -> Unit) {
    val ctx = LocalContext.current
    val dec = d.decision
    val c = dec.candidate
    val tone = when (dec.status) { "accepted" -> Tone.Success; "pending" -> Tone.Warning; else -> Tone.Danger }
    Column(Modifier.fillMaxWidth().padding(horizontal = Edge, vertical = 8.dp).clip(Shape).background(Lc.surface).padding(12.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(c.title, color = Lc.text, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium)
        FlowRowCompat {
            Pill(stringResource(when (dec.status) { "accepted" -> R.string.accepted; "pending" -> R.string.held_back; else -> R.string.rejected }) + (dec.rank?.let { " #$it" } ?: ""), tone)
            Pill(dec.quality.name.ifBlank { "?" })
            c.size?.let { Pill(formatBytes(it)) }
            if (c.protocol == "torrent" && c.seeders != null) Pill(stringResource(R.string.seeders_n, c.seeders))
            c.indexerName?.let { Pill(it) }
            if (dec.score != 0) Pill(stringResource(R.string.score_n, dec.score), if (dec.score > 0) Tone.Info else Tone.Warning)
            c.publishDate?.let { Pill(formatDate(it)) }
        }
        if (dec.customFormats.isNotEmpty()) Text(dec.customFormats.joinToString(", ") { "${it.name} (${it.score})" }, color = Lc.muted, style = MaterialTheme.typography.bodySmall)
        dec.rejections.forEach { r -> Text("• " + codeText(ctx, "rejectionCodes.${r.code}", fallback = r.message) + if (r.message.isNotBlank() && CODE_STRINGS.containsKey("rejectionCodes.${r.code}")) " — ${r.message}" else "", color = Lc.danger, style = MaterialTheme.typography.bodySmall) }
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
            if (grabbed) Pill(stringResource(R.string.grabbed), Tone.Success)
            else if (d.downloadToken != null || d.magnetUrl != null) {
                if (dec.status == "rejected") OutlinedButton(onClick = onGrab, enabled = !busy, shape = Shape) { Text(stringResource(R.string.grab_anyway)) }
                else Button(onClick = onGrab, enabled = !busy, shape = Shape) { Text(stringResource(R.string.grab)) }
            }
        }
    }
}

// ---------- Adding ----------

@Composable
fun AddScreen(kind: String) {
    val a = api(); val nav = LocalNav.current
    var q by rememberSaveable { mutableStateOf("") }
    var debounced by remember { mutableStateOf("") }
    LaunchedEffect(q) { delay(500); debounced = q.trim() }
    var picked by remember { mutableStateOf<Any?>(null) }
    val title = stringResource(when (kind) { "series" -> R.string.add_series; "artist" -> R.string.add_artist; else -> R.string.add_movie })
    val load = rememberLoad(kind, debounced) {
        if (debounced.length < 2) emptyList<Any>() else when (kind) {
            "series" -> a.lookupSeries(debounced); "artist" -> a.lookupArtists(debounced); else -> a.lookupMovies(debounced)
        }
    }
    Screen(title) {
        Column {
            SearchField(q, { q = it }, stringResource(if (kind == "artist") R.string.add_search_artist else R.string.add_search_hint))
            if (debounced.length < 2) { EmptyBox(stringResource(R.string.add_start)); return@Column }
            Loaded(load) { results ->
                if (results.isEmpty()) { EmptyBox(stringResource(R.string.nothing_found)); return@Loaded }
                LazyColumn(Modifier.fillMaxSize()) {
                    items(results) { r ->
                        Readable {
                            when (r) {
                                is MovieLookup -> LibraryRow(a.imageUrl(r.poster), r.title, r.year.takeIf { it > 0 }?.toString() ?: "", if (r.movieId != null) "downloaded" else null) {
                                    if (r.movieId != null) nav.navigate("movie/${r.movieId}") else picked = r
                                }
                                is SeriesLookup -> LibraryRow(a.imageUrl(r.poster), r.title, listOfNotNull(r.year.takeIf { it > 0 }?.toString(), r.network.ifBlank { null }).joinToString(" · "), if (r.seriesId != null) "downloaded" else null) {
                                    if (r.seriesId != null) nav.navigate("series/${r.seriesId}") else picked = r
                                }
                                is ArtistLookup -> ListRow(r.name, subtitle = listOf(r.disambiguation, r.type, r.country).filter { it.isNotBlank() }.joinToString(" · "),
                                    trailing = { if (r.artistId != null) Pill(stringResource(R.string.in_library), Tone.Success) }) {
                                    if (r.artistId != null) nav.navigate("artist/${r.artistId}") else picked = r
                                }
                            }
                        }
                    }
                }
            }
        }
    }
    picked?.let { AddDialog(kind, it, onDismiss = { picked = null }) }
}

@Composable
private fun <T> Picker(label: String, options: List<T>, selected: T?, name: (T) -> String, onPick: (T) -> Unit) {
    var open by remember { mutableStateOf(false) }
    Box {
        OutlinedTextField(selected?.let(name) ?: "", {}, readOnly = true, label = { Text(label) }, modifier = Modifier.fillMaxWidth(), colors = fieldColors(),
            trailingIcon = { TextButton(onClick = { open = true }) { Text(stringResource(R.string.change)) } })
        Box(Modifier.matchParentSize().clickable { open = true })
        DropdownMenu(open, { open = false }) { options.forEach { o -> DropdownMenuItem(text = { Text(name(o)) }, onClick = { onPick(o); open = false }) } }
    }
}

@Composable
private fun AddDialog(kind: String, item: Any, onDismiss: () -> Unit) {
    val a = api(); val nav = LocalNav.current
    val mediaKind = when (kind) { "series" -> "series"; "artist" -> "music"; else -> "movies" }
    val opts = rememberLoad(kind) {
        Triple(a.rootFolders().filter { it.kind == mediaKind }, a.qualityProfiles().filter { it.kind == mediaKind }, if (kind == "artist") a.metadataProfiles() else emptyList())
    }
    var root by remember { mutableStateOf<RootFolder?>(null) }
    var profile by remember { mutableStateOf<QualityProfile?>(null) }
    var meta by remember { mutableStateOf<MetadataProfile?>(null) }
    var monitor by remember { mutableStateOf("all") }
    var search by remember { mutableStateOf(true) }
    LaunchedEffect(opts.data) { opts.data?.let { (r, p, m) -> root = root ?: r.firstOrNull(); profile = profile ?: p.firstOrNull(); meta = meta ?: m.firstOrNull() } }
    val act = rememberActions()
    val added = stringResource(R.string.added_ok)
    val name = when (item) { is MovieLookup -> item.title; is SeriesLookup -> item.title; is ArtistLookup -> item.name; else -> "" }
    AlertDialog(
        onDismissRequest = onDismiss, containerColor = Lc.surface2,
        title = { Text(name) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                val o = opts.data
                when {
                    o == null && opts.error != null -> Text(errorText(opts.error!!), color = Lc.danger)
                    o == null -> Loading()
                    o.first.isEmpty() -> Text(stringResource(R.string.add_no_root), color = Lc.warning)
                    else -> {
                        Picker(stringResource(R.string.root_folder), o.first, root, { it.path }) { root = it }
                        Picker(stringResource(R.string.quality_profile), o.second, profile, { it.name }) { profile = it }
                        if (kind == "artist") Picker(stringResource(R.string.metadata_profile), o.third, meta, { it.name }) { meta = it }
                        if (kind == "series") {
                            val labels = listOf("all", "future", "missing", "existing", "first_season", "latest_season", "none").associateWith { monitorLabelText(it) }
                            Picker(stringResource(R.string.monitor), labels.keys.toList(), monitor, { labels[it] ?: it }) { monitor = it }
                        }
                        Row(Modifier.fillMaxWidth().clickable { search = !search }, verticalAlignment = Alignment.CenterVertically) {
                            Checkbox(search, { search = it }); Text(stringResource(R.string.search_on_add))
                        }
                    }
                }
            }
        },
        confirmButton = {
            TextButton(enabled = !act.busy && root != null && profile != null && (kind != "artist" || meta != null), onClick = {
                act.run(added) {
                    val r = root!!.id; val p = profile!!.id
                    val route = when (item) {
                        is MovieLookup -> "movie/" + a.addMovie(AddMovieRequest(item.tmdbId, r, p, searchOnAdd = search)).id
                        is SeriesLookup -> "series/" + a.addSeries(AddSeriesRequest(tmdbId = item.tmdbId.takeIf { it > 0 }, tvdbId = item.tvdbId.takeIf { item.source == "tvdb" && it > 0 }, rootFolderId = r, qualityProfileId = p, monitor = monitor, searchOnAdd = search)).id
                        is ArtistLookup -> "artist/" + a.addArtist(AddArtistRequest(item.mbid, r, p, meta!!.id, searchOnAdd = search)).id
                        else -> null
                    }
                    onDismiss()
                    if (route != null) nav.navigate(route)
                }
            }) { Text(stringResource(R.string.add)) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.cancel)) } },
    )
}

@Composable
private fun monitorLabelText(m: String) = stringResource(when (m) {
    "future" -> R.string.monitor_future; "missing" -> R.string.monitor_missing; "existing" -> R.string.monitor_existing
    "first_season" -> R.string.monitor_first; "latest_season" -> R.string.monitor_latest; "none" -> R.string.monitor_none; else -> R.string.monitor_all
})
