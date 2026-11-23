package net.scyde.locarr.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.ErrorOutline
import androidx.compose.material.icons.filled.Inbox
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.filled.Search
import androidx.compose.foundation.horizontalScroll
import androidx.compose.material3.*
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import coil.ImageLoader
import coil.compose.AsyncImage
import coil.request.ImageRequest
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import net.scyde.locarr.AppState
import net.scyde.locarr.R
import net.scyde.locarr.api.Api
import net.scyde.locarr.api.ApiException
import java.io.IOException

val LocalImages = staticCompositionLocalOf<ImageLoader?> { null }
val LocalState = staticCompositionLocalOf<AppState> { error("no state") }
val LocalNav = staticCompositionLocalOf<NavHostController> { error("no nav") }
val LocalSnack = staticCompositionLocalOf { SnackbarHostState() }

val Shape = RoundedCornerShape(8.dp)
val SmallShape = RoundedCornerShape(6.dp)
val Edge = 16.dp

@Composable fun api(): Api = LocalState.current.api!!

/** Phones get one column; wider windows (tablets, unfolded phones) more room. */
@Composable fun isWide(): Boolean = LocalConfiguration.current.screenWidthDp >= 600

/** An image through the server's cache, loaded with the session. */
@Composable
fun RemoteImage(url: String?, modifier: Modifier = Modifier, contentScale: ContentScale = ContentScale.Crop) {
    val loader = LocalImages.current
    if (loader == null || url == null) { Box(modifier.background(Lc.surface2)); return }
    AsyncImage(
        model = ImageRequest.Builder(LocalContext.current).data(url).crossfade(true).build(),
        imageLoader = loader, contentDescription = null, contentScale = contentScale, modifier = modifier.background(Lc.surface2),
    )
}

/** A 2:3 poster with the title written on it while there is no image. */
@Composable
fun Poster(url: String?, title: String, modifier: Modifier = Modifier, square: Boolean = false) {
    Box(modifier.aspectRatio(if (square) 1f else 2f / 3f).clip(SmallShape).background(Lc.surface2)) {
        if (url == null) Text(title, color = Lc.muted, style = MaterialTheme.typography.labelMedium, modifier = Modifier.align(Alignment.BottomStart).padding(8.dp), maxLines = 4)
        else RemoteImage(url, Modifier.fillMaxSize())
    }
}

@Composable
fun Pill(text: String, tone: Tone = Tone.Neutral, modifier: Modifier = Modifier) {
    Text(text, color = tone.fg, style = MaterialTheme.typography.labelSmall, fontWeight = FontWeight.SemiBold, maxLines = 1,
        modifier = modifier.background(tone.bg, RoundedCornerShape(50)).padding(horizontal = 8.dp, vertical = 3.dp))
}

@Composable
fun Dot(tone: Tone, modifier: Modifier = Modifier) = Box(modifier.size(8.dp).background(tone.fg, CircleShape))

@Composable
fun SectionTitle(text: String, modifier: Modifier = Modifier, action: (@Composable () -> Unit)? = null) {
    Row(modifier.fillMaxWidth().padding(horizontal = Edge, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(text, style = MaterialTheme.typography.titleMedium, color = Lc.text, modifier = Modifier.weight(1f).semantics { heading() })
        action?.invoke()
    }
}

/** A bordered surface for grouped content. */
@Composable
fun Panel(modifier: Modifier = Modifier, tone: Tone? = null, content: @Composable ColumnScope.() -> Unit) {
    Surface(modifier.fillMaxWidth(), shape = Shape, color = tone?.bg ?: Lc.surface, border = BorderStroke(1.dp, if (tone != null) tone.fg.copy(alpha = 0.35f) else Lc.border)) {
        Column(Modifier.padding(14.dp), content = content)
    }
}

@Composable
fun KeyValue(label: String, value: String?) {
    if (value.isNullOrBlank()) return
    Row(Modifier.fillMaxWidth().padding(vertical = 3.dp)) {
        Text(label, color = Lc.muted, style = MaterialTheme.typography.bodySmall, modifier = Modifier.width(120.dp))
        Text(value, color = Lc.text, style = MaterialTheme.typography.bodySmall, modifier = Modifier.weight(1f))
    }
}

@Composable
fun ListRow(
    title: String, modifier: Modifier = Modifier, subtitle: String? = null, leading: (@Composable () -> Unit)? = null,
    trailing: (@Composable () -> Unit)? = null, onClick: (() -> Unit)? = null,
) {
    Row(
        modifier.fillMaxWidth().then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier)
            .heightIn(min = 56.dp).padding(horizontal = Edge, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (leading != null) { leading(); Spacer(Modifier.width(12.dp)) }
        Column(Modifier.weight(1f)) {
            Text(title, color = Lc.text, style = MaterialTheme.typography.bodyLarge, maxLines = 2, overflow = TextOverflow.Ellipsis)
            if (!subtitle.isNullOrBlank()) Text(subtitle, color = Lc.muted, style = MaterialTheme.typography.bodySmall, maxLines = 3, overflow = TextOverflow.Ellipsis)
        }
        if (trailing != null) { Spacer(Modifier.width(8.dp)); trailing() }
    }
}

@Composable
fun MessageBox(icon: ImageVector, text: String, modifier: Modifier = Modifier, detail: String? = null, action: (@Composable () -> Unit)? = null) {
    Column(modifier.fillMaxWidth().padding(32.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Icon(icon, null, tint = Lc.faint, modifier = Modifier.size(40.dp))
        Text(text, color = Lc.text, style = MaterialTheme.typography.titleMedium, textAlign = TextAlign.Center)
        if (!detail.isNullOrBlank()) Text(detail, color = Lc.muted, style = MaterialTheme.typography.bodySmall, textAlign = TextAlign.Center)
        action?.invoke()
    }
}

@Composable
fun EmptyBox(text: String, modifier: Modifier = Modifier, detail: String? = null) = MessageBox(Icons.Default.Inbox, text, modifier, detail)

@Composable
fun ErrorBox(error: Throwable, onRetry: (() -> Unit)?, modifier: Modifier = Modifier) {
    MessageBox(Icons.Default.ErrorOutline, errorText(error), modifier, detail = (error as? ApiException)?.message?.takeIf { error.code !in KNOWN_ERRORS }) {
        if (onRetry != null) OutlinedButton(onClick = onRetry) { Text(stringResource(R.string.retry)) }
    }
}

@Composable
fun Loading(modifier: Modifier = Modifier) {
    Box(modifier.fillMaxWidth().padding(48.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator(color = Lc.accent) }
}

private val KNOWN_ERRORS = setOf("invalid_credentials", "totp_required", "invalid_totp", "forbidden", "not_found", "rate_limited", "identity_not_linked", "account_locked", "provider_unavailable", "quota_exceeded")

/** A short sentence for an error in the user's language. */
@Composable
fun errorText(e: Throwable): String {
    val res = when {
        e is ApiException -> when (e.code) {
            "invalid_credentials" -> R.string.err_credentials
            "invalid_totp" -> R.string.err_totp
            "forbidden" -> R.string.err_forbidden
            "not_found" -> R.string.err_not_found
            "rate_limited", "account_locked" -> R.string.err_rate_limited
            "identity_not_linked" -> R.string.err_not_linked
            "provider_unavailable" -> R.string.err_provider_unavailable
            "quota_exceeded" -> R.string.err_quota
            else -> if (e.status == 401) R.string.err_session else if (e.status >= 500) R.string.err_server else null
        }
        e is IOException -> R.string.err_network
        else -> null
    }
    return if (res != null) stringResource(res) else (e.message ?: stringResource(R.string.err_generic))
}

/** One async value with loading, error and reload: the state behind most screens. */
class Load<T>(private val scope: CoroutineScope, private val block: suspend () -> T) {
    var data by mutableStateOf<T?>(null)
    var error by mutableStateOf<Throwable?>(null)
    var loading by mutableStateOf(false)

    fun reload() {
        loading = true
        scope.launch {
            try { data = block(); error = null } catch (e: CancellationException) { throw e } catch (e: Exception) { error = e } finally { loading = false }
        }
    }
}

@Composable
fun <T> rememberLoad(vararg keys: Any?, block: suspend () -> T): Load<T> {
    val scope = rememberCoroutineScope()
    val load = remember(*keys) { Load(scope, block) }
    LaunchedEffect(load) { load.reload() }
    return load
}

/** Shows [load]'s data with pull to refresh, or its first-load spinner or error. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun <T> Loaded(load: Load<T>, modifier: Modifier = Modifier, content: @Composable (T) -> Unit) {
    val d = load.data
    PullToRefreshBox(isRefreshing = load.loading && d != null, onRefresh = { load.reload() }, modifier = modifier.fillMaxSize()) {
        when {
            d != null -> content(d)
            load.error != null -> ErrorBox(load.error!!, { load.reload() })
            else -> Loading()
        }
    }
}

/** Offset paging: [fetch] returns a page and whether more exist; [loadMore] is called near the end. */
class Pager<T>(private val scope: CoroutineScope, private val pageSize: Int, private val fetch: suspend (offset: Int) -> Pair<List<T>, Boolean>) {
    var items by mutableStateOf<List<T>>(emptyList())
    var error by mutableStateOf<Throwable?>(null)
    var loading by mutableStateOf(false)
    var refreshing by mutableStateOf(false)
    var done by mutableStateOf(false)
    var started by mutableStateOf(false)

    fun refresh() {
        refreshing = started
        loading = true
        scope.launch {
            try {
                val (page, more) = fetch(0)
                items = page; done = !more; error = null
            } catch (e: CancellationException) { throw e } catch (e: Exception) { error = e } finally { loading = false; refreshing = false; started = true }
        }
    }

    fun loadMore() {
        if (loading || done || error != null) return
        loading = true
        scope.launch {
            try {
                val (page, more) = fetch(items.size)
                items = items + page; done = !more || page.isEmpty()
            } catch (e: CancellationException) { throw e } catch (e: Exception) { error = e } finally { loading = false }
        }
    }

    companion object { const val PAGE = 50 }
}

@Composable
fun <T> rememberPager(vararg keys: Any?, pageSize: Int = Pager.PAGE, fetch: suspend (offset: Int) -> Pair<List<T>, Boolean>): Pager<T> {
    val scope = rememberCoroutineScope()
    val p = remember(*keys) { Pager(scope, pageSize, fetch) }
    LaunchedEffect(p) { p.refresh() }
    return p
}

/** Calls [Pager.loadMore] when the list scrolls near its end. */
@Composable
fun <T> PageEnd(pager: Pager<T>, state: LazyListState) {
    val near by remember(state) { derivedStateOf { val last = state.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0; last >= state.layoutInfo.totalItemsCount - 6 } }
    LaunchedEffect(near, pager.items.size) { if (near && pager.started) pager.loadMore() }
}

/** Runs a server action and reports the outcome in the snackbar. */
class Actions(private val scope: CoroutineScope, private val snack: SnackbarHostState, private val errorOf: (Throwable) -> String) {
    var busy by mutableStateOf(false)
    /** Like [run], but [block] returns the message to show (a test result). */
    fun runMsg(after: () -> Unit = {}, block: suspend () -> String) {
        if (busy) return
        busy = true
        scope.launch {
            try { val m = block(); after(); snack.showSnackbar(m) }
            catch (e: CancellationException) { throw e } catch (e: Exception) { snack.showSnackbar(errorOf(e)) }
            finally { busy = false }
        }
    }

    fun run(done: String? = null, after: () -> Unit = {}, block: suspend () -> Unit) {
        if (busy) return
        busy = true
        scope.launch {
            try {
                block(); after()
                if (done != null) snack.showSnackbar(done)
            } catch (e: CancellationException) { throw e } catch (e: Exception) {
                snack.showSnackbar(errorOf(e))
            } finally { busy = false }
        }
    }
}

@Composable
fun rememberActions(): Actions {
    val scope = rememberCoroutineScope()
    val snack = LocalSnack.current
    val ctx = LocalContext.current
    return remember { Actions(scope, snack) { e -> plainErrorText(ctx, e) } }
}

/** [errorText] outside of composition. */
fun plainErrorText(ctx: android.content.Context, e: Throwable): String {
    val res = when {
        e is ApiException && e.code == "forbidden" -> R.string.err_forbidden
        e is ApiException && e.code == "not_found" -> R.string.err_not_found
        e is ApiException && e.code == "quota_exceeded" -> R.string.err_quota
        e is ApiException && e.status >= 500 -> R.string.err_server
        e is IOException -> R.string.err_network
        else -> null
    }
    return if (res != null) ctx.getString(res) else (e.message ?: ctx.getString(R.string.err_generic))
}

/** The top bar of every screen: back (when not a tab root), title, the notification bell, actions. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun Screen(
    title: String, back: Boolean = true, bell: Boolean = true,
    actions: @Composable RowScope.() -> Unit = {}, fab: @Composable () -> Unit = {},
    content: @Composable (PaddingValues) -> Unit,
) {
    val nav = LocalNav.current
    val state = LocalState.current
    Scaffold(
        containerColor = Lc.bg,
        contentWindowInsets = WindowInsets(0, 0, 0, 0),
        floatingActionButton = fab,
        topBar = {
            TopAppBar(
                title = { Text(title, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                navigationIcon = { if (back) IconButton(onClick = { nav.popBackStack() }) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back)) } },
                actions = {
                    actions()
                    if (bell) IconButton(onClick = { nav.navigate("notifications") }) {
                        BadgedBox(badge = { if (state.unread > 0) Badge(containerColor = Lc.accent, contentColor = Lc.accentInk) { Text(if (state.unread > 99) "99+" else state.unread.toString()) } }) {
                            Icon(Icons.Default.Notifications, stringResource(R.string.notifications))
                        }
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(containerColor = Lc.bg, titleContentColor = Lc.text, navigationIconContentColor = Lc.text, actionIconContentColor = Lc.text),
                windowInsets = WindowInsets.statusBars,
            )
        },
    ) { pad -> Box(Modifier.padding(pad).fillMaxSize()) { content(PaddingValues(bottom = 24.dp)) } }
}

/** Centres content and caps its width on tablets so lines stay readable. */
@Composable
fun Readable(modifier: Modifier = Modifier, max: Dp = 760.dp, content: @Composable BoxScope.() -> Unit) {
    Box(modifier.fillMaxWidth(), contentAlignment = Alignment.TopCenter) { Box(Modifier.widthIn(max = max).fillMaxWidth(), content = content) }
}

@Composable
fun ConfirmDialog(title: String, text: String?, confirm: String, onDismiss: () -> Unit, danger: Boolean = false, onConfirm: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss, containerColor = Lc.surface2,
        title = { Text(title) }, text = text?.let { { Text(it) } },
        confirmButton = { TextButton(onClick = { onDismiss(); onConfirm() }) { Text(confirm, color = if (danger) Lc.danger else Lc.accent) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.cancel)) } },
    )
}

@Composable
fun FilterChips(options: List<Pair<String, String>>, selected: String, onSelect: (String) -> Unit, modifier: Modifier = Modifier) {
    Row(modifier.fillMaxWidth().horizontalScrollCompat().padding(horizontal = Edge), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        options.forEach { (key, label) ->
            FilterChip(selected = key == selected, onClick = { onSelect(key) }, label = { Text(label) },
                colors = FilterChipDefaults.filterChipColors(selectedContainerColor = Lc.successBg, selectedLabelColor = Lc.accent, containerColor = Lc.surface, labelColor = Lc.muted),
                border = FilterChipDefaults.filterChipBorder(enabled = true, selected = key == selected, borderColor = Lc.border, selectedBorderColor = Lc.accent.copy(alpha = 0.5f)))
        }
    }
}

@Composable
fun Modifier.horizontalScrollCompat(): Modifier = this.then(Modifier.horizontalScroll(androidx.compose.foundation.rememberScrollState()))

@Composable
fun ProgressBar(value: Float, modifier: Modifier = Modifier, color: Color = Lc.accent) {
    LinearProgressIndicator(progress = { value.coerceIn(0f, 1f) }, modifier = modifier.fillMaxWidth().height(4.dp).clip(RoundedCornerShape(2.dp)), color = color, trackColor = Lc.surface3, drawStopIndicator = {})
}

@Composable
fun SearchField(value: String, onChange: (String) -> Unit, placeholder: String, modifier: Modifier = Modifier, onSearch: () -> Unit = {}) {
    OutlinedTextField(
        value = value, onValueChange = onChange, singleLine = true, placeholder = { Text(placeholder) },
        leadingIcon = { Icon(Icons.Default.Search, null) },
        keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(imeAction = androidx.compose.ui.text.input.ImeAction.Search),
        keyboardActions = androidx.compose.foundation.text.KeyboardActions(onSearch = { onSearch() }),
        modifier = modifier.fillMaxWidth().padding(horizontal = Edge, vertical = 8.dp), shape = Shape,
        colors = OutlinedTextFieldDefaults.colors(focusedBorderColor = Lc.accent, unfocusedBorderColor = Lc.border, focusedContainerColor = Lc.surface, unfocusedContainerColor = Lc.surface),
    )
}

