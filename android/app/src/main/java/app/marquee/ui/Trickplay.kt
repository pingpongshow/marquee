package app.marquee.ui

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.util.LruCache
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import app.marquee.api.models.Trickplay
import app.marquee.core.Marquee
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.Request

/** Seek previews (PLAY-13): sprite sheets of tiles, one per interval, fetched as needed. */
class TrickplayThumbs(private val marquee: Marquee, private val itemId: Long, val info: Trickplay) {
    private val sheets = LruCache<Int, Bitmap>(3)
    private val perSheet = info.columns * info.rows

    /** The tile for a time: its sheet and where it is on it. */
    fun tile(ms: Long): Triple<Int, IntOffset, IntSize> {
        val n = (ms / info.intervalMs).toInt().coerceIn(0, info.count - 1)
        val i = n % perSheet
        return Triple(n / perSheet, IntOffset((i % info.columns) * info.width, (i / info.columns) * info.height), IntSize(info.width, info.height))
    }

    suspend fun sheet(n: Int): Bitmap? {
        sheets.get(n)?.let { return it }
        val base = marquee.baseUrl ?: return null
        return withContext(Dispatchers.IO) {
            runCatching {
                marquee.http.newCall(Request.Builder().url("$base/api/v1/items/$itemId/trickplay/$n").build()).execute().use { r ->
                    if (!r.isSuccessful) return@use null
                    BitmapFactory.decodeStream(r.body!!.byteStream())
                }
            }.getOrNull()?.also { sheets.put(n, it) }
        }
    }

    companion object {
        suspend fun load(marquee: Marquee, itemId: Long): TrickplayThumbs? = withContext(Dispatchers.IO) {
            runCatching { marquee.playback.getTrickplay(itemId) }.getOrNull()?.takeIf { it.count > 0 }?.let { TrickplayThumbs(marquee, itemId, it) }
        }
    }
}

/** The preview above the scrubber: the frame at a time, with the time under it. */
@Composable
fun PreviewThumb(thumbs: TrickplayThumbs, ms: Long, modifier: Modifier = Modifier) {
    val (sheet, offset, size) = thumbs.tile(ms)
    val bitmap by produceState<Bitmap?>(null, sheet) { value = thumbs.sheet(sheet) }
    val w = 200.dp
    val h = w * thumbs.info.height / thumbs.info.width
    Column(modifier, horizontalAlignment = Alignment.CenterHorizontally) {
        Canvas(Modifier.size(w, h).clip(RoundedCornerShape(6.dp)).border(2.dp, Color.White, RoundedCornerShape(6.dp))) {
            bitmap?.let {
                drawImage(it.asImageBitmap(), srcOffset = offset, srcSize = size,
                    dstSize = IntSize(this.size.width.toInt(), this.size.height.toInt()))
            }
        }
        Text(formatTime(ms), color = Color.White, fontWeight = FontWeight.Bold)
    }
}
