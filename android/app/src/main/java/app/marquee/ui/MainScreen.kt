package app.marquee.ui

import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.foundation.focusGroup
import androidx.compose.foundation.selection.selectable
import androidx.compose.ui.draw.clip
import androidx.compose.ui.Alignment
import androidx.compose.runtime.remember
import androidx.compose.runtime.collectAsState
import androidx.compose.material.icons.filled.GraphicEq
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.material.icons.filled.CloudOff
import androidx.compose.material.icons.filled.DownloadDone
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.foundation.clickable
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.LibraryMusic
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.VideoLibrary
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationRail
import androidx.compose.material3.NavigationRailItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import app.marquee.music.MiniPlayer
import app.marquee.music.NowPlayingScreen

private data class Dest(val route: String, val label: String, val icon: ImageVector)

// Live TV isn't a tab: it's listed under Libraries when it's set up.
private val tabs = listOf(
    Dest("home", "Home", Icons.Filled.Home),
    Dest("libraries", "Libraries", Icons.Filled.VideoLibrary),
    Dest("playlists", "Playlists", Icons.Filled.LibraryMusic),
    Dest("search", "Search", Icons.Filled.Search),
    Dest("settings", "Settings", Icons.Filled.Settings),
)

// The TV's rail: Search first of what you browse, above Libraries.
private val railTabs = tabs.sortedBy { if (it.route == "home") 0 else if (it.route == "search") 1 else 2 }

// While offline (phones and tablets) only what works without the server: its screens and Settings.
private val offlineTabs = listOf(
    Dest(OFFLINE_ROUTE, "Library", Icons.Filled.DownloadDone),
    Dest("settings", "Settings", Icons.Filled.Settings),
)

/** Screens that take over the whole display, without the tabs and mini player. */
private fun isFullScreen(route: String) = route.startsWith("player") || route == "nowplaying" || route.startsWith("live/") ||
    route.startsWith("vodplay/") || route.startsWith("vodepplay/") || route.startsWith("recap/")

/**
 * Phones: bottom navigation with the mini player above it. TV: a side rail (D-pad friendly). While
 * offline, phones and tablets swap their screens for the Offline Library, with a banner across the
 * top; the online screens stay where they were for when the server's back. A player that's open
 * stays open through the change, until it's closed.
 */
@OptIn(androidx.compose.ui.ExperimentalComposeUiApi::class)
@Composable
fun MainScreen() {
    val marquee = LocalMarquee.current
    val nav = rememberNavController()
    val entry by nav.currentBackStackEntryAsState()
    val route = entry?.destination?.route ?: "home"
    val fullScreen = isFullScreen(route)
    // The tab whose section is showing; reselecting it goes back to its first screen.
    var tab by rememberSaveable { mutableStateOf("home") }
    LaunchedEffect(route) { if (tabs.any { it.route == route }) tab = route }
    fun go(r: String) {
        if (r == tab) { nav.popBackStack(r, inclusive = false); return }
        tab = r
        nav.navigate(r) { popUpTo("home") { saveState = true }; launchSingleTop = true; restoreState = true }
    }

    if (marquee.isTv) {
        Row(Modifier.fillMaxSize()) {
            // Left from a page lands on the tab that's showing, not whichever item is level with the focus.
            val railFocus = remember { tabs.associate { it.route to FocusRequester() } }
            if (!fullScreen) Column(Modifier.fillMaxHeight().width(112.dp).padding(vertical = 24.dp, horizontal = 10.dp)
                .focusProperties { enter = { railFocus[tab] ?: FocusRequester.Default } }.focusGroup(), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                // Who's watching, above Home: opens the profile picker.
                ProfileButton(nav, tv = true, modifier = Modifier.padding(bottom = 8.dp))
                railTabs.forEach { t -> TvRailItem(t, tab == t.route, Modifier.focusRequester(railFocus.getValue(t.route))) { go(t.route) } }
                Spacer(Modifier.weight(1f))
                TvNowPlaying { nav.navigate("nowplaying") }
            }
            Box(Modifier.fillMaxSize()) {
                Routes(nav)
                // Asks for a rating after the player closed at the end (USER-19).
                RatingPromptHost(!fullScreen, Modifier.align(Alignment.BottomCenter))
            }
        }
        TimeLimitHost()
        return
    }
    val offline by marquee.offline.collectAsState()
    val offlineNav = rememberNavController()
    val offlineEntry by offlineNav.currentBackStackEntryAsState()
    val offlineRoute = offlineEntry?.destination?.route ?: OFFLINE_ROUTE
    var library by rememberSaveable { mutableStateOf(offline) }
    LaunchedEffect(offline, library, route, offlineRoute) {
        if (isFullScreen(if (library) offlineRoute else route)) return@LaunchedEffect
        library = offline
        // Next time it starts from the top, as there may be other downloads by then.
        if (!offline) runCatching { offlineNav.popBackStack(OFFLINE_ROUTE, inclusive = false) }
    }
    val shownNav = if (library) offlineNav else nav
    val shownRoute = if (library) offlineRoute else route
    // Downloads and Smart downloads open from Settings.
    val offlineTab = if (offlineRoute == "settings" || offlineRoute == "downloads" || offlineRoute == "smartdownloads") "settings" else OFFLINE_ROUTE
    // What the screens remember (scroll positions and the like) waits for them while they're away.
    val saved = androidx.compose.runtime.saveable.rememberSaveableStateHolder()
    // Not over a video, which fills the screen.
    val banner = offline && !shownRoute.startsWith("player")
    Column(Modifier.fillMaxSize()) {
        if (banner) OfflineBanner()
        Scaffold(
            if (banner) Modifier.weight(1f).consumeWindowInsets(WindowInsets.statusBars) else Modifier.weight(1f),
            bottomBar = {
                if (!isFullScreen(shownRoute)) Column {
                    MiniPlayer(onOpen = { shownNav.navigate("nowplaying") })
                    NavigationBar {
                        if (library) offlineTabs.forEach { t ->
                            NavigationBarItem(selected = offlineTab == t.route, onClick = {
                                offlineNav.navigate(t.route) { popUpTo(OFFLINE_ROUTE); launchSingleTop = true }
                            }, icon = { Icon(t.icon, t.label) }, label = { Text(t.label) })
                        }
                        else tabs.forEach { t ->
                            NavigationBarItem(selected = tab == t.route, onClick = { go(t.route) }, icon = { Icon(t.icon, t.label) }, label = { Text(t.label) })
                        }
                    }
                }
            },
        ) { padding ->
            Box(Modifier.padding(padding)) {
                if (library) saved.SaveableStateProvider("offline") { OfflineRoutes(offlineNav) }
                else saved.SaveableStateProvider("online") { Routes(nav) }
                // Asks for a rating after the player closed at the end (USER-19).
                RatingPromptHost(!isFullScreen(shownRoute), Modifier.align(Alignment.BottomCenter))
            }
        }
    }
    TimeLimitHost()
}

/** Across the top of the screen while offline, under the status bar. */
@Composable
private fun OfflineBanner() {
    Row(Modifier.fillMaxWidth().background(Surface2).statusBarsPadding().padding(horizontal = sidePadding, vertical = 4.dp)
        .semantics(mergeDescendants = true) { liveRegion = LiveRegionMode.Polite }.testTag("offlineBanner"),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.CenterHorizontally)) {
        Icon(Icons.Filled.CloudOff, null, Modifier.size(14.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
        Text("Offline: showing downloads", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

@Composable
private fun Routes(nav: NavHostController) {
    NavHost(nav, startDestination = "home") {
        composable("home") { HomeScreen(nav) }
        // All of Continue Watching (Home's row's View more).
        composable(CONTINUE_ROUTE) { ContinueWatchingScreen(nav) }
        composable("libraries") { LibrariesScreen(nav) }
        composable("playlists") { PlaylistsScreen(nav) }
        composable("search") { SearchScreen(nav) }
        composable("settings") { SettingsScreen(nav) }
        composable("library/{id}?sort={sort}", listOf(navArgument("id") { type = NavType.LongType },
            navArgument("sort") { type = NavType.StringType; nullable = true; defaultValue = null })) {
            LibraryScreen(nav, it.arguments!!.getLong("id"), it.arguments!!.getString("sort"))
        }
        composable("item/{id}", listOf(navArgument("id") { type = NavType.LongType })) { ItemScreen(nav, it.arguments!!.getLong("id")) }
        // All of an artist's Top Songs (their page's More).
        composable("topsongs/{id}", listOf(navArgument("id") { type = NavType.LongType })) { TopSongsScreen(nav, it.arguments!!.getLong("id")) }
        composable("playlist/{id}", listOf(navArgument("id") { type = NavType.LongType })) { PlaylistScreen(nav, it.arguments!!.getLong("id")) }
        composable(
            "player/{id}?start={start}&group={group}",
            listOf(
                navArgument("id") { type = NavType.LongType },
                navArgument("start") { type = NavType.LongType; defaultValue = -1L },
                navArgument("group") { type = NavType.StringType; nullable = true; defaultValue = null },
            ),
        ) { PlayerScreen(nav, it.arguments!!.getLong("id"), it.arguments!!.getLong("start").takeIf { s -> s >= 0 }, it.arguments!!.getString("group")) }
        composable("downloads") { DownloadsScreen(nav) }
        composable("smartdownloads") { SmartDownloadsScreen(nav) }
        composable("vodfollows") { VodFollowsScreen(nav) }
        composable("discover") { DiscoverScreen(nav) }
        // Opened from Libraries (and by deep links); not a tab.
        composable("livetv") { LiveTvScreen(nav) }
        // VOD (LIVE-6): VOD Movies and VOD Shows, pages of their own listed after Live TV.
        composable("vod") { VodPage(nav) }
        composable("vodshows") { VodShowsPage(nav) }
        // The VOD pages' Recommended for you, in full: rows of films or series picked for this person.
        composable("vodrecommended?kind={kind}", listOf(navArgument("kind") { type = NavType.StringType; defaultValue = "movie" })) {
            VodRecommendedScreen(nav, it.arguments!!.getString("kind") == "series")
        }
        composable("musicbrowse/{lib}/{kind}?sort={sort}", listOf(navArgument("lib") { type = NavType.LongType }, navArgument("sort") { type = NavType.StringType; nullable = true; defaultValue = null })) {
            app.marquee.music.MusicBrowseScreen(nav, it.arguments!!.getLong("lib"), it.arguments!!.getString("kind")!!, it.arguments!!.getString("sort"))
        }
        composable("musicmuse/{lib}", listOf(navArgument("lib") { type = NavType.LongType })) { app.marquee.music.MusicMuseScreen(nav, it.arguments!!.getLong("lib")) }
        composable("browse/{lib}/{kind}/{name}", listOf(navArgument("lib") { type = NavType.LongType })) {
            app.marquee.music.MoodStyleScreen(nav, it.arguments!!.getLong("lib"), it.arguments!!.getString("kind")!!, it.arguments!!.getString("name")!!)
        }
        // Movies on demand (LIVE-6): a film's page and its player.
        composable("vod/{id}", listOf(navArgument("id") { type = NavType.LongType })) { VodMovieScreen(nav, it.arguments!!.getLong("id")) }
        composable(
            "vodplay/{id}?start={start}",
            listOf(navArgument("id") { type = NavType.LongType }, navArgument("start") { type = NavType.LongType; defaultValue = -1L }),
        ) { VodPlayerScreen(nav, it.arguments!!.getLong("id"), it.arguments!!.getLong("start").takeIf { s -> s >= 0 }) }
        // VOD Shows: a show's page and an episode's player.
        composable("vodshow/{id}", listOf(navArgument("id") { type = NavType.LongType })) { VodShowScreen(nav, it.arguments!!.getLong("id")) }
        composable(
            "vodepplay/{series}/{id}?start={start}",
            listOf(navArgument("series") { type = NavType.LongType }, navArgument("id") { type = NavType.LongType }, navArgument("start") { type = NavType.LongType; defaultValue = -1L }),
        ) { VodEpisodePlayerScreen(nav, it.arguments!!.getLong("series"), it.arguments!!.getLong("id"), it.arguments!!.getLong("start").takeIf { s -> s >= 0 }) }
        composable("live/{id}", listOf(navArgument("id") { type = NavType.LongType })) { LiveWatchScreen(nav, it.arguments!!.getLong("id")) }
        composable("approvals") { ApprovalsScreen() }
        composable("stats") { StatsScreen(nav) }
        composable("nowplaying") { NowPlayingScreen(onClose = { nav.popBackStack() },
            onOpenPlaylist = { nav.navigate("playlist/$it") }) }
        composable("edithome") { EditHomeScreen(nav) }
        composable("homehub/{id}") { HomeHubScreen(nav, it.arguments!!.getString("id")!!) }
        composable("users") { UsersScreen() }
        composable("serversettings") { ServerSettingsScreen() }
        composable("subtitles") { SubtitleAppearanceScreen() }
        composable("recap/{year}", listOf(navArgument("year") { type = NavType.IntType })) { app.marquee.music.RecapScreen(nav, it.arguments!!.getInt("year")) }
        composable("muse?lib={lib}", listOf(navArgument("lib") { type = NavType.LongType; defaultValue = -1L })) {
            MuseVideoScreen(nav, it.arguments!!.getLong("lib").takeIf { l -> l >= 0 })
        }
        composable("health") { LibraryHealthScreen(nav) }
        composable("health/{check}") { HealthIssuesScreen(nav, it.arguments!!.getString("check")!!) }
    }
}

/** What works without the server: the Offline Library, the players and Settings. */
@Composable
private fun OfflineRoutes(nav: NavHostController) {
    NavHost(nav, startDestination = OFFLINE_ROUTE) {
        composable(OFFLINE_ROUTE) { OfflineLibraryScreen(nav) }
        composable("offlineshow/{id}", listOf(navArgument("id") { type = NavType.LongType })) { OfflineShowScreen(nav, it.arguments!!.getLong("id")) }
        composable("offlinealbum/{id}", listOf(navArgument("id") { type = NavType.LongType })) { OfflineAlbumScreen(it.arguments!!.getLong("id")) }
        composable("offlineplaylist/{id}", listOf(navArgument("id") { type = NavType.LongType })) { OfflinePlaylistScreen(nav, it.arguments!!.getLong("id")) }
        composable(
            "player/{id}?start={start}&group={group}",
            listOf(
                navArgument("id") { type = NavType.LongType },
                navArgument("start") { type = NavType.LongType; defaultValue = -1L },
                navArgument("group") { type = NavType.StringType; nullable = true; defaultValue = null },
            ),
        ) { PlayerScreen(nav, it.arguments!!.getLong("id"), it.arguments!!.getLong("start").takeIf { s -> s >= 0 }, it.arguments!!.getString("group")) }
        composable("nowplaying") { NowPlayingScreen(onClose = { nav.popBackStack() }) }
        composable("settings") { SettingsScreen(nav) }
        composable("downloads") { DownloadsScreen(nav) }
        composable("smartdownloads") { SmartDownloadsScreen(nav) }
    }
}

val sidePadding get() = 16.dp

/** A TV rail entry: gold when selected, a filled pill under the D-pad. */
@Composable
private fun TvRailItem(t: Dest, selected: Boolean, modifier: Modifier = Modifier, onClick: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    val tint = when { focused -> MaterialTheme.colorScheme.background; selected -> Gold; else -> MaterialTheme.colorScheme.onSurfaceVariant }
    Column(
        modifier.fillMaxWidth().clip(RoundedCornerShape(14.dp))
            .background(if (focused) Gold else Color.Transparent)
            .onFocusChanged { focused = it.isFocused }
            // A tab, so TalkBack says which one is showing (as the phones' bottom bar does).
            .selectable(selected, onClick = onClick, role = androidx.compose.ui.semantics.Role.Tab).padding(vertical = 10.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Icon(t.icon, null, tint = tint)
        Text(t.label, style = MaterialTheme.typography.labelMedium, color = tint, fontWeight = if (selected) FontWeight.Bold else FontWeight.Normal)
    }
}

/** TV: Now Playing at the bottom of the rail while music plays. */
@Composable
private fun TvNowPlaying(onOpen: () -> Unit) {
    val music = LocalMusic.current
    val now by music.now.collectAsState()
    if (now == null) return
    TvRailItem(Dest("nowplaying", "Playing", Icons.Filled.GraphicEq), false, onClick = onOpen)
}
