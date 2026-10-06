package app.marquee.music

import androidx.media3.common.C
import androidx.media3.common.audio.AudioProcessor
import org.junit.Assert.assertEquals
import org.junit.Test
import java.nio.ByteBuffer
import java.nio.ByteOrder
import kotlin.math.pow

/** Volume levelling (MUSIC-10): the gain maths and the audio-path processor. */
class LevellingTest {
    private fun lin(db: Double) = 10.0.pow(db / 20).toFloat()

    @Test fun cutsLoudTracks() {
        assertEquals(lin(-8.0), Levelling.linear(Levelling.Gains(-8.0, -6.0, 1.0), albumMode = false), 1e-5f)
        // The cut is limited.
        assertEquals(lin(-15.0), Levelling.linear(Levelling.Gains(-30.0, null, 1.0), albumMode = false), 1e-5f)
    }

    @Test fun albumModeUsesAlbumGainWithTrackFallback() {
        assertEquals(lin(-6.0), Levelling.linear(Levelling.Gains(-8.0, -6.0, 1.0), albumMode = true), 1e-5f)
        assertEquals(lin(-8.0), Levelling.linear(Levelling.Gains(-8.0, null, 1.0), albumMode = true), 1e-5f)
    }

    @Test fun boostsQuietTracksWithinPeakAndLimit() {
        // +4 dB with plenty of headroom: all of it.
        assertEquals(lin(4.0), Levelling.linear(Levelling.Gains(4.0, null, 0.3), false), 1e-5f)
        // +10 dB asked: at most +6 dB.
        assertEquals(lin(6.0), Levelling.linear(Levelling.Gains(10.0, null, 0.1), false), 1e-5f)
        // Peak 0.8: no more than 1/0.8, so peak x gain stays <= 1.
        val g = Levelling.linear(Levelling.Gains(5.0, null, 0.8), false)
        assertEquals(1.25f, g, 1e-5f)
        // Unknown peak, or already at full scale: no boost.
        assertEquals(1f, Levelling.linear(Levelling.Gains(5.0, null, null), false), 0f)
        assertEquals(1f, Levelling.linear(Levelling.Gains(5.0, null, 1.2), false), 0f)
    }

    @Test fun noDataMeansUnity() {
        assertEquals(1f, Levelling.linear(null, false), 0f)
        assertEquals(1f, Levelling.linear(Levelling.Gains(null, null, null), true), 0f)
    }

    @Test fun albumOrder() {
        assertEquals(listOf(true, true, false, false), Levelling.albumOrder(listOf(1L, 1L, 2L, null), shuffled = false))
        assertEquals(listOf(false, false, false, false), Levelling.albumOrder(listOf(1L, 1L, 2L, null), shuffled = true))
    }

    /** The gain is applied to the very first samples of a track, and switches with the track. */
    @Test fun processorAppliesGainFromTheFirstSample() {
        TrackGains.put(9001, Levelling.Gains(-6.0, null, 1.0), persist = false)
        TrackGains.put(9002, Levelling.Gains(6.0, null, 0.25), persist = false)
        val p = LevelProcessor()
        p.configure(AudioProcessor.AudioFormat(44100, 2, C.ENCODING_PCM_16BIT))
        p.flush()
        fun run(samples: ShortArray): ShortArray {
            val inBuf = ByteBuffer.allocateDirect(samples.size * 2).order(ByteOrder.nativeOrder())
            samples.forEach { inBuf.putShort(it) }
            inBuf.flip()
            p.queueInput(inBuf)
            val out = p.output
            return ShortArray(out.remaining() / 2) { out.getShort() }
        }
        p.setTrack(9001)
        val cut = run(shortArrayOf(10000, -10000, 20000, -20000))
        assertEquals(listOf(5012, -5012, 10024, -10024), cut.map { it.toInt() })
        p.setTrack(9002)
        val boosted = run(shortArrayOf(1000, -1000, 30000, -30000))
        // +6 dB (x1.995), clamped to the 16-bit range on the loud samples.
        assertEquals(listOf(1995, -1995, Short.MAX_VALUE.toInt(), Short.MIN_VALUE.toInt()), boosted.map { it.toInt() })
    }
}
