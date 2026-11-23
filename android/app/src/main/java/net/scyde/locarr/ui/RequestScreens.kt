package net.scyde.locarr.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.DoneAll
import androidx.compose.material3.*
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import net.scyde.locarr.R
import net.scyde.locarr.api.Comment
import net.scyde.locarr.api.Issue
import net.scyde.locarr.api.MediaRequest
import net.scyde.locarr.api.Notification

/** A paged list with pull to refresh, loading, error and empty states. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun <T> PagedList(
    pager: Pager<T>, key: (T) -> Any, empty: String, modifier: Modifier = Modifier,
    header: (androidx.compose.foundation.lazy.LazyListScope.() -> Unit)? = null, row: @Composable (T) -> Unit,
) {
    val list = rememberLazyListState()
    PageEnd(pager, list)
    PullToRefreshBox(pager.refreshing, { pager.refresh() }, modifier.fillMaxSize()) {
        LazyColumn(Modifier.fillMaxSize(), state = list, contentPadding = PaddingValues(bottom = 24.dp)) {
            header?.invoke(this)
            items(pager.items, key = key) { Readable { row(it) } }
            item {
                when {
                    pager.error != null -> ErrorBox(pager.error!!, { pager.error = null; if (pager.items.isEmpty()) pager.refresh() else pager.loadMore() })
                    pager.loading && !pager.refreshing -> Loading()
                    pager.started && pager.items.isEmpty() -> EmptyBox(empty)
                }
            }
        }
    }
}

@Composable
fun RequestsScreen(tab: Boolean) {
    val a = api()
    val me = LocalState.current.me
    val manager = me?.canManageRequests == true
    var mine by rememberSaveable { mutableStateOf(!manager) }
    var status by rememberSaveable { mutableStateOf(if (manager) "pending" else "all") }
    val pager = rememberPager(mine, status, pageSize = 30) { off -> a.requests(status, mine, off, 30).let { it.items to (off + it.items.size < it.total) } }
    val act = rememberActions()
    val approvedAll = stringResource(R.string.approved_all)
    Screen(stringResource(R.string.requests_title), back = !tab, actions = {
        if (manager && status == "pending" && pager.items.any { it.status == "pending" }) IconButton(onClick = {
            act.run(approvedAll, after = { pager.refresh() }) { a.approveAll(pager.items.filter { it.status == "pending" }.map { it.id }) }
        }) { Icon(Icons.Default.DoneAll, stringResource(R.string.approve_all)) }
    }) {
        Column {
            if (manager) FilterChips(listOf("all" to stringResource(R.string.requests_everyone), "mine" to stringResource(R.string.requests_mine)), if (mine) "mine" else "all", { mine = it == "mine" }, Modifier.padding(top = 4.dp))
            FilterChips(listOf(
                "pending" to stringResource(R.string.rfilter_pending), "active" to stringResource(R.string.rfilter_active),
                "available" to stringResource(R.string.rfilter_available), "failed" to stringResource(R.string.rfilter_failed), "all" to stringResource(R.string.filter_all),
            ), status, { status = it }, Modifier.padding(vertical = 4.dp))
            PagedList(pager, { it.id }, stringResource(R.string.requests_empty)) { r -> RequestRow(r, manager) { pager.refresh() } }
        }
    }
}

@Composable
fun RequestRow(r: MediaRequest, manager: Boolean, changed: () -> Unit) {
    val a = api()
    val nav = LocalNav.current
    val ctx = LocalContext.current
    val act = rememberActions()
    var declining by remember { mutableStateOf(false) }
    val music = r.kind == "album" || r.kind == "artist"
    Row(Modifier.fillMaxWidth().clickable { nav.navigate("request/${r.id}") }.padding(horizontal = Edge, vertical = 10.dp)) {
        Poster(a.imageUrl(r.poster, music = music), r.title, Modifier.width(54.dp), square = music)
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(r.title + if (r.year > 0) " (${r.year})" else "", color = Lc.text, style = MaterialTheme.typography.bodyLarge, maxLines = 2, overflow = TextOverflow.Ellipsis)
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                Pill(requestStatusLabel(r.status), requestStatusTone(r.status))
                if (r.is4k) Pill("4K", Tone.Info)
                Text(listOfNotNull(r.requestedBy.username.takeIf { manager }, formatDate(r.createdAt)).joinToString(" · "), color = Lc.faint, style = MaterialTheme.typography.bodySmall, maxLines = 1)
            }
            if (r.seasons.isNotEmpty()) Text(seasonsText(r), color = Lc.muted, style = MaterialTheme.typography.bodySmall)
            r.explanation?.takeIf { r.status !in setOf("available", "declined", "cancelled") }?.let { Text(explanationText(ctx, it), color = Lc.muted, style = MaterialTheme.typography.bodySmall, maxLines = 3, overflow = TextOverflow.Ellipsis) }
            r.progress?.takeIf { it.percent > 0 && r.status in setOf("processing", "partial", "approved") }?.let { ProgressBar(it.percent.toFloat()) }
            if (manager && r.status == "pending") Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                val approved = stringResource(R.string.approved)
                Button(onClick = { act.run(approved, after = changed) { a.approve(r.id) } }, enabled = !act.busy, shape = Shape, contentPadding = PaddingValues(horizontal = 14.dp)) { Text(stringResource(R.string.approve)) }
                OutlinedButton(onClick = { declining = true }, enabled = !act.busy, shape = Shape) { Text(stringResource(R.string.decline)) }
            }
        }
    }
    if (declining) DeclineDialog(r.id, { declining = false }, changed)
}

@Composable
fun seasonsText(r: MediaRequest): String =
    if (r.allSeasons) stringResource(R.string.all_seasons) else stringResource(R.string.seasons_list, r.seasons.joinToString(", ") { it.number.toString() })

@Composable
private fun DeclineDialog(id: Long, onDismiss: () -> Unit, changed: () -> Unit) {
    val a = api()
    val act = rememberActions()
    var reason by remember { mutableStateOf("") }
    val declined = stringResource(R.string.declined)
    AlertDialog(
        onDismissRequest = onDismiss, containerColor = Lc.surface2,
        title = { Text(stringResource(R.string.decline)) },
        text = { OutlinedTextField(reason, { reason = it }, label = { Text(stringResource(R.string.decline_reason)) }, minLines = 2, modifier = Modifier.fillMaxWidth(), colors = fieldColors()) },
        confirmButton = { TextButton(onClick = { act.run(declined, after = { onDismiss(); changed() }) { a.decline(id, reason) } }) { Text(stringResource(R.string.decline), color = Lc.danger) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.cancel)) } },
    )
}

@Composable
fun RequestDetailScreen(id: Long) {
    val a = api()
    val nav = LocalNav.current
    val ctx = LocalContext.current
    val me = LocalState.current.me
    val load = rememberLoad(id) { a.request(id) to a.requestComments(id) }
    val act = rememberActions()
    var declining by remember { mutableStateOf(false) }
    Screen(load.data?.first?.title ?: stringResource(R.string.request)) {
        Loaded(load) { (r, comments) ->
            val manager = me?.canManageRequests == true
            Readable {
                Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(Edge), verticalArrangement = Arrangement.spacedBy(14.dp)) {
                    val music = r.kind == "album" || r.kind == "artist"
                    Row(horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                        Poster(a.imageUrl(r.poster, music = music), r.title, Modifier.width(100.dp), square = music)
                        Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                            Text(r.title, style = MaterialTheme.typography.headlineSmall)
                            r.subtitle?.let { Text(it, color = Lc.muted) }
                            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) { Pill(requestStatusLabel(r.status), requestStatusTone(r.status)); if (r.is4k) Pill("4K", Tone.Info) }
                        }
                    }
                    r.explanation?.let { ex ->
                        Panel(tone = Tone.of(ex.tone)) {
                            Text(stringResource(R.string.where_is_it), style = Eyebrow, color = Tone.of(ex.tone).fg)
                            Spacer(Modifier.height(4.dp))
                            Text(explanationText(ctx, ex), color = Lc.text)
                            r.progress?.takeIf { it.percent > 0 }?.let { p -> Spacer(Modifier.height(8.dp)); ProgressBar(p.percent.toFloat()); p.etaSeconds?.let { Text(stringResource(R.string.eta, formatDuration(it.toLong())), color = Lc.faint, style = MaterialTheme.typography.bodySmall) } }
                        }
                    }
                    Panel {
                        KeyValue(stringResource(R.string.requested_by), r.requestedBy.username)
                        KeyValue(stringResource(R.string.requested_at), formatDateTime(r.createdAt))
                        if (r.seasons.isNotEmpty() || r.allSeasons) KeyValue(stringResource(R.string.seasons), seasonsText(r))
                        r.decidedBy?.let { KeyValue(stringResource(if (r.status == "declined") R.string.declined_by else R.string.approved_by), it.username + if (r.autoApproved) " · " + stringResource(R.string.auto) else "") }
                        r.declineReason?.let { KeyValue(stringResource(R.string.decline_reason), it) }
                        r.availableAt?.let { KeyValue(stringResource(R.string.available_at), formatDateTime(it)) }
                    }
                    FlowRowCompat {
                        val approved = stringResource(R.string.approved)
                        val withdrawn = stringResource(R.string.withdrawn)
                        val retrying = stringResource(R.string.retry_queued)
                        if (manager && r.status == "pending") {
                            Button(onClick = { act.run(approved, after = { load.reload() }) { a.approve(r.id) } }, shape = Shape) { Text(stringResource(R.string.approve)) }
                            OutlinedButton(onClick = { declining = true }, shape = Shape) { Text(stringResource(R.string.decline)) }
                        }
                        if (r.canCancel) OutlinedButton(onClick = { act.run(withdrawn, after = { load.reload() }) { a.cancelRequest(r.id) } }, shape = Shape) { Text(stringResource(R.string.withdraw)) }
                        if (manager && r.status == "failed") OutlinedButton(onClick = { act.run(retrying, after = { load.reload() }) { a.retryRequest(r.id) } }, shape = Shape) { Text(stringResource(R.string.retry_now)) }
                        val title = when (r.kind) { "movie" -> r.tmdbId?.let { "title/movie/$it" }; "series" -> r.tmdbId?.let { "title/series/$it" }; else -> (r.artistMbid ?: r.mbid)?.let { "title/artist/$it" } }
                        if (title != null) TextButton(onClick = { nav.navigate(title) }) { Text(stringResource(R.string.open_title)) }
                        if (me?.canManageLibrary == true) libraryRoute(r.libraryRef)?.let { lr -> TextButton(onClick = { nav.navigate(lr) }) { Text(stringResource(R.string.open_in_library)) } }
                    }
                    Comments(comments) { body -> a.commentRequest(r.id, body); load.reload() }
                }
            }
            if (declining) DeclineDialog(r.id, { declining = false }) { load.reload() }
        }
    }
}

@Composable
fun Comments(comments: List<Comment>, send: suspend (String) -> Unit) {
    val act = rememberActions()
    var text by remember { mutableStateOf("") }
    Text(stringResource(R.string.comments), style = MaterialTheme.typography.titleMedium)
    if (comments.isEmpty()) Text(stringResource(R.string.no_comments), color = Lc.faint, style = MaterialTheme.typography.bodySmall)
    comments.forEach { c ->
        Column(Modifier.fillMaxWidth().clip(Shape).background(Lc.surface).padding(12.dp)) {
            Text(listOfNotNull(c.who?.username, formatDateTime(c.createdAt)).joinToString(" · "), color = Lc.faint, style = MaterialTheme.typography.labelSmall)
            Text(c.body, color = Lc.text, style = MaterialTheme.typography.bodyMedium)
        }
    }
    OutlinedTextField(
        text, { text = it }, placeholder = { Text(stringResource(R.string.comment_hint)) }, modifier = Modifier.fillMaxWidth(), colors = fieldColors(),
        trailingIcon = { IconButton(enabled = text.isNotBlank() && !act.busy, onClick = { val t = text.trim(); act.run(after = { text = "" }) { send(t) } }) { Icon(Icons.AutoMirrored.Filled.Send, stringResource(R.string.send)) } },
    )
}

@Composable
fun issueTypeLabel(t: String) = stringResource(when (t) { "video" -> R.string.issue_video; "audio" -> R.string.issue_audio; "subtitles" -> R.string.issue_subtitles; else -> R.string.issue_other })

@Composable
fun IssuesScreen() {
    val a = api()
    var status by rememberSaveable { mutableStateOf("open") }
    val pager = rememberPager(status, pageSize = 30) { off -> a.issues(status.takeIf { it != "all" }, off, 30).let { it.items to (off + it.items.size < it.total) } }
    val nav = LocalNav.current
    Screen(stringResource(R.string.issues_title)) {
        Column {
            FilterChips(listOf("open" to stringResource(R.string.issues_open), "resolved" to stringResource(R.string.issues_resolved), "all" to stringResource(R.string.filter_all)), status, { status = it }, Modifier.padding(vertical = 4.dp))
            PagedList(pager, { it.id }, stringResource(R.string.issues_empty)) { i -> IssueRow(i) { nav.navigate("issue/${i.id}") } }
        }
    }
}

@Composable
private fun IssueRow(i: Issue, onClick: () -> Unit) {
    ListRow(
        i.title + episodeSuffix(i), subtitle = issueTypeLabel(i.type) + " · " + i.reportedBy.username + " · " + formatDate(i.createdAt) + "\n" + i.message,
        leading = { Dot(if (i.status == "open") Tone.Warning else Tone.Success) }, onClick = onClick,
        trailing = { if (i.commentCount > 0) Pill(i.commentCount.toString()) },
    )
}

private fun episodeSuffix(i: Issue) = when {
    i.seasonNumber != null && i.episodeNumber != null -> " · S%02dE%02d".format(i.seasonNumber, i.episodeNumber)
    i.seasonNumber != null -> " · S%02d".format(i.seasonNumber)
    else -> ""
}

@Composable
fun IssueDetailScreen(id: Long) {
    val a = api()
    val nav = LocalNav.current
    val me = LocalState.current.me
    val load = rememberLoad(id) { a.issue(id) to a.issueComments(id) }
    val act = rememberActions()
    var resolving by remember { mutableStateOf(false) }
    Screen(load.data?.first?.title ?: stringResource(R.string.issue)) {
        Loaded(load) { (i, comments) ->
            Readable {
                Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(Edge), verticalArrangement = Arrangement.spacedBy(14.dp)) {
                    Row(horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                        Poster(a.imageUrl(i.poster, music = i.kind == "album"), i.title, Modifier.width(90.dp), square = i.kind == "album")
                        Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                            Text(i.title + episodeSuffix(i), style = MaterialTheme.typography.titleLarge)
                            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                                Pill(stringResource(if (i.status == "open") R.string.issues_open else R.string.issues_resolved), if (i.status == "open") Tone.Warning else Tone.Success)
                                Pill(issueTypeLabel(i.type))
                            }
                            Text(stringResource(R.string.reported_by, i.reportedBy.username, formatDateTime(i.createdAt)), color = Lc.faint, style = MaterialTheme.typography.bodySmall)
                        }
                    }
                    Panel { Text(i.message, color = Lc.text) }
                    FlowRowCompat {
                        val reopened = stringResource(R.string.reopened)
                        if (me?.canManageRequests == true && i.status == "open") Button(onClick = { resolving = true }, shape = Shape) { Text(stringResource(R.string.resolve)) }
                        if (me?.canManageRequests == true && i.status == "resolved") OutlinedButton(onClick = { act.run(reopened, after = { load.reload() }) { a.reopenIssue(i.id) } }, shape = Shape) { Text(stringResource(R.string.reopen)) }
                        if (me?.canManageLibrary == true) libraryRoute(i.libraryRef)?.let { lr -> TextButton(onClick = { nav.navigate(lr) }) { Text(stringResource(R.string.open_in_library)) } }
                    }
                    Comments(comments) { body -> a.commentIssue(i.id, body); load.reload() }
                }
            }
            if (resolving) {
                var search by remember { mutableStateOf(false) }
                val resolved = stringResource(R.string.resolved)
                AlertDialog(
                    onDismissRequest = { resolving = false }, containerColor = Lc.surface2,
                    title = { Text(stringResource(R.string.resolve)) },
                    text = { Row(Modifier.clickable { search = !search }, verticalAlignment = Alignment.CenterVertically) { Checkbox(search, { search = it }); Text(stringResource(R.string.resolve_search)) } },
                    confirmButton = { TextButton(onClick = { resolving = false; act.run(resolved, after = { load.reload() }) { a.resolveIssue(i.id, search) } }) { Text(stringResource(R.string.resolve)) } },
                    dismissButton = { TextButton(onClick = { resolving = false }) { Text(stringResource(R.string.cancel)) } },
                )
            }
        }
    }
}

@Composable
fun NotificationsScreen() {
    val a = api()
    val state = LocalState.current
    val nav = LocalNav.current
    val scope = rememberCoroutineScope()
    val load = rememberLoad { a.notifications(limit = 100) }
    Screen(stringResource(R.string.notifications), bell = false, actions = {
        if ((load.data?.unread ?: 0) > 0) TextButton(onClick = { scope.launch { runCatching { a.markRead(all = true) }; state.refreshUnread(); load.reload() } }) { Text(stringResource(R.string.mark_all_read)) }
    }) {
        Loaded(load) { list ->
            if (list.items.isEmpty()) { EmptyBox(stringResource(R.string.notifications_empty), detail = stringResource(R.string.notifications_empty_hint)); return@Loaded }
            LazyColumn(Modifier.fillMaxSize()) {
                items(list.items, key = { it.id }) { n ->
                    Readable { NotificationRow(n) {
                        scope.launch { if (!n.read) { runCatching { a.markRead(listOf(n.id)) }; state.refreshUnread() } }
                        nav.navigate(net.scyde.locarr.notify.PollLogic.routeFor(n))
                    } }
                }
            }
        }
    }
}

@Composable
private fun NotificationRow(n: Notification, onClick: () -> Unit) {
    val a = api()
    val ctx = LocalContext.current
    Row(Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = Edge, vertical = 12.dp), verticalAlignment = Alignment.Top) {
        Poster(a.imageUrl(n.poster, music = n.mediaKind == "album" || n.mediaKind == "artist"), n.title, Modifier.width(40.dp))
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f)) {
            Text(notificationText(ctx, n), color = Lc.text, style = MaterialTheme.typography.bodyMedium, fontWeight = if (n.read) FontWeight.Normal else FontWeight.SemiBold)
            Text(formatDateTime(n.createdAt), color = Lc.faint, style = MaterialTheme.typography.bodySmall)
        }
        if (!n.read) Dot(Tone.Success, Modifier.padding(top = 6.dp))
    }
}
