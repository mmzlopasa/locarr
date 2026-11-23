package net.scyde.locarr.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.OpenInNew
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.*
import androidx.compose.material3.TabRowDefaults.tabIndicatorOffset
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import net.scyde.locarr.R
import net.scyde.locarr.api.*
import java.time.LocalDate

// ---------- Overview ----------

/** The admin home: health, the queue, "why didn't it land?", what's coming, requests waiting. */
@Composable
fun OverviewScreen() {
    val a = api(); val nav = LocalNav.current
    val me = LocalState.current.me
    val ctx = LocalContext.current
    val load = rememberLoad {
        val today = LocalDate.now()
        OverviewData(
            health = if (me?.isAdmin == true) runCatching { a.health() }.getOrNull() else null,
            queue = runCatching { a.queue() }.getOrNull(),
            board = runCatching { a.board() }.getOrNull(),
            upcoming = runCatching { a.calendar(today.toString(), today.plusDays(7).toString()) }.getOrDefault(emptyList()),
            summary = runCatching { a.requestSummary() }.getOrNull(),
        )
    }
    Screen(stringResource(R.string.tab_home), back = false) {
        Loaded(load) { d ->
            Readable(max = 900.dp) {
                Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(Edge), verticalArrangement = Arrangement.spacedBy(14.dp)) {
                    // Health
                    d.health?.let { h ->
                        val errors = h.issues.count { it.severity == "error" }
                        val warnings = h.issues.count { it.severity == "warning" }
                        val tone = if (errors > 0) Tone.Danger else if (warnings > 0) Tone.Warning else Tone.Success
                        Panel(Modifier.clickable { nav.navigate("health") }, tone = tone) {
                            Text(stringResource(R.string.health).uppercase(), style = Eyebrow, color = tone.fg)
                            Text(if (h.issues.isEmpty()) stringResource(R.string.health_ok) else stringResource(R.string.health_counts, errors, warnings), color = Lc.text, style = MaterialTheme.typography.titleMedium)
                            h.issues.take(2).forEach { Text("• " + healthText(ctx, it), color = Lc.muted, style = MaterialTheme.typography.bodySmall, maxLines = 2, overflow = TextOverflow.Ellipsis) }
                        }
                    }
                    // Requests and issues
                    d.summary?.let { s ->
                        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                            StatTile(stringResource(R.string.pending_requests), s.pending, if (s.pending > 0) Tone.Warning else Tone.Neutral, Modifier.weight(1f)) { nav.navigate("all-requests") }
                            StatTile(stringResource(R.string.on_their_way), s.processing, Tone.Info, Modifier.weight(1f)) { nav.navigate("all-requests") }
                            StatTile(stringResource(R.string.open_issues), s.openIssues, if (s.openIssues > 0) Tone.Danger else Tone.Neutral, Modifier.weight(1f)) { nav.navigate("issues") }
                        }
                    }
                    // Queue
                    d.queue?.let { q ->
                        val active = q.items.filter { it.status == "active" }
                        SectionTitle(stringResource(R.string.queue_n, active.size), Modifier.padding(horizontal = 0.dp).offset(x = (-16).dp), action = { TextButton(onClick = { nav.navigate("activity") }) { Text(stringResource(R.string.see_all)) } })
                        if (active.isEmpty()) Text(stringResource(R.string.queue_empty), color = Lc.faint, style = MaterialTheme.typography.bodySmall)
                        active.take(4).forEach { QueueLine(it) }
                        q.clients.filter { it.enabled && !it.status.ok }.forEach { c ->
                            Panel(tone = Tone.Danger) { Text(stringResource(R.string.client_down, c.name) + (c.status.error?.let { ": $it" } ?: ""), color = Lc.text, style = MaterialTheme.typography.bodySmall) }
                        }
                    }
                    // Why didn't it land?
                    d.board?.let { b ->
                        SectionTitle(stringResource(R.string.board_title), Modifier.offset(x = (-16).dp))
                        val groups = b.groups.filter { it.code !in setOf("available", "downloading", "importing", "not_monitored") && it.count > 0 }
                        if (groups.isEmpty()) Text(stringResource(R.string.board_ok), color = Lc.faint, style = MaterialTheme.typography.bodySmall)
                        groups.take(5).forEach { g -> BoardGroupRow(g) { nav.navigate("board/${g.code}") } }
                    }
                    // Upcoming
                    if (d.upcoming.isNotEmpty()) {
                        SectionTitle(stringResource(R.string.upcoming), Modifier.offset(x = (-16).dp), action = { TextButton(onClick = { nav.navigate("calendar") }) { Text(stringResource(R.string.see_all)) } })
                        d.upcoming.take(6).forEach { e -> CalendarLine(e) }
                    }
                }
            }
        }
    }
}

private data class OverviewData(val health: HealthReport?, val queue: Queue?, val board: BoardSummary?, val upcoming: List<CalendarEntry>, val summary: RequestSummary?)

@Composable
private fun StatTile(label: String, value: Int, tone: Tone, modifier: Modifier, onClick: () -> Unit) {
    Column(modifier.clip(Shape).background(Lc.surface).clickable(onClick = onClick).padding(12.dp)) {
        Text(value.toString(), style = MaterialTheme.typography.headlineSmall, color = if (value > 0) tone.fg else Lc.text)
        Text(label, color = Lc.muted, style = MaterialTheme.typography.bodySmall, maxLines = 2)
    }
}

fun healthText(ctx: android.content.Context, i: HealthIssue) = codeText(ctx, "health.codes.${i.code}", i.params.texts(), i.message)

// ---------- Activity tabs ----------

@Composable
fun ActivityScreen() {
    var tab by rememberSaveable { mutableIntStateOf(0) }
    val tabs = listOf(R.string.queue, R.string.board_short, R.string.wanted, R.string.calendar, R.string.history)
    Screen(stringResource(R.string.tab_activity), back = false) {
        Column {
            ScrollableTabRow(tab, containerColor = Lc.bg, contentColor = Lc.text, edgePadding = Edge,
                indicator = { pos -> TabRowDefaults.SecondaryIndicator(Modifier.tabIndicatorOffset(pos[tab]), color = Lc.accent) }) {
                tabs.forEachIndexed { i, l -> Tab(tab == i, { tab = i }, text = { Text(stringResource(l)) }) }
            }
            when (tab) {
                0 -> QueueContent()
                1 -> BoardContent()
                2 -> WantedContent()
                3 -> CalendarContent()
                else -> HistoryContent()
            }
        }
    }
}

@Composable fun CalendarScreen() = Screen(stringResource(R.string.calendar)) { CalendarContent() }
@Composable fun WantedScreen() = Screen(stringResource(R.string.wanted)) { WantedContent() }
@Composable fun HistoryScreen() = Screen(stringResource(R.string.history)) { HistoryContent() }

// ---------- Queue ----------

@Composable
private fun QueueLine(q: QueueItem, onClick: (() -> Unit)? = null) {
    val ctx = LocalContext.current
    Column(Modifier.fillMaxWidth().clip(Shape).background(Lc.surface).then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier).padding(12.dp), verticalArrangement = Arrangement.spacedBy(5.dp)) {
        Text(q.item?.let { it.title + (it.year?.let { y -> " ($y)" } ?: "") } ?: q.title, color = Lc.text, style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
        if (q.item != null) Text(q.title, color = Lc.faint, style = MaterialTheme.typography.labelSmall, maxLines = 1, overflow = TextOverflow.Ellipsis, fontFamily = FontFamily.Monospace)
        ProgressBar(q.progress.toFloat(), color = when (q.state) { "failed", "warning" -> Lc.danger; "paused" -> Lc.muted; "seeding", "completed" -> Lc.success; else -> Lc.accent })
        Text(listOfNotNull(
            codeText(ctx, "downloadStates.${q.state}", fallback = q.state), percent(q.progress),
            formatBytes(q.downloadedBytes) + " / " + formatBytes(q.sizeBytes),
            q.downloadSpeed.takeIf { it > 0 }?.let { formatBytes(it) + "/s" }, q.etaSeconds?.takeIf { it > 0 }?.let { "ETA " + formatDuration(it) }, q.clientName,
        ).joinToString(" · "), color = Lc.muted, style = MaterialTheme.typography.bodySmall)
        if (q.message.isNotBlank()) Text(q.message, color = Lc.warning, style = MaterialTheme.typography.bodySmall)
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun QueueContent() {
    val a = api(); val nav = LocalNav.current
    val load = rememberLoad { a.queue() }
    val act = rememberActions()
    var removing by remember { mutableStateOf<QueueItem?>(null) }
    // Live progress while the screen is open.
    LaunchedEffect(load) { while (true) { delay(5000); load.reload() } }
    Loaded(load) { q ->
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(Edge), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            items(q.clients.filter { it.enabled && !it.status.ok }, key = { "c" + it.id }) { c ->
                Panel(tone = Tone.Danger) { Text(stringResource(R.string.client_down, c.name) + (c.status.error?.let { ": $it" } ?: ""), color = Lc.text, style = MaterialTheme.typography.bodySmall) }
            }
            if (q.items.isEmpty()) item { EmptyBox(stringResource(R.string.queue_empty)) }
            items(q.items, key = { it.id }) { item ->
                Readable {
                    Box {
                        QueueLine(item) { libraryRoute(item.item?.ref)?.let(nav::navigate) }
                        var menu by remember { mutableStateOf(false) }
                        IconButton(onClick = { menu = true }, Modifier.align(Alignment.TopEnd)) { Icon(Icons.Default.MoreVert, stringResource(R.string.actions)) }
                        DropdownMenu(menu, { menu = false }) {
                            if (item.state == "paused") DropdownMenuItem(text = { Text(stringResource(R.string.resume)) }, leadingIcon = { Icon(Icons.Default.PlayArrow, null) }, onClick = { menu = false; act.run(after = { load.reload() }) { a.resume(item.id) } })
                            else DropdownMenuItem(text = { Text(stringResource(R.string.pause)) }, leadingIcon = { Icon(Icons.Default.Pause, null) }, onClick = { menu = false; act.run(after = { load.reload() }) { a.pause(item.id) } })
                            if (q.clients.firstOrNull { it.id == item.clientId }?.canPrioritise != false) {
                                DropdownMenuItem(text = { Text(stringResource(R.string.move_top)) }, onClick = { menu = false; act.run(after = { load.reload() }) { a.priority(item.id, "top") } })
                                DropdownMenuItem(text = { Text(stringResource(R.string.move_last)) }, onClick = { menu = false; act.run(after = { load.reload() }) { a.priority(item.id, "last") } })
                            }
                            DropdownMenuItem(text = { Text(stringResource(R.string.remove), color = Lc.danger) }, onClick = { menu = false; removing = item })
                        }
                    }
                }
            }
        }
    }
    removing?.let { item ->
        var data by remember { mutableStateOf(false) }
        var block by remember { mutableStateOf(false) }
        val removed = stringResource(R.string.removed)
        AlertDialog(
            onDismissRequest = { removing = null }, containerColor = Lc.surface2,
            title = { Text(stringResource(R.string.remove_download)) },
            text = {
                Column {
                    Text(item.title, style = MaterialTheme.typography.bodySmall, color = Lc.muted)
                    Row(Modifier.clickable { data = !data }, verticalAlignment = Alignment.CenterVertically) { Checkbox(data, { data = it }); Text(stringResource(R.string.remove_data)) }
                    Row(Modifier.clickable { block = !block }, verticalAlignment = Alignment.CenterVertically) { Checkbox(block, { block = it }); Text(stringResource(R.string.remove_blocklist)) }
                }
            },
            confirmButton = { TextButton(onClick = { removing = null; act.run(removed, after = { load.reload() }) { a.removeDownload(item.id, data, block) } }) { Text(stringResource(R.string.remove), color = Lc.danger) } },
            dismissButton = { TextButton(onClick = { removing = null }) { Text(stringResource(R.string.cancel)) } },
        )
    }
}

// ---------- Board ("why didn't it land?") ----------

@Composable
fun boardGroupTitle(code: String) = codeText(LocalContext.current, "board.groups.$code.title", fallback = code.replace('_', ' '))

@Composable
private fun BoardGroupRow(g: BoardGroup, onClick: () -> Unit) {
    val ctx = LocalContext.current
    val tone = Tone.of(g.tone)
    Row(Modifier.fillMaxWidth().clip(Shape).background(Lc.surface).clickable(onClick = onClick).padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
        Dot(tone)
        Spacer(Modifier.width(10.dp))
        Column(Modifier.weight(1f)) {
            Text(boardGroupTitle(g.code), color = Lc.text, style = MaterialTheme.typography.bodyLarge)
            Text(codeText(ctx, "board.groups.${g.code}.hint"), color = Lc.muted, style = MaterialTheme.typography.bodySmall, maxLines = 2, overflow = TextOverflow.Ellipsis)
            if (g.reasons.isNotEmpty()) Text(g.reasons.take(2).joinToString(", ") { codeText(ctx, "rejectionCodes.${it.code}", fallback = it.example) + " ×" + it.count }, color = Lc.faint, style = MaterialTheme.typography.labelSmall)
        }
        Text(g.count.toString(), style = MaterialTheme.typography.titleLarge, color = tone.fg)
    }
}

@Composable
private fun BoardContent() {
    val a = api(); val nav = LocalNav.current
    val load = rememberLoad { a.board() }
    Loaded(load) { b ->
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(Edge), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            item { Text(stringResource(R.string.board_subtitle, b.total), color = Lc.muted, style = MaterialTheme.typography.bodySmall) }
            val groups = b.groups.filter { it.count > 0 }
            if (groups.isEmpty()) item { EmptyBox(stringResource(R.string.board_ok)) }
            items(groups, key = { it.code }) { g -> Readable { BoardGroupRow(g) { nav.navigate("board/${g.code}") } } }
        }
    }
}

fun boardItemText(ctx: android.content.Context, i: BoardItem): String =
    if (CODE_STRINGS.containsKey("board.itemCodes.${i.code}")) codeText(ctx, "board.itemCodes.${i.code}", i.params.texts() + mapOf("title" to i.title), i.message)
    else explanationText(ctx, Explanation(i.code, i.tone, i.message, i.params, i.reasons))

@Composable
fun BoardGroupScreen(code: String) {
    val a = api(); val nav = LocalNav.current
    val ctx = LocalContext.current
    val act = rememberActions()
    val pager = rememberPager(code) { off -> a.boardItems(code, off).let { it.items to (off + it.items.size < it.total) } }
    val searching = stringResource(R.string.search_started)
    Screen(boardGroupTitle(code), actions = {
        if (pager.items.isNotEmpty()) IconButton(onClick = { act.run(searching) { a.boardAction("search", pager.items.map { it.key }) } }) { Icon(Icons.Default.Search, stringResource(R.string.search_all_shown)) }
    }) {
        PagedList(pager, { it.key }, stringResource(R.string.board_ok), header = {
            item {
                val fix = pager.items.firstOrNull()?.fix
                Column(Modifier.padding(Edge), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    Text(codeText(ctx, "board.groups.$code.hint"), color = Lc.muted, style = MaterialTheme.typography.bodySmall)
                    if (!fix.isNullOrBlank()) Text(stringResource(R.string.how_to_fix) + ": " + codeText(ctx, "board.fix.$fix", fallback = fix), color = Lc.accent, style = MaterialTheme.typography.bodySmall)
                }
            }
        }) { i ->
            var menu by remember { mutableStateOf(false) }
            val done = stringResource(R.string.done)
            ListRow(i.title, subtitle = boardItemText(ctx, i), leading = { Dot(Tone.of(i.tone)) },
                trailing = {
                    Box {
                        IconButton(onClick = { menu = true }) { Icon(Icons.Default.MoreVert, stringResource(R.string.actions)) }
                        DropdownMenu(menu, { menu = false }) {
                            listOf("search" to R.string.search_now, "blocklist_retry" to R.string.blocklist_retry, "retry_import" to R.string.retry_import, "unmonitor" to R.string.unmonitor).forEach { (action, label) ->
                                DropdownMenuItem(text = { Text(stringResource(label)) }, onClick = { menu = false; act.run(done, after = { pager.refresh() }) { a.boardAction(action, listOf(i.key)) } })
                            }
                        }
                    }
                },
                onClick = { (libraryRoute(i.itemRef) ?: libraryRoute(i.parentRef))?.let(nav::navigate) })
        }
    }
}

// ---------- Wanted ----------

@Composable
private fun WantedContent() {
    val a = api(); val nav = LocalNav.current
    var kind by rememberSaveable { mutableStateOf("episodes_missing") }
    Column {
        FilterChips(listOf(
            "episodes_missing" to stringResource(R.string.wanted_episodes_missing), "episodes_cutoff" to stringResource(R.string.wanted_episodes_cutoff),
            "movies_missing" to stringResource(R.string.wanted_movies_missing), "movies_cutoff" to stringResource(R.string.wanted_movies_cutoff),
        ), kind, { kind = it }, Modifier.padding(vertical = 8.dp))
        if (kind.startsWith("episodes")) {
            val pager = rememberPager(kind) { off -> a.wantedEpisodes(if (kind == "episodes_missing") "missing" else "cutoff_unmet", off).let { it.items to (off + it.items.size < it.total) } }
            PagedList(pager, { it.id }, stringResource(R.string.wanted_empty)) { e ->
                ListRow(e.seriesTitle, subtitle = "S%02dE%02d · %s · %s".format(e.seasonNumber, e.episodeNumber, e.title, formatDate(e.airDate)),
                    leading = { Poster(a.imageUrl(e.poster), e.seriesTitle, Modifier.width(36.dp)) }, onClick = { nav.navigate("season/${e.seriesId}/${e.seasonNumber}") })
            }
        } else {
            val pager = rememberPager(kind) { off -> a.movies(null, if (kind == "movies_missing") "missing" else "cutoff_unmet", off).let { it.items to (off + it.items.size < it.total) } }
            PagedList(pager, { it.id }, stringResource(R.string.wanted_empty)) { m ->
                ListRow(m.title, subtitle = listOfNotNull(m.year.takeIf { it > 0 }?.toString(), m.file?.quality?.name).joinToString(" · "),
                    leading = { Poster(a.imageUrl(m.poster), m.title, Modifier.width(36.dp)) }, onClick = { nav.navigate("movie/${m.id}") })
            }
        }
    }
}

// ---------- Calendar ----------

@Composable
private fun CalendarLine(e: CalendarEntry) {
    val nav = LocalNav.current
    val a = api()
    val sub = if (e.kind == "episode") "S%02dE%02d".format(e.seasonNumber ?: 0, e.episodeNumber ?: 0) + (e.episodeTitle?.let { " · $it" } ?: "")
    else stringResource(when (e.kind) { "in_cinemas" -> R.string.in_cinemas; "digital" -> R.string.digital_release; else -> R.string.physical_release })
    ListRow(e.title, Modifier.offset(x = 0.dp), subtitle = sub + (e.airDateUtc?.let { " · " + formatDateTime(it).substringAfterLast(' ') } ?: ""),
        leading = { Poster(a.imageUrl(e.poster), e.title, Modifier.width(34.dp)) },
        trailing = { if (e.hasFile) Pill(stringResource(R.string.mstatus_downloaded), Tone.Success) else if (!e.monitored) Pill(stringResource(R.string.mstatus_unmonitored)) },
        onClick = { (e.movieId?.let { "movie/$it" } ?: e.seriesId?.let { "series/$it" })?.let(nav::navigate) })
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun CalendarContent() {
    val a = api()
    var weeks by rememberSaveable { mutableIntStateOf(4) }
    val today = LocalDate.now()
    val load = rememberLoad(weeks) { a.calendar(today.minusDays(3).toString(), today.plusWeeks(weeks.toLong()).toString()) }
    Loaded(load) { entries ->
        val byDay = entries.sortedWith(compareBy({ it.date }, { it.airDateUtc ?: "" })).groupBy { it.date }
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
            if (byDay.isEmpty()) item { EmptyBox(stringResource(R.string.calendar_empty)) }
            byDay.forEach { (day, list) ->
                item(key = "d$day") {
                    val d = runCatching { LocalDate.parse(day) }.getOrNull()
                    Text((if (d == today) stringResource(R.string.today) + " · " else "") + formatDate(day), style = Eyebrow, color = if (d == today) Lc.accent else Lc.muted,
                        modifier = Modifier.padding(horizontal = Edge, vertical = 10.dp))
                }
                items(list, key = { "${it.kind}-${it.movieId}-${it.episodeId}-${it.date}" }) { Readable { CalendarLine(it) } }
            }
            item { TextButton(onClick = { weeks += 4 }, Modifier.padding(horizontal = Edge)) { Text(stringResource(R.string.show_later)) } }
        }
    }
}

// ---------- History ----------

@Composable
private fun HistoryContent() {
    val a = api(); val nav = LocalNav.current
    var cat by rememberSaveable { mutableStateOf("all") }
    val pager = rememberPager(cat) { off -> a.history(cat.takeIf { it != "all" }, off).let { it.items to it.hasMore } }
    Column {
        FilterChips(listOf("all" to stringResource(R.string.filter_all), "grabbed" to stringResource(R.string.hist_grabbed), "imported" to stringResource(R.string.hist_imported),
            "upgraded" to stringResource(R.string.hist_upgraded), "failed" to stringResource(R.string.hist_failed), "deleted" to stringResource(R.string.hist_deleted), "subtitles" to stringResource(R.string.subtitles)),
            cat, { cat = it }, Modifier.padding(vertical = 8.dp))
        PagedList(pager, { it.id }, stringResource(R.string.history_empty)) { h ->
            val tone = when (h.category) { "imported", "upgraded" -> Tone.Success; "failed" -> Tone.Danger; "grabbed" -> Tone.Info; "deleted" -> Tone.Warning; else -> Tone.Neutral }
            ListRow(h.title, subtitle = listOfNotNull(formatDateTime(h.at), h.release, h.indexer, h.client, h.message.ifBlank { null }).joinToString(" · "),
                leading = { Dot(tone) }, onClick = { libraryRoute(h.itemRef)?.let(nav::navigate) })
        }
    }
}

// ---------- System (admins) ----------

@Composable
fun WebAction(route: String) {
    val ctx = LocalContext.current
    val a = api()
    IconButton(onClick = { openUrl(ctx, a.webUrl(route)) }) { Icon(Icons.AutoMirrored.Filled.OpenInNew, stringResource(R.string.open_in_web)) }
}

@Composable
fun HealthScreen() {
    val a = api(); val ctx = LocalContext.current
    val load = rememberLoad { a.health() }
    val act = rememberActions()
    Screen(stringResource(R.string.health), actions = {
        IconButton(onClick = { act.run { load.data = a.runHealth() } }, enabled = !act.busy) { Icon(Icons.Default.Refresh, stringResource(R.string.health_run)) }
    }) {
        Loaded(load) { h ->
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(Edge), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                item { Text(h.checkedAt?.let { stringResource(R.string.health_checked, formatDateTime(it)) } ?: "", color = Lc.faint, style = MaterialTheme.typography.bodySmall) }
                if (h.issues.isEmpty()) item { EmptyBox(stringResource(R.string.health_ok)) }
                items(h.issues, key = { it.key }) { i ->
                    Readable {
                        Panel(tone = Tone.of(i.severity)) {
                            Text(healthText(ctx, i), color = Lc.text)
                            Text(stringResource(R.string.since, formatDateTime(i.since)), color = Lc.faint, style = MaterialTheme.typography.bodySmall)
                            Row {
                                i.fixRoute?.let { r -> TextButton(onClick = { openUrl(ctx, a.webUrl(r)) }) { Text(stringResource(R.string.fix_in_web)) } }
                                if (i.docs.isNotBlank()) TextButton(onClick = { openUrl(ctx, if (i.docs.startsWith("http")) i.docs else a.webUrl(i.docs)) }) { Text(stringResource(R.string.how_to_fix)) }
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
fun IndexersScreen() {
    var tab by rememberSaveable { mutableIntStateOf(0) }
    Screen(stringResource(R.string.indexers), actions = { WebAction("indexers") }) {
        Column {
            TabRow(tab, containerColor = Lc.bg, contentColor = Lc.text, indicator = { pos -> TabRowDefaults.SecondaryIndicator(Modifier.tabIndicatorOffset(pos[tab]), color = Lc.accent) }) {
                listOf(R.string.indexers_yours, R.string.indexers_sites, R.string.indexers_usenet).forEachIndexed { i, l -> Tab(tab == i, { tab = i }, text = { Text(stringResource(l)) }) }
            }
            when (tab) { 0 -> YourIndexers(); 1 -> BuiltInSites(); else -> UsenetPresets() }
        }
    }
}

@Composable
private fun YourIndexers() {
    val a = api()
    val ctx = LocalContext.current
    val load = rememberLoad { a.indexers() }
    val act = rememberActions()
    val okText = stringResource(R.string.test_ok)
    Loaded(load) { list ->
        if (list.isEmpty()) { EmptyBox(stringResource(R.string.indexers_empty), detail = stringResource(R.string.indexers_empty_hint)); return@Loaded }
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
            item { Text(stringResource(R.string.edit_in_web_hint), color = Lc.faint, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(Edge)) }
            items(list, key = { it.id }) { i ->
                val tone = when (i.health.status) { "healthy" -> Tone.Success; "failing" -> Tone.Danger; "disabled" -> Tone.Warning; else -> Tone.Neutral }
                val err = i.health.lastErrorCode?.let { codeText(ctx, "indexers.errorKinds.$it", fallback = i.health.lastError ?: it) } ?: i.health.lastError
                Readable {
                    ListRow(i.name, subtitle = listOfNotNull(codeText(ctx, "indexers.kinds.${i.kind}", fallback = i.kind),
                        codeText(ctx, "indexers.health.${i.health.status}", fallback = i.health.status),
                        if (!i.enabled) stringResource(R.string.disabled) else null, err.takeIf { i.health.status == "failing" }).joinToString(" · "),
                        leading = { Dot(tone) },
                        trailing = { OutlinedButton(onClick = { act.runMsg(after = { load.reload() }) { val r = a.testIndexer(i.id); throwIfFailed(r.ok, r.error ?: r.errorCode); okText + " (${r.durationMs} ms)" } }, enabled = !act.busy, shape = Shape) { Text(stringResource(R.string.test)) } })
                }
            }
        }
    }
}

/** Public sites Locarr searches itself (built-in definitions). Adding one happens in the web. */
@Composable
private fun BuiltInSites() {
    val a = api()
    val ctx = LocalContext.current
    val load = rememberLoad { a.indexerCatalog() }
    Loaded(load) { c ->
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
            item { Text(stringResource(R.string.indexers_sites_hint), color = Lc.faint, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(Edge)) }
            items(c.errors, key = { "e" + it.file }) { e -> Readable { Box(Modifier.padding(horizontal = Edge, vertical = 4.dp)) { Panel(tone = Tone.Danger) { Text(e.file + ": " + e.message, style = MaterialTheme.typography.bodySmall) } } } }
            if (c.definitions.isEmpty()) item { EmptyBox(stringResource(R.string.nothing_found)) }
            items(c.definitions, key = { it.id }) { d ->
                Readable {
                    ListRow(d.name, subtitle = listOfNotNull(d.description.ifBlank { null }, d.language, if (d.flaresolverr == "always") stringResource(R.string.needs_flaresolverr) else null).joinToString(" · "),
                        leading = { Dot(if (d.indexerIds.isNotEmpty()) Tone.Success else Tone.Neutral) },
                        trailing = { if (d.indexerIds.isNotEmpty()) Pill(stringResource(R.string.in_use), Tone.Success) else if (!d.builtin) Pill(stringResource(R.string.custom)) },
                        onClick = { openUrl(ctx, a.webUrl("indexers")) })
                }
            }
        }
    }
}

@Composable
private fun UsenetPresets() {
    val a = api()
    val ctx = LocalContext.current
    val load = rememberLoad { a.indexerCatalog() }
    Loaded(load) { c ->
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
            item { Text(stringResource(R.string.indexers_usenet_hint), color = Lc.faint, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(Edge)) }
            items(c.presets, key = { it.id }) { p ->
                Readable { ListRow(p.name, subtitle = p.siteUrl, trailing = { Icon(Icons.AutoMirrored.Filled.OpenInNew, null, tint = Lc.muted) }, onClick = { openUrl(ctx, a.webUrl("indexers")) }) }
            }
        }
    }
}

/** A failed test becomes an error the snackbar shows. */
fun throwIfFailed(ok: Boolean, error: String?) { if (!ok) throw ApiException(400, "test_failed", error ?: "failed") }

@Composable
fun ClientsScreen() {
    val a = api()
    val load = rememberLoad { a.downloadClients() }
    val act = rememberActions()
    val okText = stringResource(R.string.test_ok)
    Screen(stringResource(R.string.download_clients), actions = { WebAction("downloaders") }) {
        Loaded(load) { list ->
            if (list.isEmpty()) { EmptyBox(stringResource(R.string.clients_empty), detail = stringResource(R.string.edit_in_web_hint)); return@Loaded }
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                item { Text(stringResource(R.string.edit_in_web_hint), color = Lc.faint, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(Edge)) }
                items(list, key = { it.id }) { c ->
                    Readable {
                        ListRow(c.name, subtitle = listOfNotNull(c.implementation, "${c.host}${c.port?.let { ":$it" } ?: ""}", if (!c.enabled) stringResource(R.string.disabled) else null,
                            c.status.version, c.status.freeSpaceBytes?.let { stringResource(R.string.free_space, formatBytes(it)) }, c.status.error).joinToString(" · "),
                            leading = { Dot(if (!c.enabled) Tone.Neutral else if (c.status.ok) Tone.Success else Tone.Danger) },
                            trailing = { OutlinedButton(onClick = { act.runMsg(after = { load.reload() }) { val r = a.testDownloadClient(c); throwIfFailed(r.ok, r.message ?: r.errorCode); okText + (r.version?.let { " · $it" } ?: "") } }, enabled = !act.busy, shape = Shape) { Text(stringResource(R.string.test)) } })
                    }
                }
            }
        }
    }
}

@Composable
fun FoldersScreen() {
    val a = api()
    val load = rememberLoad { a.rootFolders() }
    Screen(stringResource(R.string.media_folders), actions = { WebAction("settings/media") }) {
        Loaded(load) { list ->
            if (list.isEmpty()) { EmptyBox(stringResource(R.string.folders_empty), detail = stringResource(R.string.edit_in_web_hint)); return@Loaded }
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(Edge), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                items(list, key = { it.id }) { f ->
                    Readable {
                        Panel(tone = if (!f.accessible || !f.writable) Tone.Danger else null) {
                            Text(f.path, color = Lc.text, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodyMedium)
                            Text(stringResource(when (f.kind) { "series" -> R.string.kind_series; "music" -> R.string.kind_music; else -> R.string.kind_movies }), color = Lc.muted, style = MaterialTheme.typography.bodySmall)
                            val total = f.totalSpaceBytes; val free = f.freeSpaceBytes
                            if (total != null && free != null && total > 0) {
                                Spacer(Modifier.height(6.dp))
                                val used = 1f - free.toFloat() / total
                                ProgressBar(used, color = if (used > 0.95f) Lc.danger else if (used > 0.85f) Lc.warning else Lc.accent)
                                Text(stringResource(R.string.free_of, formatBytes(free), formatBytes(total)), color = Lc.muted, style = MaterialTheme.typography.bodySmall)
                            }
                            if (!f.accessible) Text(stringResource(R.string.folder_missing), color = Lc.danger, style = MaterialTheme.typography.bodySmall)
                            else if (!f.writable) Text(stringResource(R.string.folder_readonly) + (f.notWritableDetail?.let { ": $it" } ?: ""), color = Lc.danger, style = MaterialTheme.typography.bodySmall)
                        }
                    }
                }
            }
        }
    }
}
