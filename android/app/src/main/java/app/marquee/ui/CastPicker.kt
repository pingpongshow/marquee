package app.marquee.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Cast
import androidx.compose.material.icons.filled.CastConnected
import androidx.compose.material.icons.filled.Speaker
import androidx.compose.material.icons.filled.Tv
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import androidx.mediarouter.media.MediaRouter

/** The Cast button (D82): opens a device picker, gold while casting. Hidden on TVs and without Cast. */
@Composable
fun CastButton(tint: Color = Color.White) {
    val marquee = LocalMarquee.current
    if (marquee.isTv || !marquee.cast.available) return
    val device by marquee.cast.device.collectAsState()
    var open by remember { mutableStateOf(false) }
    IconButton({ open = true }, Modifier.focusRing().testTag("castButton")) {
        if (device != null) Icon(Icons.Filled.CastConnected, "Casting to $device", tint = Gold)
        else Icon(Icons.Filled.Cast, "Cast", tint = tint)
    }
    if (open) CastPicker { open = false }
}

@Composable
private fun CastPicker(onDismiss: () -> Unit) {
    val marquee = LocalMarquee.current
    val cast = marquee.cast
    val device by cast.device.collectAsState()
    val devices by cast.devices.collectAsState()
    DisposableEffect(Unit) {
        cast.discover(true)
        onDispose { cast.discover(false) }
    }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(device?.let { "Casting to $it" } ?: "Cast to") },
        text = {
            Column {
                if (device != null) {
                    Text("Playback continues here when you stop casting.", color = MaterialTheme.colorScheme.onSurfaceVariant)
                } else if (devices.isEmpty()) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                        Text("Looking for TVs and speakers on this network…", Modifier.padding(start = 12.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                } else devices.forEach { r ->
                    Row(Modifier.fillMaxWidth().clickable { cast.connect(r); onDismiss() }.focusRing().padding(vertical = 12.dp),
                        verticalAlignment = Alignment.CenterVertically) {
                        Icon(if (r.deviceType == MediaRouter.RouteInfo.DEVICE_TYPE_TV) Icons.Filled.Tv else Icons.Filled.Speaker, null,
                            tint = MaterialTheme.colorScheme.onSurfaceVariant)
                        Column(Modifier.padding(start = 16.dp)) {
                            Text(r.name)
                            r.description?.takeIf { it.isNotBlank() }?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                        }
                    }
                }
            }
        },
        confirmButton = {
            if (device != null) TextButton({ cast.disconnect(); onDismiss() }, Modifier.focusRing()) { Text("Stop casting") }
            else TextButton(onDismiss, Modifier.focusRing()) { Text("Close") }
        },
    )
}
