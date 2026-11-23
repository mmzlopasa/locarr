package net.scyde.locarr.ui

import android.Manifest
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Dashboard
import androidx.compose.material.icons.filled.Explore
import androidx.compose.material.icons.filled.Inbox
import androidx.compose.material.icons.filled.MoreHoriz
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Timeline
import androidx.compose.material.icons.filled.VideoLibrary
import androidx.compose.material.icons.filled.CloudOff
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.LifecycleResumeEffect
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import kotlinx.coroutines.launch
import net.scyde.locarr.AppState
import net.scyde.locarr.Connection
import net.scyde.locarr.R
import net.scyde.locarr.api.User

/** A tab of the bottom bar (phones) or the navigation rail (tablets). */
data class Tab(val route: String, val label: Int, val icon: ImageVector)

/** Library managers get the admin tabs; requesters a simpler app around Discover. */
fun tabsFor(u: User?): List<Tab> = if (u?.canManageLibrary == true) listOf(
    Tab("home", R.string.tab_home, Icons.Default.Dashboard),
    Tab("discover", R.string.tab_discover, Icons.Default.Explore),
    Tab("library", R.string.tab_library, Icons.Default.VideoLibrary),
    Tab("activity", R.string.tab_activity, Icons.Default.Timeline),
    Tab("more", R.string.tab_more, Icons.Default.MoreHoriz),
) else listOf(
    Tab("discover", R.string.tab_discover, Icons.Default.Explore),
    Tab("search", R.string.tab_search, Icons.Default.Search),
    Tab("requests", R.string.tab_requests, Icons.Default.Inbox),
    Tab("more", R.string.tab_more, Icons.Default.MoreHoriz),
)

@Composable
fun LocarrRoot(state: AppState) {
    LocarrTheme {
        LaunchedEffect(Unit) { state.start() }
        CompositionLocalProvider(LocalState provides state, LocalImages provides state.images) {
            Surface(color = Lc.bg, modifier = Modifier.fillMaxSize()) {
                when (state.connection) {
                    Connection.Loading -> Box(Modifier.fillMaxSize().systemBarsPadding()) { Loading() }
                    Connection.NoServer -> ConnectScreen(state)
                    Connection.SignedOut -> SignInScreen(state, state.account?.url ?: "")
                    Connection.Unreachable -> Unreachable(state)
                    Connection.Ok -> MainShell(state)
                }
            }
        }
    }
}

@Composable
private fun Unreachable(state: AppState) {
    val scope = rememberCoroutineScope()
    Box(Modifier.fillMaxSize().systemBarsPadding(), contentAlignment = androidx.compose.ui.Alignment.Center) {
        MessageBox(Icons.Default.CloudOff, stringResource(R.string.unreachable_title), detail = stringResource(R.string.unreachable_detail, state.account?.url ?: "")) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(onClick = { scope.launch { state.start() } }) { Text(stringResource(R.string.retry)) }
                if (state.accounts.size > 1) OutlinedButton(onClick = { scope.launch { state.addServer() } }) { Text(stringResource(R.string.other_server)) }
            }
        }
    }
}

@Composable
private fun MainShell(state: AppState) {
    val nav = rememberNavController()
    val snack = remember { SnackbarHostState() }
    val me = state.me
    val tabs = remember(me) { tabsFor(me) }
    val start = tabs.first().route
    val scope = rememberCoroutineScope()

    // Android 13+: ask once for permission to post notifications.
    val ask = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) {}
    LaunchedEffect(Unit) { if (Build.VERSION.SDK_INT >= 33) ask.launch(Manifest.permission.POST_NOTIFICATIONS) }

    // Back in the app: refresh the unread badge and poll for news.
    LifecycleResumeEffect(Unit) {
        scope.launch { state.refreshUnread() }
        onPauseOrDispose {}
    }

    // A notification was tapped: switch server when needed, then open its screen.
    val link = state.pendingLink
    LaunchedEffect(link) {
        if (link == null) return@LaunchedEffect
        if (link.serverId != null && link.serverId != state.account?.id && state.accounts.any { it.id == link.serverId }) {
            state.switchTo(link.serverId)
            return@LaunchedEffect // MainShell is recreated for the other server; the link is handled then.
        }
        state.pendingLink = null
        nav.navigate(link.route) { launchSingleTop = true }
    }

    val backStack by nav.currentBackStackEntryAsState()
    val current = backStack?.destination?.route
    val wide = isWide()
    val goTab: (String) -> Unit = { route ->
        nav.navigate(route) {
            popUpTo(nav.graph.findStartDestination().id) { saveState = true }
            launchSingleTop = true
            restoreState = true
        }
    }

    CompositionLocalProvider(LocalNav provides nav, LocalSnack provides snack) {
        Row(Modifier.fillMaxSize()) {
            if (wide) NavigationRail(containerColor = Lc.surface, modifier = Modifier.fillMaxHeight().systemBarsPadding()) {
                Spacer(Modifier.height(12.dp))
                tabs.forEach { t ->
                    NavigationRailItem(selected = current == t.route, onClick = { goTab(t.route) },
                        icon = { Icon(t.icon, null) }, label = { Text(stringResource(t.label)) },
                        colors = NavigationRailItemDefaults.colors(selectedIconColor = Lc.accentInk, indicatorColor = Lc.accent, selectedTextColor = Lc.text, unselectedIconColor = Lc.muted, unselectedTextColor = Lc.muted))
                }
            }
            Scaffold(
                modifier = Modifier.weight(1f),
                containerColor = Lc.bg,
                snackbarHost = { SnackbarHost(snack) },
                bottomBar = {
                    if (!wide) NavigationBar(containerColor = Lc.surface, tonalElevation = 0.dp) {
                        tabs.forEach { t ->
                            NavigationBarItem(selected = current == t.route, onClick = { goTab(t.route) },
                                icon = { Icon(t.icon, null) }, label = { Text(stringResource(t.label), maxLines = 1) },
                                colors = NavigationBarItemDefaults.colors(selectedIconColor = Lc.accentInk, indicatorColor = Lc.accent, selectedTextColor = Lc.text, unselectedIconColor = Lc.muted, unselectedTextColor = Lc.muted))
                        }
                    }
                },
            ) { pad ->
                Box(Modifier.padding(pad).consumeWindowInsets(pad)) { Routes(nav, start) }
            }
        }
    }
}

@Composable
private fun Routes(nav: androidx.navigation.NavHostController, start: String) {
    val long = { name: String -> navArgument(name) { type = NavType.LongType } }
    val int = { name: String -> navArgument(name) { type = NavType.IntType } }
    NavHost(nav, startDestination = start) {
        // Tabs
        composable("home") { OverviewScreen() }
        composable("discover") { DiscoverScreen() }
        composable("search") { SearchScreen(tab = true) }
        composable("requests") { RequestsScreen(tab = true) }
        composable("library") { LibraryScreen() }
        composable("activity") { ActivityScreen() }
        composable("more") { MoreScreen() }

        // Requester
        composable("discover/search") { SearchScreen(tab = false) }
        composable("discover/list/{key}") { DiscoverListScreen(it.arguments?.getString("key") ?: "trending") }
        composable("title/{kind}/{id}") { TitleScreen(it.arguments?.getString("kind") ?: "movie", it.arguments?.getString("id") ?: "") }
        composable("person/{id}", listOf(long("id"))) { PersonScreen(it.arguments!!.getLong("id")) }
        composable("request/{id}", listOf(long("id"))) { RequestDetailScreen(it.arguments!!.getLong("id")) }
        composable("all-requests") { RequestsScreen(tab = false) }
        composable("issues") { IssuesScreen() }
        composable("issue/{id}", listOf(long("id"))) { IssueDetailScreen(it.arguments!!.getLong("id")) }
        composable("notifications") { NotificationsScreen() }
        composable("account") { AccountScreen() }

        // Library managers
        composable("movie/{id}", listOf(long("id"))) { MovieScreen(it.arguments!!.getLong("id")) }
        composable("series/{id}", listOf(long("id"))) { SeriesScreen(it.arguments!!.getLong("id")) }
        composable("season/{id}/{n}", listOf(long("id"), int("n"))) { SeasonScreen(it.arguments!!.getLong("id"), it.arguments!!.getInt("n")) }
        composable("artist/{id}", listOf(long("id"))) { ArtistScreen(it.arguments!!.getLong("id")) }
        composable("album/{id}", listOf(long("id"))) { AlbumScreen(it.arguments!!.getLong("id")) }
        composable("releases/{kind}/{id}/{n}", listOf(long("id"), int("n"))) {
            ReleasesScreen(it.arguments?.getString("kind") ?: "movie", it.arguments!!.getLong("id"), it.arguments!!.getInt("n"))
        }
        composable("add/{kind}") { AddScreen(it.arguments?.getString("kind") ?: "movie") }
        composable("board/{code}") { BoardGroupScreen(it.arguments?.getString("code") ?: "") }
        composable("health") { HealthScreen() }
        composable("history") { HistoryScreen() }
        composable("indexers") { IndexersScreen() }
        composable("clients") { ClientsScreen() }
        composable("folders") { FoldersScreen() }
        composable("calendar") { CalendarScreen() }
        composable("wanted") { WantedScreen() }
    }
}
