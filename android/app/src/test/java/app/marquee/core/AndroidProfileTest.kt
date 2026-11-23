package app.marquee.core

import android.view.Display.HdrCapabilities.HDR_TYPE_DOLBY_VISION
import android.view.Display.HdrCapabilities.HDR_TYPE_HDR10
import android.view.Display.HdrCapabilities.HDR_TYPE_HDR10_PLUS
import android.view.Display.HdrCapabilities.HDR_TYPE_HLG
import app.marquee.api.models.DeviceProfile.Hdr
import app.marquee.core.AndroidProfile.VideoDecoders
import org.junit.Assert.assertEquals
import org.junit.Test

/** HDR plays as it is only when both a decoder and the screen take the format; else the server tone maps. */
class AndroidProfileTest {
    private val everything = VideoDecoders(tenBit = true, hdr10 = true, hdr10Plus = true, dolbyVision = true)
    private val allTypes = setOf(HDR_TYPE_HDR10, HDR_TYPE_HDR10_PLUS, HDR_TYPE_HLG, HDR_TYPE_DOLBY_VISION)

    @Test fun needsTheScreen() {
        assertEquals(emptyList<Hdr>(), AndroidProfile.hdrFormats(everything, emptySet()))
        assertEquals(listOf(Hdr.HDR10, Hdr.HLG), AndroidProfile.hdrFormats(everything, setOf(HDR_TYPE_HDR10, HDR_TYPE_HLG)))
        assertEquals(listOf(Hdr.HDR10, Hdr.HDR10PLUS, Hdr.HLG, Hdr.DOLBY_VISION), AndroidProfile.hdrFormats(everything, allTypes))
    }

    @Test fun needsTheDecoder() {
        assertEquals(emptyList<Hdr>(), AndroidProfile.hdrFormats(VideoDecoders(), allTypes))
        // A 10-bit decoder that doesn't say HDR10: HLG only.
        assertEquals(listOf(Hdr.HLG), AndroidProfile.hdrFormats(VideoDecoders(tenBit = true), allTypes))
        // HDR10 without HDR10+ or Dolby Vision decoders, on a screen with them all.
        assertEquals(listOf(Hdr.HDR10, Hdr.HLG), AndroidProfile.hdrFormats(VideoDecoders(tenBit = true, hdr10 = true), allTypes))
    }
}
