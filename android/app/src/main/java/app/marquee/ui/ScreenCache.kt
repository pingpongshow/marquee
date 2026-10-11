package app.marquee.ui

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.State
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewmodel.compose.viewModel

/**
 * What a screen loaded, kept while the screen is on the back stack (a ViewModel scoped to its
 * navigation entry). Going back to a list then shows the same items straight away, so its
 * saved scroll position (and, on TV, the focused item) can be restored instead of the list
 * starting again from the top while it reloads.
 */
class ScreenCache : ViewModel() {
    val values = HashMap<String, Any?>()
}

/** A value made once per screen [key] and kept while the screen is on the back stack. */
@Suppress("UNCHECKED_CAST")
@Composable
fun <T> rememberCached(key: String, init: () -> T): T {
    val cache: ScreenCache = viewModel()
    return remember(key) { cache.values.getOrPut(key, init) as T }
}

/**
 * Loads a value, keeping the last one while the screen is on the back stack: it's shown at
 * once on coming back and refreshed in place (a failed refresh keeps it).
 */
@Composable
fun <T : Any> cachedLoad(key: String, vararg keys: Any?, load: suspend () -> T?): State<T?> {
    val state = rememberCached<MutableState<T?>>(key) { mutableStateOf(null) }
    LaunchedEffect(key, *keys) { load()?.let { state.value = it } }
    return state
}

/** The item last opened from a list, so on coming back the TV focuses it again (not the first). */
@Composable
fun rememberLastOpened(key: String): MutableState<Long?> = rememberCached(key + ":opened") { mutableStateOf(null) }
