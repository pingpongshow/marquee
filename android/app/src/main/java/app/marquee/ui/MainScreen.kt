package app.marquee.ui

import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.draw.clip
import androidx.compose.ui.Alignment
import androidx.compose.runtime.remember
import androidx.compose.runtime.collectAsState
import androidx.compose.material.icons.filled.GraphicEq
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.Spacer
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

/** Phones: bottom navigation with the mini player above it. TV: a side rail (D-pad friendly). */
@Composable
fun MainScreen() {
    val marquee = LocalMarquee.current
    val nav = rememberNavController()
    val entry by nav.currentBackStackEntryAsState()
    val route = entry?.destination?.route ?: "home"
    val fullScreen = route.startsWith("player") || route == "nowplaying" || route.startsWith("live/") || route.startsWith("recap/")
    // The tab whose section is showing; reselecting it goes back to its first screen.
    var tab by rememberSaveable { mutableStateOf("home") }
    LaunchedEffect(route) { if (tabs.any { it.route == route }) tab = route }
    fun go(r: String) {
        if (r == tab) { nav.popBackStack(r, inclusive = false); return }
        tab = r
        nav.navigate(r) { popUpTo("home") { saveState = true }; launchSingleTop = true; restoreState = true }
    }

    // Remote control (USER-14): a video sent from another app opens here, replacing one playing.
    val remote = LocalRemote.current
    LaunchedEffect(Unit) {
        remote.navigate.collect { r ->
            val playing = nav.currentBackStackEntry?.destination?.route?.startsWith("player") == true
            nav.navigate(r) { if (playing) popUpTo("player/{id}?start={start}&group={group}") { inclusive = true } }
        }
    }

    if (marquee.isTv) {
        Row(Modifier.fillMaxSize()) {
            if (!fullScreen) Column(Modifier.fillMaxHeight().width(112.dp).padding(vertical = 24.dp, horizontal = 10.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                tabs.forEach { t -> TvRailItem(t, tab == t.route) { go(t.route) } }
                Spacer(Modifier.weight(1f))
                TvNowPlaying { nav.navigate("nowplaying") }
            }
            Box(Modifier.fillMaxSize()) { Routes(nav) }
        }
        return
    }
    Scaffold(
        bottomBar = {
            if (!fullScreen) Column {
                MiniPlayer(onOpen = { nav.navigate("nowplaying") })
                NavigationBar {
                    tabs.forEach { t ->
                        NavigationBarItem(selected = tab == t.route, onClick = { go(t.route) }, icon = { Icon(t.icon, t.label) }, label = { Text(t.label) })
                    }
                }
            }
        },
    ) { padding -> Box(Modifier.padding(padding)) { Routes(nav) } }
}

@Composable
private fun Routes(nav: NavHostController) {
    NavHost(nav, startDestination = "home") {
        composable("home") { HomeScreen(nav) }
        composable("libraries") { LibrariesScreen(nav) }
        composable("playlists") { PlaylistsScreen(nav) }
        composable("search") { SearchScreen(nav) }
        composable("settings") { SettingsScreen(nav) }
        composable("library/{id}", listOf(navArgument("id") { type = NavType.LongType })) { LibraryScreen(nav, it.arguments!!.getLong("id")) }
        composable("item/{id}", listOf(navArgument("id") { type = NavType.LongType })) { ItemScreen(nav, it.arguments!!.getLong("id")) }
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
        composable("discover") { DiscoverScreen(nav) }
        // Opened from Libraries (and by deep links); not a tab.
        composable("livetv") { LiveTvScreen(nav) }
        composable("musicbrowse/{lib}/{kind}?sort={sort}", listOf(navArgument("lib") { type = NavType.LongType }, navArgument("sort") { type = NavType.StringType; nullable = true; defaultValue = null })) {
            app.marquee.music.MusicBrowseScreen(nav, it.arguments!!.getLong("lib"), it.arguments!!.getString("kind")!!, it.arguments!!.getString("sort"))
        }
        composable("musicmuse/{lib}", listOf(navArgument("lib") { type = NavType.LongType })) { app.marquee.music.MusicMuseScreen(nav, it.arguments!!.getLong("lib")) }
        composable("browse/{lib}/{kind}/{name}", listOf(navArgument("lib") { type = NavType.LongType })) {
            app.marquee.music.MoodStyleScreen(nav, it.arguments!!.getLong("lib"), it.arguments!!.getString("kind")!!, it.arguments!!.getString("name")!!)
        }
        composable("live/{id}", listOf(navArgument("id") { type = NavType.LongType })) { LiveWatchScreen(nav, it.arguments!!.getLong("id")) }
        composable("approvals") { ApprovalsScreen() }
        composable("stats") { StatsScreen(nav) }
        composable("nowplaying") { NowPlayingScreen(onClose = { nav.popBackStack() }, onRemote = { nav.navigate("remote/$it") },
            onOpenPlaylist = { nav.navigate("playlist/$it") }) }
        composable("edithome") { EditHomeScreen(nav) }
        composable("users") { UsersScreen() }
        composable("serversettings") { ServerSettingsScreen() }
        composable("subtitles") { SubtitleAppearanceScreen() }
        composable("remote") { RemotePlayersScreen(nav) }
        composable("remote/{id}", listOf(navArgument("id") { type = NavType.LongType })) { RemoteScreen(nav, it.arguments!!.getLong("id")) }
        composable("recap/{year}", listOf(navArgument("year") { type = NavType.IntType })) { app.marquee.music.RecapScreen(nav, it.arguments!!.getInt("year")) }
        composable("muse?lib={lib}", listOf(navArgument("lib") { type = NavType.LongType; defaultValue = -1L })) {
            MuseVideoScreen(nav, it.arguments!!.getLong("lib").takeIf { l -> l >= 0 })
        }
        composable("health") { LibraryHealthScreen(nav) }
        composable("health/{check}") { HealthIssuesScreen(nav, it.arguments!!.getString("check")!!) }
    }
}

val sidePadding get() = 16.dp

/** A TV rail entry: gold when selected, a filled pill under the D-pad. */
@Composable
private fun TvRailItem(t: Dest, selected: Boolean, onClick: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    val tint = when { focused -> MaterialTheme.colorScheme.background; selected -> Gold; else -> MaterialTheme.colorScheme.onSurfaceVariant }
    Column(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(14.dp))
            .background(if (focused) Gold else Color.Transparent)
            .onFocusChanged { focused = it.isFocused }
            .clickable(onClick = onClick).padding(vertical = 10.dp),
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
    TvRailItem(Dest("nowplaying", "Playing", Icons.Filled.GraphicEq), false, onOpen)
}
