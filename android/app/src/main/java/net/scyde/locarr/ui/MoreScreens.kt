package net.scyde.locarr.ui

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ExitToApp
import androidx.compose.material.icons.automirrored.filled.OpenInNew
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.appcompat.app.AppCompatDelegate
import kotlinx.coroutines.launch
import net.scyde.locarr.BuildConfig
import net.scyde.locarr.R
import net.scyde.locarr.applyLanguage

/** Settings that are edited in the web interface (route on the server, label). */
val WEB_SETTINGS = listOf(
    "settings" to R.string.web_general,
    "settings/users" to R.string.web_users,
    "settings/profiles" to R.string.web_profiles,
    "settings/quality" to R.string.web_quality,
    "settings/customformats" to R.string.web_customformats,
    "settings/rules" to R.string.web_rules,
    "settings/mediamanagement" to R.string.web_naming,
    "settings/metadata" to R.string.web_metadata,
    "settings/metadataprofiles" to R.string.web_metadataprofiles,
    "settings/requests" to R.string.web_requests,
    "settings/importlists" to R.string.web_importlists,
    "settings/subtitles" to R.string.web_subtitles,
    "settings/mediaserver" to R.string.web_mediaserver,
    "settings/notifications" to R.string.web_notifications,
    "settings/system" to R.string.web_monitoring,
    "system/logs" to R.string.web_logs,
    "system/backups" to R.string.web_backups,
    "system/tasks" to R.string.web_tasks,
    "migrate" to R.string.web_migrate,
)

@Composable
private fun NavRow(icon: ImageVector, label: String, subtitle: String? = null, trailing: (@Composable () -> Unit)? = null, onClick: () -> Unit) {
    ListRow(label, subtitle = subtitle, leading = { Icon(icon, null, tint = Lc.muted) }, trailing = trailing, onClick = onClick)
}

@Composable
fun MoreScreen() {
    val nav = LocalNav.current
    val state = LocalState.current
    val ctx = LocalContext.current
    val me = state.me
    val a = api()
    Screen(stringResource(R.string.tab_more), back = false) {
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
            item {
                Readable {
                    NavRow(Icons.Default.AccountCircle, me?.username ?: "", subtitle = (state.account?.name?.ifBlank { null } ?: state.account?.url ?: "") + " · " + roleLabel(me?.role)) { nav.navigate("account") }
                }
            }
            item { SectionTitle(stringResource(R.string.section_requests)) }
            item { Readable { Column {
                if (me?.canManageLibrary == true) {
                    NavRow(Icons.Default.Inbox, stringResource(R.string.requests_title)) { nav.navigate("all-requests") }
                    NavRow(Icons.Default.Search, stringResource(R.string.search)) { nav.navigate("discover/search") }
                }
                NavRow(Icons.Default.ReportProblem, stringResource(R.string.issues_title)) { nav.navigate("issues") }
                NavRow(Icons.Default.Notifications, stringResource(R.string.notifications), trailing = { if (state.unread > 0) Pill(state.unread.toString(), Tone.Success) }) { nav.navigate("notifications") }
            } } }
            if (me?.canManageLibrary == true) {
                item { SectionTitle(stringResource(R.string.section_library)) }
                item { Readable { Column {
                    NavRow(Icons.Default.CalendarMonth, stringResource(R.string.calendar)) { nav.navigate("calendar") }
                    NavRow(Icons.Default.SearchOff, stringResource(R.string.wanted)) { nav.navigate("wanted") }
                    NavRow(Icons.Default.History, stringResource(R.string.history)) { nav.navigate("history") }
                } } }
            }
            if (me?.isAdmin == true) {
                item { SectionTitle(stringResource(R.string.section_system)) }
                item { Readable { Column {
                    NavRow(Icons.Default.HealthAndSafety, stringResource(R.string.health)) { nav.navigate("health") }
                    NavRow(Icons.Default.Radar, stringResource(R.string.indexers)) { nav.navigate("indexers") }
                    NavRow(Icons.Default.Download, stringResource(R.string.download_clients)) { nav.navigate("clients") }
                    NavRow(Icons.Default.Folder, stringResource(R.string.media_folders)) { nav.navigate("folders") }
                } } }
                item { SectionTitle(stringResource(R.string.section_web)) }
                item { Text(stringResource(R.string.section_web_hint), color = Lc.faint, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(horizontal = Edge)) }
                items(WEB_SETTINGS) { (route, label) ->
                    Readable { NavRow(Icons.AutoMirrored.Filled.OpenInNew, stringResource(label)) { openUrl(ctx, a.webUrl(route)) } }
                }
            }
            item { Text("Locarr ${BuildConfig.VERSION_NAME}", color = Lc.faint, style = MaterialTheme.typography.labelSmall, modifier = Modifier.padding(Edge)) }
        }
    }
}

@Composable
fun roleLabel(role: String?) = stringResource(when (role) { "admin" -> R.string.role_admin; "manager" -> R.string.role_manager; else -> R.string.role_requester })

@Composable
fun AccountScreen() {
    val state = LocalState.current
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val a = api()
    var confirmSignOut by remember { mutableStateOf(false) }
    var removing by remember { mutableStateOf<String?>(null) }
    val current = AppCompatDelegate.getApplicationLocales().toLanguageTags()
    Screen(stringResource(R.string.account)) {
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
            item { SectionTitle(stringResource(R.string.servers)) }
            items(state.accounts, key = { it.id }) { acc ->
                Readable {
                    ListRow(acc.name.ifBlank { acc.url }, subtitle = listOf(acc.username, acc.url, if (acc.sealedToken == null) stringResource(R.string.signed_out) else "").filter { it.isNotBlank() }.joinToString(" · "),
                        leading = { RadioButton(acc.id == state.account?.id, onClick = { scope.launch { state.switchTo(acc.id) } }) },
                        trailing = { IconButton(onClick = { removing = acc.id }) { Icon(Icons.Default.Delete, stringResource(R.string.remove_server)) } },
                        onClick = { if (acc.id != state.account?.id) scope.launch { state.switchTo(acc.id) } })
                }
            }
            item { Readable { ListRow(stringResource(R.string.add_server), leading = { Icon(Icons.Default.Add, null, tint = Lc.accent) }) { scope.launch { state.addServer() } } } }
            item { SectionTitle(stringResource(R.string.language)) }
            item {
                FilterChips(listOf("" to stringResource(R.string.lang_system), "en" to "English", "hr" to "Hrvatski"), current, { applyLanguage(it) })
            }
            item { SectionTitle(stringResource(R.string.security)) }
            item { Readable { ListRow(stringResource(R.string.security_web), subtitle = stringResource(R.string.security_web_hint), leading = { Icon(Icons.Default.Lock, null, tint = Lc.muted) }) { openUrl(ctx, a.webUrl("account")) } } }
            item { Readable { ListRow(stringResource(R.string.notification_settings), subtitle = stringResource(R.string.notification_settings_hint), leading = { Icon(Icons.Default.NotificationsActive, null, tint = Lc.muted) }) {
                val i = android.content.Intent(android.provider.Settings.ACTION_APP_NOTIFICATION_SETTINGS).putExtra(android.provider.Settings.EXTRA_APP_PACKAGE, ctx.packageName)
                runCatching { ctx.startActivity(i) }
            } } }
            item { Spacer(Modifier.height(12.dp)) }
            item { Readable { ListRow(stringResource(R.string.sign_out), leading = { Icon(Icons.AutoMirrored.Filled.ExitToApp, null, tint = Lc.danger) }) { confirmSignOut = true } } }
        }
    }
    if (confirmSignOut) ConfirmDialog(stringResource(R.string.sign_out), stringResource(R.string.sign_out_hint), stringResource(R.string.sign_out), { confirmSignOut = false }, danger = true) { scope.launch { state.signOut() } }
    removing?.let { id -> ConfirmDialog(stringResource(R.string.remove_server), stringResource(R.string.remove_server_hint), stringResource(R.string.remove), { removing = null }, danger = true) { scope.launch { state.removeServer(id) } } }
}
