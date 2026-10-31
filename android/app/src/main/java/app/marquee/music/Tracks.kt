package app.marquee.music

import android.net.Uri
import android.os.Bundle
import androidx.annotation.OptIn
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.util.UnstableApi
import app.marquee.api.models.ItemSummary
import app.marquee.core.Marquee

/**
 * A queue entry for a track: id = track id, URI resolved to a playback session by MusicService.
 * context is the station or mix it came from (Station.context), sent with its playback for the
 * playlist statistics (MUSIC-17); a DJ pick is always "dj". radio marks the entries a radio
 * added itself, which it may replace when it's steered.
 */
@OptIn(UnstableApi::class)
fun trackItem(marquee: Marquee, t: ItemSummary, dj: String? = null, context: String? = null, radio: Boolean = false): MediaItem = MediaItem.Builder()
    .setMediaId(t.id.toString())
    .setUri(trackUri(t.id, if (dj != null) "dj" else context))
    .setMediaMetadata(
        MediaMetadata.Builder()
            .setTitle(t.title)
            .setArtist(t.artistCredit ?: t.grandparentTitle)
            .setAlbumTitle(t.parentTitle)
            .setAlbumArtist(t.grandparentTitle)
            .setDurationMs(t.durationMs)
            .setArtworkUri(marquee.imageUrl(t.images?.poster, 512)?.let(Uri::parse))
            .setIsBrowsable(false)
            .setIsPlayable(true)
            .setMediaType(MediaMetadata.MEDIA_TYPE_MUSIC)
            .setExtras(Bundle().apply {
                if (dj != null) putString("dj", dj)
                (if (dj != null) "dj" else context)?.let { putString("context", it) }
                if (radio) putBoolean("radio", true)
                t.parentId?.let { putLong("album", it) }
                t.audioFormat?.let { AudioQuality.put(this, it) }
                // The heart as the list had it, for Up Next rows (Now Playing loads the current track's own).
                t.favorite?.let { putBoolean("favorite", it) }
            })
            .build(),
    )
    .build()


/** The play context a queue entry carries (see [trackItem]). */
fun playContext(item: MediaItem): String? = item.mediaMetadata.extras?.getString("context")

/** A track's queue URI; the play context rides along so MusicService can send it when the track starts. */
fun trackUri(id: Long, context: String?): String =
    "marquee://track/$id" + (context?.let { "?context=" + Uri.encode(it) } ?: "")
