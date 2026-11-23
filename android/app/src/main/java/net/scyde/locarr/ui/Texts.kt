package net.scyde.locarr.ui

import android.content.Context
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import net.scyde.locarr.api.Explanation
import net.scyde.locarr.api.Notification
import net.scyde.locarr.api.RequestExplanation

/** Replaces `{{name}}` placeholders (the web client's format) with [params]; unknown ones become "". */
fun interpolate(template: String, params: Map<String, String>): String =
    Regex("""\{\{\s*([a-zA-Z0-9_]+)\s*\}\}""").replace(template) { params[it.groupValues[1]] ?: "" }

/** A JSON value as text: arrays joined with ", ", null as "". */
fun JsonElement?.text(): String = when (this) {
    null, JsonNull -> ""
    is JsonPrimitive -> contentOrNull ?: ""
    is JsonArray -> joinToString(", ") { it.text() }
    is JsonObject -> toString()
}

fun JsonObject?.texts(): Map<String, String> = this?.mapValues { it.value.text() } ?: emptyMap()

/** The translated text for a server code, or [fallback] (the server's English) when the app doesn't know it. */
fun codeText(ctx: Context, key: String, params: Map<String, String> = emptyMap(), fallback: String = ""): String {
    val id = CODE_STRINGS[key] ?: return fallback.ifBlank { key.substringAfterLast('.') }
    return interpolate(ctx.getString(id), params)
}

/** Formats an ISO timestamp param as a short local date and time. */
private fun whenText(v: String): String = if (v.isBlank()) "–" else formatDateTime(v)

/** "Why isn't it here?" in the user's language (same rules as the web client). */
fun explanationText(ctx: Context, ex: Explanation): String {
    val p = ex.params.texts()
    val reason = ex.reasons.firstOrNull()?.let { codeText(ctx, "rejectionCodes.${it.code}", fallback = it.example) } ?: ""
    val vars = mapOf(
        "progress" to ((p["progress"]?.toDoubleOrNull() ?: 0.0) * 100).toInt().toString(),
        "title" to (p["title"] ?: ""),
        "state" to codeText(ctx, "downloadStates.${p["state"]}", fallback = p["state"] ?: ""),
        "detail" to (p["detail"] ?: ""),
        "searches" to (p["searches"] ?: "0"),
        "candidates" to (p["candidates"] ?: "0"),
        "next" to whenText(p["nextSearch"] ?: ""),
        "from" to whenText(p["availableFrom"] ?: ""),
        "retry" to whenText(p["retryAt"] ?: ""),
        "reason" to reason,
        "accepted" to (p["accepted"] ?: "0"),
    )
    if (ex.code == "grab_failed" && p["detail"].isNullOrBlank()) return codeText(ctx, "explain.codes.grab_failed_unknown", vars, ex.message)
    return codeText(ctx, "explain.codes.${ex.code}", vars, ex.message)
}

fun explanationText(ctx: Context, ex: RequestExplanation) =
    explanationText(ctx, Explanation(ex.code, ex.tone, ex.message, ex.params))

/** One line for a notification (title of the item, actor, comment). */
fun notificationText(ctx: Context, n: Notification): String {
    val p = n.params.texts() + mapOf("title" to n.title, "actor" to (n.actor?.username ?: ""), "message" to (n.message ?: ""))
    return codeText(ctx, "notifications.types.${n.type.replace('.', '_')}", p, n.message ?: n.title)
}
