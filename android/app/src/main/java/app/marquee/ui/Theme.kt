package app.marquee.ui

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

val Gold = Color(0xFFE8B84A)
val Ink = Color(0xFF0B0B0F)
val Surface1 = Color(0xFF16161C)
val Surface2 = Color(0xFF202029)

@Composable
fun MarqueeTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = darkColorScheme(
            primary = Gold, onPrimary = Color.Black, secondary = Gold,
            background = Ink, surface = Surface1, surfaceVariant = Surface2,
            onBackground = Color(0xFFEDEDF2), onSurface = Color(0xFFEDEDF2), onSurfaceVariant = Color(0xFFA0A0AE),
        ),
        content = content,
    )
}
