package app.marquee

import android.content.Intent
import android.graphics.Bitmap
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.ExperimentalTestApi
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.hasSetTextAction
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
        rule.waitText("Continue Watching", substring = true)
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
     * Sonic Sage, Now Playing's source, lyrics and ratings (M6.5). Needs the test music
     * library and the sonic analysis sidecar.
     */
    @Test fun musicFeatures() {
        connectAndSignIn()
        openLibrary("Music")
        rule.waitText("Sonic Sage", 20_000)
        shot("m1-discover")
        rule.onNode(hasSetTextAction()).performTextInput("white noise and static hiss")
        rule.onNode(hasText("Play") and hasClickAction() and !hasContentDescription("Play")).performClick()
        openNowPlaying()
        rule.waitText("white noise and static hiss")
        rule.waitUntilAtLeastOneExists(hasText("Hiss Theory", substring = true) or hasText("Static Kids", substring = true), 10_000)
        shot("m2-now-playing-sage")
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
        rule.onNode(hasContentDescription("3 stars")).performClick() // clears it again
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
        key(KeyEvent.KEYCODE_DPAD_DOWN) // Continue Watching → Recently Added Anime
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
        rule.waitText("Continue Watching")
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
}
