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

    private fun showControls() {
        if (rule.onAllNodes(hasContentDescription("Playback settings")).fetchSemanticsNodes().isEmpty())
            rule.onNode(hasContentDescription("Video player")).performClick()
        rule.waitUntilAtLeastOneExists(hasContentDescription("Playback settings"), 5_000)
    }
}
