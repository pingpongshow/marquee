package app.marquee.ui

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.keyframes
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Backspace
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import kotlinx.coroutines.launch
import kotlin.math.roundToInt

/** The message for a PIN that didn't work: wrong, too many tries, or the server's own reason. */
internal fun pinError(e: Throwable): String = when ((e as? app.marquee.api.infrastructure.ClientException)?.statusCode) {
    401, 403 -> "Wrong PIN. Try again."
    429 -> "Too many tries. Wait a minute and try again."
    else -> serverMessage(e, "Couldn't sign in")
}

/** A profile PIN's length (the server takes four digits). */
internal const val PIN_LENGTH = 4

/** What a key press does on the PIN pad: a digit, rubbing one out, or nothing it knows. */
internal sealed interface PinKey {
    data class Digit(val d: Char) : PinKey
    data object Back : PinKey
}

/** The PIN pad's key for a hardware key (number row, number pad, a TV remote's digits, Backspace), or null. */
internal fun pinKey(key: Key): PinKey? = when (key) {
    Key.Zero, Key.NumPad0 -> PinKey.Digit('0')
    Key.One, Key.NumPad1 -> PinKey.Digit('1')
    Key.Two, Key.NumPad2 -> PinKey.Digit('2')
    Key.Three, Key.NumPad3 -> PinKey.Digit('3')
    Key.Four, Key.NumPad4 -> PinKey.Digit('4')
    Key.Five, Key.NumPad5 -> PinKey.Digit('5')
    Key.Six, Key.NumPad6 -> PinKey.Digit('6')
    Key.Seven, Key.NumPad7 -> PinKey.Digit('7')
    Key.Eight, Key.NumPad8 -> PinKey.Digit('8')
    Key.Nine, Key.NumPad9 -> PinKey.Digit('9')
    Key.Backspace, Key.Delete -> PinKey.Back
    else -> null
}

/** The PIN after a key: digits up to [PIN_LENGTH], and Back rubs out the last. */
internal fun pinAfter(pin: String, k: PinKey): String = when (k) {
    is PinKey.Digit -> if (pin.length < PIN_LENGTH) pin + k.d else pin
    PinKey.Back -> pin.dropLast(1)
}

/**
 * A profile's PIN: a small pad that takes each tap at once (no keyboard to wait for) and signs in
 * as soon as the fourth digit is in. A wrong PIN shakes the dots, clears them and says so. The
 * number keys of a keyboard or remote work too, and on a TV the D-pad moves round the pad.
 * [submit] returns an error message, or null when it worked.
 */
@Composable
internal fun PinPadDialog(name: String, avatarUrl: String?, onDismiss: () -> Unit, submit: suspend (String) -> String?) {
    val marquee = LocalMarquee.current
    val tv = marquee.isTv
    var pin by remember { mutableStateOf("") }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    val shake = remember { Animatable(0f) }
    val scope = rememberCoroutineScope()
    val keys = remember { FocusRequester() }

    fun press(k: PinKey) {
        if (busy) return
        val next = pinAfter(pin, k)
        if (next == pin) return
        pin = next
        if (k is PinKey.Digit) error = null
        if (pin.length < PIN_LENGTH) return
        busy = true
        scope.launch {
            val e = submit(pin)
            busy = false
            if (e != null) {
                error = e
                shake.animateTo(0f, keyframes {
                    durationMillis = 360
                    -14f at 45; 14f at 105; -10f at 165; 10f at 225; -4f at 285; 0f at 360
                })
                pin = ""
            }
        }
    }

    // A hardware keyboard or remote types straight in, whatever has the focus.
    LaunchedEffect(Unit) { if (!tv) runCatching { keys.requestFocus() } }
    Dialog(onDismissRequest = onDismiss, properties = DialogProperties(usePlatformDefaultWidth = false)) {
        Surface(shape = RoundedCornerShape(20.dp), color = MaterialTheme.colorScheme.surface,
            modifier = Modifier.width(if (tv) 340.dp else 288.dp).testTag("pinPad")
                .onPreviewKeyEvent { e ->
                    if (e.type != KeyEventType.KeyDown) return@onPreviewKeyEvent false
                    val k = pinKey(e.key) ?: return@onPreviewKeyEvent false
                    press(k); true
                }
                .focusRequester(keys).focusable(!tv)) {
            Column(Modifier.padding(horizontal = 20.dp, vertical = 18.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                Avatar(name, avatarUrl, 48)
                Text(name, Modifier.padding(top = 8.dp), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold, maxLines = 1)
                Text("Enter PIN", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                // The dots: filled as digits go in; they shake when the PIN is wrong.
                Row(Modifier.padding(vertical = 14.dp).offset { IntOffset(shake.value.roundToInt(), 0) }
                    .semantics { contentDescription = "${pin.length} of $PIN_LENGTH digits entered" },
                    horizontalArrangement = Arrangement.spacedBy(14.dp), verticalAlignment = Alignment.CenterVertically) {
                    repeat(PIN_LENGTH) { i ->
                        val filled = i < pin.length
                        Box(Modifier.size(14.dp).clip(CircleShape)
                            .background(if (filled) Gold else MaterialTheme.colorScheme.surfaceVariant)
                            .border(1.dp, if (error != null) MaterialTheme.colorScheme.error else Gold.copy(alpha = 0.6f), CircleShape))
                    }
                }
                Box(Modifier.height(20.dp), contentAlignment = Alignment.Center) {
                    when {
                        busy -> CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                        error != null -> Text(error!!, Modifier.semantics { liveRegion = LiveRegionMode.Polite }, color = MaterialTheme.colorScheme.error,
                            style = MaterialTheme.typography.bodySmall, maxLines = 2)
                    }
                }
                Column(Modifier.padding(top = 6.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    listOf("123", "456", "789").forEachIndexed { row, digits ->
                        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                            digits.forEach { d -> PinPadKey(d.toString(), focusFirst = tv && row == 0 && d == '1') { press(PinKey.Digit(d)) } }
                        }
                    }
                    Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        Box(Modifier.size(keySize(tv)))
                        PinPadKey("0") { press(PinKey.Digit('0')) }
                        PinPadKey("", description = "Delete", icon = true, enabled = pin.isNotEmpty()) { press(PinKey.Back) }
                    }
                }
                TextButton(onDismiss, Modifier.padding(top = 6.dp).focusRing()) { Text("Cancel") }
            }
        }
    }
}

private fun keySize(tv: Boolean) = if (tv) 64.dp else 60.dp

/** One key of the pad: a plain click (no long-press or double-tap wait), so quick typing never drops a digit. */
@Composable
private fun PinPadKey(label: String, description: String = label, icon: Boolean = false, enabled: Boolean = true, focusFirst: Boolean = false, onPress: () -> Unit) {
    val tv = LocalMarquee.current.isTv
    Box(Modifier.size(keySize(tv)).clip(CircleShape).background(MaterialTheme.colorScheme.surfaceVariant.copy(alpha = if (enabled) 1f else 0.4f))
        .initialFocus(focusFirst)
        .focusCard(onPress, shape = CircleShape)
        .semantics { contentDescription = description }
        .testTag("pin-$description"), contentAlignment = Alignment.Center) {
        if (icon) Icon(Icons.AutoMirrored.Filled.Backspace, null, Modifier.size(22.dp),
            tint = MaterialTheme.colorScheme.onSurface.copy(alpha = if (enabled) 1f else 0.4f))
        else Text(label, fontSize = 24.sp, fontWeight = FontWeight.Medium)
    }
}
