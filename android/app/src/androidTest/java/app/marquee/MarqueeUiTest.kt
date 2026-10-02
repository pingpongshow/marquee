package app.marquee

import android.content.Intent
import android.graphics.Bitmap
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.ExperimentalTestApi
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.hasScrollToNodeAction
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.isFocused
import androidx.compose.ui.test.isSelected
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.ComposeTestRule
import androidx.compose.ui.test.junit4.createEmptyComposeRule
import androidx.compose.ui.test.onFirst
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import androidx.test.core.app.ActivityScenario
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import android.content.ComponentName
import android.view.KeyEvent
import androidx.media3.common.MediaItem
import androidx.media3.session.MediaBrowser
import androidx.media3.session.SessionToken
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import java.util.concurrent.TimeUnit
import okhttp3.MediaType.Companion.toMediaType
import org.junit.After
import org.junit.Assume.assumeTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import java.io.File

/**
 * End-to-end flows against a running Marquee server (instrumentation argument `server`,
 * default the development server on the host: 10.0.2.2:32597). Screenshots go to the app's
 * external files dir under shots/.
 */
@OptIn(ExperimentalTestApi::class)
@RunWith(AndroidJUnit4::class)
class MarqueeUiTest {
    @get:Rule val rule = createEmptyComposeRule()
    private lateinit var scenario: ActivityScenario<MainActivity>

    private val args = InstrumentationRegistry.getArguments()
    private val server = args.getString("server") ?: "10.0.2.2:32597"
    private val profile = args.getString("profile") ?: "Kiddo"
    private val context = InstrumentationRegistry.getInstrumentation().targetContext
    private val isTv = context.packageManager.hasSystemFeature(android.content.pm.PackageManager.FEATURE_LEANBACK)

    @Before fun launch() {
        val intent = Intent(context, MainActivity::class.java).putExtra("marquee-reset", true)
        scenario = ActivityScenario.launch(intent)
    }

    @After fun close() = scenario.close()

    private fun shot(name: String) {
        rule.waitForIdle()
        Thread.sleep(500) // let the frame reach the screen
        val bmp = InstrumentationRegistry.getInstrumentation().uiAutomation.takeScreenshot() ?: return
        val dir = File(context.getExternalFilesDir(null), "shots").apply { mkdirs() }
        File(dir, "$name.png").outputStream().use { bmp.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    private fun ComposeTestRule.waitText(text: String, timeout: Long = 15_000, substring: Boolean = false) =
        waitUntilAtLeastOneExists(hasText(text, substring = substring), timeout)

    private fun tap(text: String, substring: Boolean = false) =
        rule.onAllNodesWithText(text, substring = substring).onFirst().performClick()

    private fun back() = scenario.onActivity { it.onBackPressedDispatcher.onBackPressed() }

    private fun connectAndSignIn() {
        rule.waitUntilAtLeastOneExists(hasSetTextAction(), 10_000)
        shot("01-connect")
        rule.onNode(hasSetTextAction()).performTextInput(server)
        rule.onNodeWithText("Connect").performClick()
        rule.waitText("Who's watching?")
        shot("02-who-is-watching")
        tap(profile)
        rule.waitText("Recently Added", 20_000, substring = true)
        shot("03-home")
    }

    private fun openLibrary(name: String) {
        tap("Libraries")
        rule.waitText(name)
        tap(name)
    }

    @Test fun browseAndPlay() {
        connectAndSignIn()
        openLibrary("Movies")
        rule.waitText("00 Preview Test")
        shot("04-movies")
        tap("00 Preview Test")
        rule.waitUntilAtLeastOneExists(hasText("Play") or hasText("Resume"), 10_000)
        Thread.sleep(1000) // artwork
        shot("05-movie-detail")
        rule.onAllNodes(hasText("Play") or hasText("Resume")).onFirst().performClick()
        val playing = hasContentDescription("Video player") and SemanticsMatcher.expectValue(SemanticsProperties.StateDescription, "Playing")
        rule.waitUntilAtLeastOneExists(playing, 30_000)
        Thread.sleep(4000)
        shot("06-video-playing")
        back()
        rule.waitText("00 Preview Test")

        // Music: play an artist; the mini player appears and pauses. Reselecting the
        // Libraries tab goes back to the list.
        openLibrary("Music")
        rule.waitText("Calm Pads", substring = true)
        tap("Calm Pads", substring = true)
        rule.waitText("Play")
        shot("07-artist")
        tap("Play")
        if (isTv) {
            // TV: no mini player; Now Playing sits at the bottom of the rail.
            rule.waitText("Playing", 15_000)
            Thread.sleep(2000)
            shot("08-rail-playing")
            tap("Playing")
            rule.waitText("PLAYING FROM")
            rule.onAllNodes(hasContentDescription("Pause")).onFirst().performClick()
        } else {
            rule.waitUntilAtLeastOneExists(hasContentDescription("Pause"), 15_000)
            Thread.sleep(2000)
            shot("08-mini-player")
            rule.onAllNodes(hasContentDescription("Pause")).onFirst().performClick()
            rule.waitUntilAtLeastOneExists(hasContentDescription("Play"), 5_000)
            rule.onNode(hasContentDescription("Open Now Playing")).performClick()
            rule.waitText("PLAYING FROM")
        }
        shot("09-now-playing")
    }

    /**
     * Muse, Now Playing's source, lyrics and ratings (M6.5). Needs the test music
     * library and the sonic analysis sidecar.
     */
    @Test fun musicFeatures() {
        connectAndSignIn()
        openLibrary("Music")
        rule.waitText("Muse", 20_000)
        shot("m1-discover")
        rule.onNode(hasSetTextAction()).performTextInput("white noise and static hiss")
        rule.onNode(hasText("Play") and hasClickAction() and !hasContentDescription("Play")).performClick()
        openNowPlaying()
        rule.waitText("white noise and static hiss")
        rule.waitUntilAtLeastOneExists(hasText("Hiss Theory", substring = true) or hasText("Static Kids", substring = true), 10_000)
        shot("m2-now-playing-muse")
        rule.onNode(hasContentDescription("Up Next")).performClick()
        Thread.sleep(1000)
        shot("m3-up-next")
        rule.onNode(hasContentDescription("Close Now Playing")).performClick()

        // An album with an .lrc sidecar: synced lyrics, then a rating.
        tap("Calm Pads", substring = true)
        rule.waitText("Floating")
        rule.onAllNodes(hasContentDescription("Floating")).onFirst().performClick()
        rule.waitText("Radio")
        shot("m4-album")
        tap("Play")
        openNowPlaying()
        rule.waitText("Floating 1")
        rule.onNode(hasContentDescription("Lyrics")).performClick()
        rule.waitText("Floating on a quiet sea")
        Thread.sleep(3000)
        shot("m5-lyrics")
        rule.onNode(hasContentDescription("Lyrics")).performClick()
        rule.onNode(hasContentDescription("3 stars")).performClick()
        // Saved on the server: the stars only stay lit when the request succeeds.
        val rated = hasContentDescription("Rating") and SemanticsMatcher.expectValue(SemanticsProperties.StateDescription, "3 of 5 stars")
        Thread.sleep(1500)
        rule.waitUntilAtLeastOneExists(rated, 5_000)
        shot("m6-rated")
        // Half stars: the left half of the third star.
        rule.onNode(hasContentDescription("2.5 stars")).performClick()
        rule.waitUntilAtLeastOneExists(hasContentDescription("Rating") and SemanticsMatcher.expectValue(SemanticsProperties.StateDescription, "2.5 of 5 stars"), 5_000)
        rule.onNode(hasContentDescription("2.5 stars")).performClick() // clears it again
        rule.onNode(hasContentDescription("Start Radio")).performClick()
        rule.waitText("Floating 1 Radio", 15_000, substring = true)
        shot("m7-track-radio")
    }

    private fun openNowPlaying() {
        if (isTv) {
            rule.waitText("Playing", 20_000)
            tap("Playing")
        } else {
            rule.waitUntilAtLeastOneExists(hasContentDescription("Open Now Playing"), 20_000)
            rule.onNode(hasContentDescription("Open Now Playing")).performClick()
        }
        rule.waitText("PLAYING FROM")
    }

    /** Android TV: Home to a show, a season and back again with the remote alone. */
    @Test fun tvRemoteNavigation() {
        assumeTrue("TV only", isTv)
        connectAndSignIn()
        // Down the shelves until the anime show has focus.
        val anime = hasContentDescription("Test Anime") and isFocused()
        for (i in 0 until 6) {
            if (rule.onAllNodes(anime).fetchSemanticsNodes().isNotEmpty()) break
            key(KeyEvent.KEYCODE_DPAD_DOWN)
        }
        rule.onNode(anime).assertExists()
        key(KeyEvent.KEYCODE_DPAD_CENTER)
        rule.waitText("Seasons")
        shot("t1-show")
        key(KeyEvent.KEYCODE_DPAD_DOWN) // Play → Season 1
        key(KeyEvent.KEYCODE_DPAD_CENTER)
        rule.waitText("Episodes")
        shot("t2-season")
        key(KeyEvent.KEYCODE_BACK)
        rule.waitText("Seasons")
        key(KeyEvent.KEYCODE_BACK)
        rule.waitText("Recently Added", substring = true)
    }

    private fun key(code: Int) {
        rule.waitForIdle()
        Thread.sleep(400)
        InstrumentationRegistry.getInstrumentation().sendKeyDownUpSync(code)
    }

    /** The browse tree Android Auto and Assistant use (MUSIC-14): browse, play a station, an album track and a search. */
    @Test fun androidAutoBrowse() {
        connectAndSignIn()
        val token = SessionToken(context, ComponentName(context, app.marquee.music.MusicService::class.java))
        fun onMain(block: () -> Unit) = InstrumentationRegistry.getInstrumentation().runOnMainSync(block)
        // The browser lives on the main thread; calls go there and their futures are awaited here.
        fun <T> call(block: () -> com.google.common.util.concurrent.ListenableFuture<T>): T {
            var f: com.google.common.util.concurrent.ListenableFuture<T>? = null
            onMain { f = block() }
            return f!!.get(30, TimeUnit.SECONDS)
        }
        val browser = call { MediaBrowser.Builder(context, token).setApplicationLooper(android.os.Looper.getMainLooper()).buildAsync() }
        fun children(id: String) = call { browser.getChildren(id, 0, 500, null) }.value!!
        fun current(): MediaItem? { var m: MediaItem? = null; onMain { m = browser.currentMediaItem }; return m }
        fun waitPlaying(what: String, check: (MediaItem) -> Boolean) {
            val until = System.currentTimeMillis() + 20_000
            while (System.currentTimeMillis() < until) {
                var ok = false
                onMain { ok = browser.isPlaying && browser.currentMediaItem?.let(check) == true }
                if (ok) return
                Thread.sleep(250)
            }
            throw AssertionError("$what didn't start; now ${current()?.mediaMetadata?.title}")
        }
        try {
            val root = call { browser.getLibraryRoot(null) }.value!!
            assertEquals(listOf("For You", "Playlists", "Artists", "Recently Added"), children(root.mediaId).map { it.mediaMetadata.title.toString() })
            val forYou = children("foryou")
            assertTrue(forYou.any { it.mediaMetadata.title.toString() == "Library Radio" })

            // A station: plays real tracks.
            onMain { browser.setMediaItem(MediaItem.Builder().setMediaId("radio:library").build()); browser.prepare(); browser.play() }
            waitPlaying("Library Radio") { it.mediaId.toLongOrNull() != null }

            // Artists → albums → a track: the whole album plays from that track.
            val artist = children("artists").first { it.mediaMetadata.title.toString() == "Calm Pads" }
            val album = children(artist.mediaId).first { it.mediaMetadata.title.toString() == "Floating" }
            val tracks = children(album.mediaId)
            val second = tracks.first { it.mediaMetadata.title.toString() == "Floating 2" }
            onMain { browser.setMediaItem(second); browser.prepare(); browser.play() }
            waitPlaying("Floating 2") { it.mediaMetadata.title.toString() == "Floating 2" }
            var count = 0
            onMain { count = browser.mediaItemCount }
            assertEquals(tracks.size, count)

            // "Play Hiss Theory on Marquee".
            val voice = MediaItem.Builder().setMediaId("").setRequestMetadata(MediaItem.RequestMetadata.Builder().setSearchQuery("Hiss Theory").build()).build()
            onMain { browser.setMediaItem(voice); browser.prepare(); browser.play() }
            waitPlaying("search") { it.mediaMetadata.artist.toString() == "Hiss Theory" }
            onMain { browser.stop() }
        } finally {
            onMain { browser.release() }
        }
    }

    /** Phones: a converted movie and an album download, then play from the device (M7, MUSIC-14). */
    @Test fun downloadAndPlayOffline() {
        assumeTrue("phones and tablets only", !isTv)
        connectAndSignIn()
        openLibrary("Movies")
        rule.waitText("00 Preview Test")
        tap("00 Preview Test")
        rule.waitText("Download", substring = true)
        if (rule.onAllNodesWithText("Downloaded").fetchSemanticsNodes().isNotEmpty()) {
            tap("Downloaded")
            tap("Delete Download")
        }
        tap("Download")
        tap("Low (480p)")
        rule.waitText("Downloaded", 240_000) // converted on the server, then fetched
        shot("d1-downloaded")

        // The album too.
        back()
        rule.waitText("00 Preview Test")
        openLibrary("Music")
        rule.waitText("Calm Pads", substring = true)
        tap("Calm Pads", substring = true)
        rule.onAllNodes(hasContentDescription("Floating")).onFirst().performClick()
        rule.waitText("Radio")
        tap("Download")

        tap("Libraries")
        rule.waitText("Downloads")
        tap("Downloads")
        rule.waitText("Movies and TV")
        rule.waitText("Floating 4", 60_000)
        shot("d2-downloads")
        tap("00 Preview Test")
        val playing = hasContentDescription("Video player") and SemanticsMatcher.expectValue(SemanticsProperties.StateDescription, "Playing")
        rule.waitUntilAtLeastOneExists(playing, 15_000)
        Thread.sleep(3000)
        shot("d3-playing-download")
        back()
        rule.waitText("Floating 2")
        tap("Floating 2")
        rule.waitUntilAtLeastOneExists(hasContentDescription("Pause"), 15_000)
        shot("d4-music-from-device")
        rule.onAllNodes(hasContentDescription("Pause")).onFirst().performClick()

        // Clean up.
        listOf("00 Preview Test", "Floating 1", "Floating 2", "Floating 3", "Floating 4").forEach { t ->
            rule.onAllNodes(hasContentDescription("Delete $t")).fetchSemanticsNodes().firstOrNull()?.let {
                rule.onNode(hasContentDescription("Delete $t")).performClick()
            }
        }
    }

    /** Phones: with the network off, downloads still play and the play is sent once it's back. */
    @Test fun offlinePlayback() {
        assumeTrue("phones and tablets only", !isTv)
        connectAndSignIn()
        openLibrary("Music")
        rule.waitText("Calm Pads", substring = true)
        tap("Calm Pads", substring = true)
        rule.onAllNodes(hasContentDescription("Floating")).onFirst().performClick()
        rule.waitText("Radio")
        if (rule.onAllNodesWithText("Download").fetchSemanticsNodes().isNotEmpty()) tap("Download")
        tap("Libraries")
        tap("Downloads")
        rule.waitText("Floating 4", 60_000)
        rule.waitUntil(60_000) { rule.onAllNodesWithText("In progress").fetchSemanticsNodes().isEmpty() }

        // Offline: the app comes back up without the server.
        shell("svc wifi disable"); shell("svc data disable")
        try {
            scenario.close()
            scenario = ActivityScenario.launch(Intent(context, MainActivity::class.java))
            rule.waitText("Open Downloads", 40_000)
            shot("o1-offline-home")
            tap("Open Downloads")
            rule.waitText("You're offline", substring = true)
            tap("Floating 3")
            rule.waitUntilAtLeastOneExists(hasContentDescription("Pause"), 15_000)
            Thread.sleep(2000)
            shot("o2-offline-playing")
            rule.onAllNodes(hasContentDescription("Pause")).onFirst().performClick()
        } finally {
            shell("svc wifi enable"); shell("svc data enable")
        }
        listOf("Floating 1", "Floating 2", "Floating 3", "Floating 4").forEach { t ->
            if (rule.onAllNodes(hasContentDescription("Delete $t")).fetchSemanticsNodes().isNotEmpty()) rule.onNode(hasContentDescription("Delete $t")).performClick()
        }
    }

    private fun shell(cmd: String) {
        InstrumentationRegistry.getInstrumentation().uiAutomation.executeShellCommand(cmd).close()
        Thread.sleep(1500)
    }

    /** Marquee's player controls: settings restart the stream at a new quality; TV scrubs with previews. */
    @Test fun videoPlayerControls() {
        connectAndSignIn()
        openLibrary("Movies")
        rule.waitText("00 Preview Test")
        tap("00 Preview Test")
        rule.waitUntilAtLeastOneExists(hasText("Play") or hasText("Resume"), 10_000)
        rule.onAllNodes(hasText("Play") or hasText("Resume")).onFirst().performClick()
        val playing = hasContentDescription("Video player") and SemanticsMatcher.expectValue(SemanticsProperties.StateDescription, "Playing")
        rule.waitUntilAtLeastOneExists(playing, 30_000)
        if (isTv) {
            // Controls fade; then the remote scrubs with a preview and select jumps there.
            rule.waitUntil(10_000) { rule.onAllNodes(hasContentDescription("Playback settings")).fetchSemanticsNodes().isEmpty() }
            var before = 0L
            onMainPosition { before = it }
            repeat(3) { key(KeyEvent.KEYCODE_DPAD_RIGHT) }
            rule.waitUntilAtLeastOneExists(hasContentDescription("Position"), 5_000)
            Thread.sleep(1500)
            shot("p1-tv-scrub-preview")
            key(KeyEvent.KEYCODE_DPAD_CENTER)
            rule.waitUntilAtLeastOneExists(playing, 15_000)
            // Three presses: about 30 s further on.
            var after = 0L
            rule.waitUntil(5_000) { onMainPosition { after = it }; after - before >= 25_000 }
            assertTrue("jumped ${after - before} ms", after - before in 25_000..45_000)
        } else {
            showControls()
            Thread.sleep(500)
            shot("p1-controls")
            rule.onNode(hasContentDescription("Playback settings")).performClick()
            rule.waitText("Playback")
            shot("p2-settings")
            rule.onNode(hasScrollToNodeAction()).performScrollToNode(hasText("4 Mbps 720p"))
            tap("4 Mbps 720p")
            rule.waitUntilAtLeastOneExists(playing, 30_000)
            showControls()
            rule.onNode(hasContentDescription("Playback settings")).performClick()
            rule.waitText("Playback")
            rule.onNode(hasScrollToNodeAction()).performScrollToNode(hasText("4 Mbps 720p"))
            rule.onNode(hasText("4 Mbps 720p") and isSelected()).assertExists() // the stream restarted with the cap
            shot("p3-transcoding")
            tap("Close")
        }
        back()
    }

    /** The video player's position in ms, from its progress semantics. */
    private fun onMainPosition(got: (Long) -> Unit) {
        val node = rule.onNode(hasContentDescription("Video player")).fetchSemanticsNode()
        got((node.config[SemanticsProperties.ProgressBarRangeInfo].current * 1000).toLong())
    }

    /** Scrolls whichever scrollable container holds a matching node there; false when none does. */
    private fun scrollTo(m: SemanticsMatcher): Boolean {
        val n = rule.onAllNodes(hasScrollToNodeAction()).fetchSemanticsNodes().size
        return (0 until n).any { i -> runCatching { rule.onAllNodes(hasScrollToNodeAction())[i].performScrollToNode(m) }.isSuccess }
    }

    private fun showControls() {
        if (rule.onAllNodes(hasContentDescription("Playback settings")).fetchSemanticsNodes().isEmpty())
            rule.onNode(hasContentDescription("Video player")).performClick()
        rule.waitUntilAtLeastOneExists(hasContentDescription("Playback settings"), 5_000)
    }

    /** Discover (REQ-1): request a title from Seerr's trending list, see it, withdraw it. Needs Seerr connected. */
    @Test fun discoverAndRequest() {
        connectAndSignIn()
        tap("Libraries")
        rule.waitText("Discover", 15_000)
        tap("Discover")
        val requestable = hasContentDescription("Request ", substring = true)
        rule.waitUntilAtLeastOneExists(requestable, 20_000)
        Thread.sleep(1500)
        shot("r1-discover")
        rule.onAllNodes(requestable).onFirst().performClick()
        rule.waitUntilAtLeastOneExists(hasText("An admin approves", substring = true), 10_000)
        val confirm = hasText("Request") and hasClickAction()
        rule.waitUntil(15_000) { rule.onAllNodes(confirm).fetchSemanticsNodes().any { n -> !n.config.contains(SemanticsProperties.Disabled) } }
        shot("r2-request")
        rule.onAllNodes(confirm).onFirst().performClick()
        // My Requests is at the end of the grid: scroll there once the list has reloaded.
        rule.waitUntil(15_000) { scrollTo(hasText("Withdraw")) }
        shot("r3-my-requests")
        rule.onAllNodes(hasText("Withdraw")).onFirst().performClick()
        rule.waitUntil(10_000) { !scrollTo(hasText("Withdraw")) }
    }

    /** Live TV (LIVE-2/3): guide with a playing preview (phones), What's On, full screen, channel down. Needs a source. */
    @Test fun liveTv() {
        connectAndSignIn()
        tap("Live TV")
        rule.waitText("What's On", 15_000)
        val playing = { name: String -> hasContentDescription(name) and SemanticsMatcher.expectValue(SemanticsProperties.StateDescription, "Playing") }
        if (!isTv) {
            rule.waitUntilAtLeastOneExists(playing("Live preview"), 30_000)
            Thread.sleep(2000)
            shot("l1-guide")
        }
        tap("What's On")
        rule.waitUntilAtLeastOneExists(hasContentDescription("Watch ", substring = true), 10_000)
        shot("l2-whats-on")
        rule.onAllNodes(hasContentDescription("Watch ", substring = true)).onFirst().performClick()
        rule.waitUntilAtLeastOneExists(playing("Live TV"), 30_000)
        Thread.sleep(3000)
        shot("l3-watching")
        if (isTv) key(KeyEvent.KEYCODE_DPAD_DOWN) else rule.onNode(hasContentDescription("Channel down")).performClick()
        rule.waitUntilAtLeastOneExists(playing("Live TV"), 30_000)
        Thread.sleep(3000)
        shot("l4-next-channel")
        back()
    }

    private val adminToken = args.getString("admintoken")?.takeIf { it != "none" }

    /** Calls the server as the test admin (the other viewer in watch-together). */
    private fun adminApi(method: String, path: String, body: String? = null, asToken: String? = null): String {
        val token = asToken ?: adminToken ?: throw org.junit.AssumptionViolatedException("no admin token")
        val req = okhttp3.Request.Builder().url("http://$server/api/v1$path").header("Authorization", "Bearer $token")
            .method(method, body?.let { okhttp3.RequestBody.create("application/json".toMediaType(), it) } ?: if (method == "POST") okhttp3.RequestBody.create(null, ByteArray(0)) else null)
            .build()
        return okhttp3.OkHttpClient().newCall(req).execute().use { it.body!!.string() }
    }

    /** Watch together (SYNC-1): the admin starts a group; this device joins from Home and follows the admin's pause. */
    @Test fun watchTogether() {
        val created = adminApi("POST", "/syncplay/groups", """{"itemId":359,"positionMs":30000}""")
        val id = Regex("\"id\":\"([0-9a-f]+)\"").find(created)!!.groupValues[1]
        try {
            connectAndSignIn()
            rule.waitUntilAtLeastOneExists(hasContentDescription("Join 00 Preview Test"), 30_000)
            shot("w1-home-join")
            rule.onNode(hasContentDescription("Join 00 Preview Test")).performClick()
            rule.waitUntilAtLeastOneExists(hasContentDescription("Watching together"), 30_000)
            rule.waitUntil(20_000) { adminApi("GET", "/syncplay/groups/$id").contains("\"name\":\"Kiddo\"") }
            shot("w2-joined")
            adminApi("POST", "/syncplay/groups/$id/command", """{"action":"pause","positionMs":60000}""")
            val paused = hasContentDescription("Video player") and SemanticsMatcher.expectValue(SemanticsProperties.StateDescription, "Paused")
            rule.waitUntilAtLeastOneExists(paused, 15_000)
            Thread.sleep(1500)
            var at = 0L
            onMainPosition { at = it }
            assertTrue("following the admin to 60 s, at $at", at in 58_000..62_000)
            shot("w3-paused-by-admin")
            back()
        } finally {
            adminApi("POST", "/syncplay/groups/$id/leave")
        }
    }

    /** Moods and styles (MUSIC-18): the Chill mood lists tracks and starts its radio. */
    @Test fun moodsAndStyles() {
        connectAndSignIn()
        openLibrary("Music")
        rule.waitUntil(20_000) { scrollTo(hasContentDescription("Chill mood")) }
        shot("ms1-tiles")
        rule.onNode(hasContentDescription("Chill mood")).performClick()
        rule.waitText("Play Chill Radio")
        rule.waitText("Tracks", 20_000)
        shot("ms2-mood")
        tap("Play Chill Radio")
        if (isTv) rule.waitText("Playing", 15_000)
        else rule.waitUntilAtLeastOneExists(hasContentDescription("Open Now Playing"), 15_000)
    }

    /** Playlist downloads (MUSIC-19): keep "Road Trip" on the device, then remove it. */
    @Test fun playlistDownload() {
        assumeTrue("phones and tablets only", !isTv)
        connectAndSignIn()
        tap("Libraries")
        rule.waitText("Playlists")
        rule.onAllNodesWithText("Playlists").onFirst().performClick()
        rule.waitText("Road Trip")
        tap("Road Trip")
        rule.waitText("Shuffle")
        if (rule.onAllNodesWithText("Remove", substring = true).fetchSemanticsNodes().isNotEmpty()) tap("Remove", substring = true)
        tap("Download")
        val downloaded = SemanticsMatcher.expectValue(SemanticsProperties.StateDescription, "Downloaded")
        rule.waitUntilAtLeastOneExists(downloaded, 60_000)
        shot("pd1-downloaded")
        rule.onNode(downloaded).performClick()
        rule.waitText("Download")
    }

    /** Crossfade (MUSIC-9): with 4 s set, the next track starts before the current one ends. */
    @Test fun crossfade() {
        assumeTrue("phones", !isTv)
        connectAndSignIn()
        val app = context.applicationContext as MarqueeApplication
        InstrumentationRegistry.getInstrumentation().runOnMainSync { app.music.setCrossfade(4) }
        openLibrary("Music")
        rule.waitUntil(20_000) { scrollTo(hasText("Library Radio")) }
        tap("Library Radio")
        rule.waitUntilAtLeastOneExists(hasContentDescription("Open Now Playing"), 20_000)
        rule.onNode(hasContentDescription("Open Now Playing")).performClick()
        rule.waitText("PLAYING FROM")
        Thread.sleep(3000)
        val first = app.music.now.value?.id
        // Jump to 8 s before the end.
        val dur = app.music.position.value.second
        assertTrue("duration known", dur > 10_000)
        InstrumentationRegistry.getInstrumentation().runOnMainSync { app.music.seek(dur - 8_000) }
        // The next track takes over about 4 s before the end, not at it.
        val start = System.currentTimeMillis()
        rule.waitUntil(15_000) { app.music.now.value?.id != first }
        val took = System.currentTimeMillis() - start
        assertTrue("took over after $took ms (expected ~4 s, well before 8 s)", took in 2_000..6_500)
        shot("cf1-after-crossfade")
        InstrumentationRegistry.getInstrumentation().runOnMainSync { app.music.setCrossfade(0); app.music.stop() }
    }

    /** M7 on Android: watchlist, extras, collections and your stats. */
    @Test fun watchlistCollectionsStats() {
        connectAndSignIn()
        openLibrary("Movies")
        rule.waitText("00 Preview Test")
        tap("00 Preview Test")
        rule.waitUntilAtLeastOneExists(hasText("Watchlist") or hasText("On Watchlist"), 10_000)
        if (rule.onAllNodesWithText("On Watchlist").fetchSemanticsNodes().isNotEmpty()) tap("On Watchlist")
        tap("Watchlist")
        rule.waitText("On Watchlist")
        rule.waitUntil(10_000) { scrollTo(hasText("Extras")) }
        shot("m7a-item")
        tap("On Watchlist") // back off the watchlist
        back()
        rule.waitText("Collections")
        tap("Collections")
        rule.waitText("Test Saga", 10_000)
        shot("m7b-collections")
        tap("Test Saga")
        rule.waitText("In this collection", 10_000)
        rule.waitText("15 Thunder")
        tap("Settings")
        tap("Your Stats")
        rule.waitUntilAtLeastOneExists(hasText("Plays") or hasText("Nothing played in this period."), 15_000)
        tap("All time")
        rule.waitText("Plays", 15_000)
        shot("m7c-stats")
    }

    /** RFC 6238 code for a base32 secret, [ahead] steps from now. */
    private fun totp(secret: String, ahead: Int = 0): String {
        val alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
        var bits = 0; var value = 0
        val key = java.io.ByteArrayOutputStream()
        for (c in secret.uppercase()) {
            val i = alphabet.indexOf(c).takeIf { it >= 0 } ?: continue
            value = (value shl 5) or i; bits += 5
            if (bits >= 8) { key.write((value shr (bits - 8)) and 0xFF); bits -= 8 }
        }
        val step = System.currentTimeMillis() / 30_000 + ahead
        val mac = javax.crypto.Mac.getInstance("HmacSHA1").apply { init(javax.crypto.spec.SecretKeySpec(key.toByteArray(), "HmacSHA1")) }
            .doFinal(java.nio.ByteBuffer.allocate(8).putLong(step).array())
        val o = mac[19].toInt() and 15
        val n = ((mac[o].toInt() and 0x7F) shl 24 or ((mac[o + 1].toInt() and 0xFF) shl 16) or ((mac[o + 2].toInt() and 0xFF) shl 8) or (mac[o + 3].toInt() and 0xFF)) % 1_000_000
        return "%06d".format(n)
    }

    /** Two-factor sign-in (USER-9): a password account with an authenticator is asked for a code. */
    @Test fun twoFactorSignIn() {
        val name = "twofa-android-${(1000..9999).random()}"
        val pass = "correct horse battery"
        val id = org.json.JSONObject(adminApi("POST", "/users", """{"username":"$name","displayName":"$name","password":"$pass"}""")).getLong("id")
        try {
            val token = org.json.JSONObject(adminApi("POST", "/auth/login",
                """{"username":"$name","password":"$pass","device":{"clientId":"uitest-$name","name":"UI test","platform":"android"}}""")).getString("token")
            val secret = org.json.JSONObject(adminApi("POST", "/auth/totp/setup", "", token)).getString("secret")
            adminApi("POST", "/auth/totp/enable", """{"code":"${totp(secret)}"}""", token)

            rule.waitUntilAtLeastOneExists(hasSetTextAction(), 10_000)
            rule.onNode(hasSetTextAction()).performTextInput(server)
            rule.onNodeWithText("Connect").performClick()
            rule.waitText("Who's watching?")
            tap("Sign in with a username")
            rule.waitText("Username")
            rule.onNode(hasSetTextAction() and hasText("Username")).performTextInput(name)
            rule.onNode(hasSetTextAction() and hasText("Password")).performTextInput(pass)
            rule.onAllNodesWithText("Sign In").onFirst().performClick()
            rule.waitUntilAtLeastOneExists(androidx.compose.ui.test.hasTestTag("totpCode"), 10_000)
            shot("t1-code-asked")
            rule.onNode(androidx.compose.ui.test.hasTestTag("totpCode")).performTextInput(totp(secret, 1)) // the enable code's step is used up
            rule.onAllNodesWithText("Sign In").onFirst().performClick()
            rule.waitText("Recently Added", 20_000, substring = true)
            shot("t2-signed-in")
        } finally {
            adminApi("DELETE", "/users/$id")
        }
    }

    /** DVR (LIVE-5): record a programme from the guide, see it marked, cancel it from Recordings. */
    @Test fun recording() {
        // A clean DVR, and the profile allowed to record.
        val rules = org.json.JSONArray(adminApi("GET", "/livetv/recording-rules"))
        for (i in 0 until rules.length()) adminApi("DELETE", "/livetv/recording-rules/${rules.getJSONObject(i).getLong("id")}")
        val recs = org.json.JSONArray(adminApi("GET", "/livetv/recordings"))
        for (i in 0 until recs.length()) recs.getJSONObject(i).takeIf { it.getString("status") == "scheduled" }?.let { adminApi("DELETE", "/livetv/recordings/${it.getLong("id")}") }
        val users = org.json.JSONArray(adminApi("GET", "/users"))
        val kid = (0 until users.length()).map { users.getJSONObject(it) }.first { it.getString("displayName") == profile }
        val restrictions = kid.optJSONObject("restrictions") ?: org.json.JSONObject()
        restrictions.put("canRecord", true)
        adminApi("PATCH", "/users/${kid.getLong("id")}", org.json.JSONObject().put("restrictions", restrictions).toString())

        connectAndSignIn()
        tap("Live TV")
        rule.waitText("Recordings", 15_000)
        // Move the guide on 90 minutes so the programmes shown haven't started.
        rule.waitUntilAtLeastOneExists(hasContentDescription("Later"), 10_000)
        rule.onNode(hasContentDescription("Later")).performClick()
        // Wait for the later window: nothing on now ("25m left") is shown any more.
        rule.waitUntil(15_000) { rule.onAllNodesWithText("m left", substring = true).fetchSemanticsNodes().isEmpty() }
        val programme = hasContentDescription(", Marquee News", substring = true) and hasClickAction()
        rule.waitUntilAtLeastOneExists(programme, 10_000)
        rule.onAllNodes(programme).onFirst().performClick()
        rule.waitText("Record series", 10_000)
        shot("r1-programme")
        tap("Record")
        // What was scheduled, from the server.
        var title = ""
        rule.waitUntil(10_000) {
            val list = org.json.JSONArray(adminApi("GET", "/livetv/recordings"))
            title = (0 until list.length()).map { list.getJSONObject(it) }.firstOrNull { it.getString("status") == "scheduled" }?.getString("title") ?: ""
            title.isNotEmpty()
        }
        rule.waitUntilAtLeastOneExists(hasContentDescription("$title, Marquee News, will record"), 10_000)
        shot("r2-guide-marked")

        tap("Recordings")
        rule.waitText("Upcoming", 10_000)
        rule.waitText(title)
        shot("r3-recordings")
        tap("Cancel")
        rule.waitUntil(10_000) { rule.onAllNodesWithText("Upcoming").fetchSemanticsNodes().isEmpty() }
    }
}
