package net.scyde.locarr.ui

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.em
import androidx.compose.ui.unit.sp

/** Design tokens: the same values as the web client's CSS variables (web/src/styles.css). */
object Lc {
    val bg = Color(0xFF0B0E12)
    val surface = Color(0xFF11151A)
    val surface2 = Color(0xFF171C22)
    val surface3 = Color(0xFF1E242C)
    val border = Color(0xFF252C35)
    val borderStrong = Color(0xFF343D48)
    val text = Color(0xFFE4E8EE)
    val muted = Color(0xFF8C96A4)
    val faint = Color(0xFF5F6875)
    val accent = Color(0xFF4FD1A5)
    val accentInk = Color(0xFF052419)
    val danger = Color(0xFFF0727A)
    val dangerBg = Color(0xFF2A1417)
    val warning = Color(0xFFE6B450)
    val warningBg = Color(0xFF2A2214)
    val info = Color(0xFF6CB6FF)
    val infoBg = Color(0xFF13202E)
    val success = Color(0xFF4FD1A5)
    val successBg = Color(0xFF10251E)
}

enum class Tone(val fg: Color, val bg: Color) {
    Success(Lc.success, Lc.successBg), Info(Lc.info, Lc.infoBg), Warning(Lc.warning, Lc.warningBg),
    Danger(Lc.danger, Lc.dangerBg), Neutral(Lc.muted, Lc.surface3);

    companion object {
        fun of(s: String?): Tone = when (s) {
            "success" -> Success; "info" -> Info; "warning" -> Warning; "danger", "error" -> Danger; else -> Neutral
        }
    }
}

val Eyebrow = TextStyle(fontWeight = FontWeight.SemiBold, fontSize = 11.sp, letterSpacing = 0.08.em)

@Composable
fun LocarrTheme(content: @Composable () -> Unit) {
    val base = Typography()
    MaterialTheme(
        colorScheme = darkColorScheme(
            primary = Lc.accent, onPrimary = Lc.accentInk, secondary = Lc.accent, onSecondary = Lc.accentInk,
            background = Lc.bg, onBackground = Lc.text, surface = Lc.surface, onSurface = Lc.text,
            surfaceVariant = Lc.surface2, onSurfaceVariant = Lc.muted, outline = Lc.borderStrong, outlineVariant = Lc.border,
            error = Lc.danger, onError = Lc.bg, surfaceContainer = Lc.surface, surfaceContainerHigh = Lc.surface2,
            surfaceContainerHighest = Lc.surface3, surfaceContainerLow = Lc.surface, surfaceContainerLowest = Lc.bg,
            secondaryContainer = Lc.surface3, onSecondaryContainer = Lc.text, primaryContainer = Lc.successBg, onPrimaryContainer = Lc.accent,
        ),
        typography = base.copy(
            headlineMedium = base.headlineMedium.copy(fontWeight = FontWeight.Bold, letterSpacing = (-0.01).em),
            headlineSmall = base.headlineSmall.copy(fontWeight = FontWeight.Bold, letterSpacing = (-0.01).em),
            titleLarge = base.titleLarge.copy(fontWeight = FontWeight.SemiBold),
            titleMedium = base.titleMedium.copy(fontWeight = FontWeight.SemiBold),
        ),
        content = content,
    )
}
