package app.marquee.music

import android.net.Uri
import android.os.Bundle
import androidx.annotation.OptIn
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.util.UnstableApi
import app.marquee.api.models.ItemSummary
import app.marquee.core.Marquee

/** A queue entry for a track: id = track id, URI resolved to a playback session by MusicService. */
@OptIn(UnstableApi::class)
fun trackItem(marquee: Marquee, t: ItemSummary, dj: String? = null): MediaItem = MediaItem.Builder()
    .setMediaId(t.id.toString())
    .setUri("marquee://track/${t.id}")
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
                t.parentId?.let { putLong("album", it) }
                t.audioFormat?.let { AudioQuality.put(this, it) }
            })
            .build(),
    )
    .build()

/** Volume levelling (MUSIC-10): which ReplayGain value to apply. */
enum class Levelling(val label: String) {
    Off("Off"),
    Track("Track"),
    Album("Album"),
    /** Album gain while an album plays in order, track gain otherwise. */
    Auto("Smart"),
}
