package app.marquee

import android.content.Intent
import android.graphics.Bitmap
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.ExperimentalTestApi
import androidx.compose.ui.test.SemanticsMatcher
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
import org.junit.After
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
}
