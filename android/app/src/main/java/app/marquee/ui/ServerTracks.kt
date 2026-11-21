package app.marquee.ui

import androidx.media3.common.C
import androidx.media3.common.Player
import androidx.media3.common.TrackSelectionOverride
import androidx.media3.common.Tracks
import app.marquee.api.models.MediaStream
import app.marquee.api.models.PlaybackSession

/**
 * The id given to the subtitle sidecar the server sends (WebVTT), so it can be told apart from
 * subtitles inside the file.
 */
internal const val SERVER_SUBTITLE_ID = "marquee-subtitle"

/**
 * Which of the file's audio tracks the server chose, counted among the file's own audio tracks
 * in order (as ExoPlayer lists them when it plays the file itself), or null when it can't be
 * told: the server's choice isn't one of them, or ExoPlayer sees a different number of tracks.
 */
internal fun serverAudioPosition(streams: List<MediaStream>, chosen: Long?, playerAudioTracks: Int): Int? {
    val own = streams.filter { it.kind == MediaStream.Kind.AUDIO && !it.external }.sortedBy { it.id }
    if (chosen == null || own.size != playerAudioTracks) return null
    return own.indexOfFirst { it.id == chosen }.takeIf { it >= 0 }
}

/**
 * Makes the player use the tracks the server chose for the session (PLAY-8): the audio and
 * subtitles remembered for the show, or the ones the viewer picked. With direct play ExoPlayer
 * has the whole file and would otherwise pick its own audio (the file's default) and show
 * subtitles inside the file by their flags or the device's caption settings. Only the server's
 * subtitle sidecar shows, and none when the server chose none.
 */
internal fun applyServerTracks(player: Player, tracks: Tracks, session: PlaybackSession, streams: List<MediaStream>) {
    val params = player.trackSelectionParameters.buildUpon()
        .clearOverridesOfType(C.TRACK_TYPE_TEXT)
        .clearOverridesOfType(C.TRACK_TYPE_AUDIO)
    val text = tracks.groups.filter { it.type == C.TRACK_TYPE_TEXT }
    val sidecar = text.firstOrNull { g -> (0 until g.length).any { g.getTrackFormat(it).id?.endsWith(SERVER_SUBTITLE_ID) == true } }
    if (sidecar != null) {
        params.setTrackTypeDisabled(C.TRACK_TYPE_TEXT, false)
            .setOverrideForType(TrackSelectionOverride(sidecar.mediaTrackGroup, 0))
    } else {
        params.setTrackTypeDisabled(C.TRACK_TYPE_TEXT, true)
    }
    if (session.decision.method == app.marquee.api.models.PlaybackDecision.Method.DIRECT_PLAY) {
        val audio = tracks.groups.filter { it.type == C.TRACK_TYPE_AUDIO }
        serverAudioPosition(streams, session.audioStreamId, audio.size)?.let { k ->
            val g = audio[k]
            if (g.isSupported) params.setOverrideForType(TrackSelectionOverride(g.mediaTrackGroup, 0))
        }
    }
    player.trackSelectionParameters = params.build()
}
