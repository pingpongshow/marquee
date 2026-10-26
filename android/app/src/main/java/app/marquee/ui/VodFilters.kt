package app.marquee.ui

import android.content.Context
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.LibraryAddCheck
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.InputChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import app.marquee.api.infrastructure.ClientException
import app.marquee.api.infrastructure.ServerException
import app.marquee.api.models.VodCategory
import app.marquee.api.models.VodStatusSourcesInner
import java.net.URLDecoder
import java.net.URLEncoder

// The filters VOD Movies and VOD Shows share (LIVE-6): genre, decade, minimum rating, watched
// state, source and whether the library already has the title, sent with the list's category, search and sort. Remembered per page.

/** One page's filters. [watched] is the API's value: unwatched, inProgress or watched. */
internal data class VodFilters(
    val genre: String? = null,
    /** The decade's first year (1980 = 1980–1989). */
    val decade: Int? = null,
    /** Out of 10. */
    val minRating: Int? = null,
    val watched: String? = null,
    val sourceId: String? = null,
    /** true = only titles already in a library the user can see; false = only those that aren't. */
    val inLibrary: Boolean? = null,
) {
    val active: Boolean get() = genre != null || decade != null || minRating != null || watched != null || sourceId != null || inLibrary != null
    val yearFrom: Int? get() = decade
    val yearTo: Int? get() = decade?.let { it + 9 }

    /** A line for the preferences ("genre=Drama&decade=1980"). */
    fun encode(): String = listOfNotNull(
        genre?.let { "genre" to it }, decade?.let { "decade" to it.toString() }, minRating?.let { "minRating" to it.toString() },
        watched?.let { "watched" to it }, sourceId?.let { "sourceId" to it }, inLibrary?.let { "inLibrary" to it.toString() },
    ).joinToString("&") { (k, v) -> "$k=${URLEncoder.encode(v, "UTF-8")}" }

    companion object {
        val DECADES = listOf(2020, 2010, 2000, 1990, 1980, 1970, 1960, 1950, 1940, 1930)
        val RATINGS = listOf(9, 8, 7, 6, 5)
        val WATCHED = listOf("unwatched", "inProgress", "watched")

        fun decode(s: String?): VodFilters {
            if (s.isNullOrBlank()) return VodFilters()
            val m = s.split("&").mapNotNull { part ->
                val i = part.indexOf('=')
                if (i <= 0) null else part.substring(0, i) to runCatching { URLDecoder.decode(part.substring(i + 1), "UTF-8") }.getOrNull()
            }.toMap()
            return VodFilters(
                genre = m["genre"]?.takeIf { it.isNotEmpty() },
                decade = m["decade"]?.toIntOrNull(),
                minRating = m["minRating"]?.toIntOrNull()?.takeIf { it in 0..10 },
                watched = m["watched"]?.takeIf { it in WATCHED },
                sourceId = m["sourceId"]?.takeIf { it.isNotEmpty() },
                inLibrary = m["inLibrary"]?.toBooleanStrictOrNull(),
            )
        }

        fun watchedLabel(w: String, shows: Boolean) = when (w) {
            "unwatched" -> if (shows) "Unwatched episodes" else "Unwatched"
            "inProgress" -> "In progress"
            else -> "Watched"
        }

        fun inLibraryLabel(v: Boolean) = if (v) "In my library" else "Not in my library"
    }
}

/** What a VOD page remembers on this device for the server: its category, sort and filters. */
internal class VodPagePrefs(context: Context, server: String?, shows: Boolean) {
    private val prefs = context.getSharedPreferences("marquee.vod", Context.MODE_PRIVATE)
    private val prefix = "${server ?: "-"}.${if (shows) "shows" else "movies"}."
    var category: String
        get() = prefs.getString(prefix + "category", "") ?: ""
        set(v) = prefs.edit().putString(prefix + "category", v).apply()
    var sort: String
        get() = prefs.getString(prefix + "sort", "title") ?: "title"
        set(v) = prefs.edit().putString(prefix + "sort", v).apply()
    var filters: VodFilters
        get() = VodFilters.decode(prefs.getString(prefix + "filters", null))
        set(v) = prefs.edit().putString(prefix + "filters", v.encode()).apply()
}

/**
 * A green ring round the poster of a title a library already has. Chained after focusCard, so it
 * sits inside the poster's clip with the same corners and the focus ring is drawn over it on TV.
 */
internal fun Modifier.inLibraryBorder(inLibrary: Boolean?, shape: RoundedCornerShape = RoundedCornerShape(8.dp)): Modifier =
    if (inLibrary == true) border(2.dp, InLibraryGreen, shape) else this

/** What a VOD tile's state reads as: watched and/or in the library. */
internal fun vodTileState(watched: Boolean, inLibrary: Boolean?): String? =
    listOfNotNull(if (watched) "Watched" else null, if (inLibrary == true) "In your library" else null).joinToString(", ").ifEmpty { null }

/** The small "In your library" label by a VOD film's or show's title. */
@Composable
internal fun InLibraryLabel() {
    androidx.compose.foundation.layout.Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(4.dp)) {
        Icon(Icons.Filled.LibraryAddCheck, null, Modifier.size(16.dp), tint = InLibraryGreen)
        Text("In your library", color = InLibraryGreen, style = MaterialTheme.typography.labelLarge, fontWeight = FontWeight.SemiBold)
    }
}

/**
 * Playing a film or episode failed to start: the source's failure (502) and a busy source (503)
 * get a plain explanation; anything else says what went wrong.
 */
internal fun vodStartError(e: Throwable, what: String): String {
    val code = (e as? ServerException)?.statusCode ?: (e as? ClientException)?.statusCode
    return when (code) {
        502 -> "The source couldn't play this $what just now. Try again in a moment."
        503 -> "The source is busy (it allows only so many streams at once). Try again shortly."
        else -> serverMessage(e, "Couldn't start the $what")
    }
}

/** The active filters as removable chips, and Clear all (nothing while there are none). */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun VodActiveFilters(filters: VodFilters, shows: Boolean, sources: List<VodStatusSourcesInner>, onChange: (VodFilters) -> Unit) {
    if (!filters.active) return
    val chips = listOfNotNull(
        filters.watched?.let { VodFilters.watchedLabel(it, shows) to filters.copy(watched = null) },
        filters.genre?.let { it to filters.copy(genre = null) },
        filters.decade?.let { "${it}s" to filters.copy(decade = null) },
        filters.minRating?.let { "Rated $it+" to filters.copy(minRating = null) },
        filters.sourceId?.let { id -> (sources.firstOrNull { it.id == id }?.name ?: "Source") to filters.copy(sourceId = null) },
        filters.inLibrary?.let { VodFilters.inLibraryLabel(it) to filters.copy(inLibrary = null) },
    )
    FlowRow(Modifier.semantics { contentDescription = "Active filters" }, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        chips.forEach { (label, without) ->
            InputChip(true, { onChange(without) }, { Text(label) }, Modifier.focusRing(RoundedCornerShape(8.dp)).semantics { contentDescription = "Remove filter $label" },
                trailingIcon = { Icon(Icons.Filled.Close, null, Modifier.size(16.dp)) })
        }
        TextButton({ onChange(VodFilters()) }, Modifier.focusRing()) { Text("Clear all") }
    }
}

/** The filter choices, as the library's chips: watched, in my library, genre, decade, rating and (with more than one) source. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun VodFilterDialog(
    filters: VodFilters, shows: Boolean, genres: List<VodCategory>?, sources: List<VodStatusSourcesInner>,
    onChange: (VodFilters) -> Unit, onDismiss: () -> Unit,
) {
    val chip = Modifier.focusRing(RoundedCornerShape(8.dp))
    @Composable
    fun Section(title: String, content: @Composable () -> Unit) {
        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(title, style = MaterialTheme.typography.labelLarge, fontWeight = FontWeight.SemiBold, color = MaterialTheme.colorScheme.onSurfaceVariant)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) { content() }
        }
    }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Filter ${if (shows) "shows" else "movies"}") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()).semantics { contentDescription = "Filters" }, verticalArrangement = Arrangement.spacedBy(14.dp)) {
                Section("Watched") {
                    FilterChip(filters.watched == null, { onChange(filters.copy(watched = null)) }, { Text("Any") }, chip.initialFocus(true))
                    VodFilters.WATCHED.forEach { w ->
                        FilterChip(filters.watched == w, { onChange(filters.copy(watched = w)) }, { Text(VodFilters.watchedLabel(w, shows)) }, chip)
                    }
                }
                Section("In my library") {
                    FilterChip(filters.inLibrary == null, { onChange(filters.copy(inLibrary = null)) }, { Text("Any") }, chip)
                    FilterChip(filters.inLibrary == true, { onChange(filters.copy(inLibrary = true)) }, { Text("Yes") }, chip)
                    FilterChip(filters.inLibrary == false, { onChange(filters.copy(inLibrary = false)) }, { Text("No") }, chip)
                }
                Section("Genre") {
                    FilterChip(filters.genre == null, { onChange(filters.copy(genre = null)) }, { Text("All genres") }, chip)
                    (genres ?: emptyList()).forEach { g ->
                        FilterChip(filters.genre == g.name, { onChange(filters.copy(genre = g.name)) }, { Text("${g.name} (${g.count})") }, chip)
                    }
                }
                Section("Decade") {
                    FilterChip(filters.decade == null, { onChange(filters.copy(decade = null)) }, { Text("Any decade") }, chip)
                    VodFilters.DECADES.forEach { d ->
                        FilterChip(filters.decade == d, { onChange(filters.copy(decade = d)) }, { Text("${d}s") }, chip)
                    }
                }
                Section("Rating") {
                    FilterChip(filters.minRating == null, { onChange(filters.copy(minRating = null)) }, { Text("Any rating") }, chip)
                    VodFilters.RATINGS.forEach { r ->
                        FilterChip(filters.minRating == r, { onChange(filters.copy(minRating = r)) }, { Text("Rated $r+") }, chip)
                    }
                }
                if (sources.size > 1) Section("Source") {
                    FilterChip(filters.sourceId == null, { onChange(filters.copy(sourceId = null)) }, { Text("All sources") }, chip)
                    sources.forEach { s ->
                        FilterChip(filters.sourceId == s.id, { onChange(filters.copy(sourceId = s.id)) }, { Text(s.name) }, chip)
                    }
                }
            }
        },
        confirmButton = { TextButton(onDismiss, Modifier.focusRing()) { Text("Done") } },
        dismissButton = if (filters.active) { { TextButton({ onChange(VodFilters()) }, Modifier.focusRing()) { Text("Clear all") } } } else null,
    )
}
