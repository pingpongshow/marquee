package app.marquee.music

import android.content.SharedPreferences
import android.media.audiofx.Equalizer
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Equalizer
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Slider
import androidx.compose.material3.SliderDefaults
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import app.marquee.ui.Gold
import app.marquee.ui.LocalMusic
import app.marquee.ui.focusRing
import kotlin.math.ln
import kotlin.math.roundToInt

/**
 * The music equaliser: ten bands from 31 Hz to 16 kHz (the same as the web and Apple apps),
 * presets or custom gains of ±12 dB, kept per device in the music preferences.
 */
data class EqSettings(val on: Boolean = false, val preset: String = "Flat", val gains: List<Float> = EqPreset.Flat.gains) {
    fun save(prefs: SharedPreferences) = prefs.edit()
        .putBoolean("eq.on", on).putString("eq.preset", preset).putString("eq.gains", gains.joinToString(",")).apply()

    companion object {
        val frequencies = listOf(31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000)
        const val MAX_DB = 12f
        const val CUSTOM = "Custom"

        fun load(prefs: SharedPreferences): EqSettings {
            val gains = prefs.getString("eq.gains", null)?.split(',')?.mapNotNull { it.toFloatOrNull() }?.takeIf { it.size == frequencies.size }
            return EqSettings(prefs.getBoolean("eq.on", false), prefs.getString("eq.preset", null) ?: "Flat", gains ?: EqPreset.Flat.gains)
        }
    }
}

enum class EqPreset(val label: String, val gains: List<Float>) {
    Flat("Flat", listOf(0f, 0f, 0f, 0f, 0f, 0f, 0f, 0f, 0f, 0f)),
    BassBoost("Bass Boost", listOf(6f, 5f, 4f, 2.5f, 1f, 0f, 0f, 0f, 0f, 0f)),
    BassReducer("Bass Reducer", listOf(-6f, -5f, -4f, -2.5f, -1f, 0f, 0f, 0f, 0f, 0f)),
    TrebleBoost("Treble Boost", listOf(0f, 0f, 0f, 0f, 0f, 1f, 2.5f, 4f, 5f, 6f)),
    Vocal("Vocal", listOf(-2f, -2f, -1f, 1f, 3f, 4f, 3.5f, 2f, 0f, -1f)),
    Rock("Rock", listOf(4.5f, 3.5f, 2f, 0f, -1f, -1f, 0.5f, 2f, 3f, 4f)),
    Pop("Pop", listOf(-1f, 0f, 1.5f, 3f, 4f, 3f, 1.5f, 0f, -0.5f, -1f)),
    Jazz("Jazz", listOf(3f, 2f, 1f, 2f, -1f, -1f, 0f, 1f, 2f, 3f)),
    Classical("Classical", listOf(4f, 3f, 2f, 1f, -1f, -1f, 0f, 2f, 3f, 4f)),
    Electronic("Electronic", listOf(5f, 4f, 1f, 0f, -2f, 1.5f, 0.5f, 1f, 4f, 5f)),
    Loudness("Loudness", listOf(6f, 4f, 0f, 0f, -2f, 0f, -1f, -3f, 3f, 1f)),
}

/**
 * Applies [EqSettings] to an audio session with the platform equaliser. The device's own
 * bands (often five) take the gain the ten-band curve has at their centre frequencies.
 */
class SessionEqualizer(sessionId: Int) {
    private val eq: Equalizer? = runCatching { Equalizer(0, sessionId) }.getOrNull()
    val bands: Int get() = eq?.numberOfBands?.toInt() ?: 0

    fun apply(s: EqSettings) {
        val e = eq ?: return
        runCatching {
            val (lo, hi) = e.bandLevelRange.let { it[0].toInt() to it[1].toInt() }
            for (b in 0 until e.numberOfBands) {
                val hz = e.getCenterFreq(b.toShort()) / 1000.0 // milliHertz
                val mb = (gainAt(s.gains, hz) * 100).roundToInt().coerceIn(lo, hi)
                e.setBandLevel(b.toShort(), mb.toShort())
            }
            e.enabled = s.on
        }
    }

    fun release() { runCatching { eq?.release() } }

    companion object {
        /** The curve's gain at a frequency: linear between bands on a log scale, flat past the ends. */
        fun gainAt(gains: List<Float>, hz: Double): Float {
            val f = EqSettings.frequencies
            if (hz <= f.first()) return gains.first()
            if (hz >= f.last()) return gains.last()
            val i = f.indexOfLast { it <= hz }
            val t = (ln(hz) - ln(f[i].toDouble())) / (ln(f[i + 1].toDouble()) - ln(f[i].toDouble()))
            return (gains[i] + (gains[i + 1] - gains[i]) * t).toFloat()
        }
    }
}

/** The EQ button in Now Playing: gold while the equaliser is on. */
@Composable
fun EqButton() {
    val music = LocalMusic.current
    val eq by music.eq.collectAsState()
    var open by remember { mutableStateOf(false) }
    IconButton({ open = true }, Modifier.focusRing()) {
        Icon(Icons.Filled.Equalizer, "Equaliser", tint = if (eq.on) Gold else MaterialTheme.colorScheme.onSurfaceVariant)
    }
    if (open) EqSheet { open = false }
}

/** The equaliser: on/off, presets and the ten bands. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun EqSheet(onClose: () -> Unit) {
    val music = LocalMusic.current
    val eq by music.eq.collectAsState()
    Dialog(onClose) {
        Surface(shape = RoundedCornerShape(16.dp), color = MaterialTheme.colorScheme.surface, modifier = Modifier.widthIn(max = 560.dp)) {
            Column(Modifier.heightIn(max = 640.dp).verticalScroll(rememberScrollState()).padding(20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text("Equaliser", Modifier.weight(1f), style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
                    Switch(eq.on, { music.setEq(eq.copy(on = it)) }, Modifier.focusRing().semantics { contentDescription = "Equaliser on" })
                }
                when {
                    MusicService.equalizerBands == 0 -> Text("This device doesn't offer an equaliser, so these settings can't be applied here.",
                        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
                    else -> Text("Applies to music playing on this device (not on Cast devices).",
                        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    EqPreset.entries.forEach { p ->
                        FilterChip(eq.preset == p.label, { music.setEq(EqSettings(on = true, preset = p.label, gains = p.gains)) }, { Text(p.label) },
                            Modifier.focusRing(RoundedCornerShape(8.dp)))
                    }
                    if (eq.preset == EqSettings.CUSTOM) FilterChip(true, {}, { Text(EqSettings.CUSTOM) })
                }
                EqSettings.frequencies.forEachIndexed { i, hz ->
                    val g = eq.gains[i]
                    val label = if (hz >= 1000) "${hz / 1000} kHz" else "$hz Hz"
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(label, Modifier.width(60.dp), style = MaterialTheme.typography.labelMedium)
                        Slider(
                            g, { v -> music.setEq(eq.copy(on = true, preset = EqSettings.CUSTOM, gains = eq.gains.toMutableList().also { it[i] = v.roundToInt().toFloat() })) },
                            Modifier.weight(1f).focusRing(RoundedCornerShape(8.dp)).semantics { contentDescription = "$label gain" },
                            valueRange = -EqSettings.MAX_DB..EqSettings.MAX_DB, steps = 23,
                            colors = SliderDefaults.colors(thumbColor = Gold, activeTrackColor = Gold),
                        )
                        Text((if (g > 0) "+" else "") + "%.0f dB".format(g), Modifier.width(56.dp).padding(start = 8.dp), style = MaterialTheme.typography.labelMedium)
                    }
                }
                Row {
                    TextButton({ music.setEq(EqSettings(on = eq.on)) }, Modifier.focusRing()) { Text("Reset") }
                    Row(Modifier.weight(1f), horizontalArrangement = Arrangement.End) { TextButton(onClose, Modifier.focusRing()) { Text("Done") } }
                }
            }
        }
    }
}
