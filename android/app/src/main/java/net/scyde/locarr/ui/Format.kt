package net.scyde.locarr.ui

import java.time.Instant
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.util.Locale

fun parseInstant(iso: String?): Instant? {
    if (iso.isNullOrBlank()) return null
    return runCatching { OffsetDateTime.parse(iso).toInstant() }.getOrNull()
        ?: runCatching { Instant.parse(iso) }.getOrNull()
        ?: runCatching { LocalDate.parse(iso.take(10)).atStartOfDay(ZoneId.systemDefault()).toInstant() }.getOrNull()
}

fun formatDateTime(iso: String?): String {
    val i = parseInstant(iso) ?: return iso ?: ""
    return DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM, FormatStyle.SHORT).withLocale(Locale.getDefault())
        .format(i.atZone(ZoneId.systemDefault()))
}

fun formatDate(iso: String?): String {
    if (iso.isNullOrBlank()) return ""
    val d = runCatching { LocalDate.parse(iso.take(10)) }.getOrNull() ?: return iso
    return DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM).withLocale(Locale.getDefault()).format(d)
}

fun formatBytes(b: Long?): String {
    if (b == null || b <= 0) return "0 B"
    val units = listOf("B", "KB", "MB", "GB", "TB", "PB")
    var v = b.toDouble(); var u = 0
    while (v >= 1024 && u < units.lastIndex) { v /= 1024; u++ }
    return if (u == 0) "$b B" else String.format(Locale.getDefault(), "%.1f %s", v, units[u])
}

fun formatDuration(seconds: Long?): String {
    if (seconds == null || seconds < 0) return "–"
    val h = seconds / 3600; val m = (seconds % 3600) / 60; val s = seconds % 60
    return when { h > 0 -> "${h}h ${m}m"; m > 0 -> "${m}m ${s}s"; else -> "${s}s" }
}

fun percent(p: Double) = "${(p.coerceIn(0.0, 1.0) * 100).toInt()} %"
