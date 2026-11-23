package net.scyde.locarr.notify

import java.net.URI
import java.net.URLDecoder
import java.net.URLEncoder

/**
 * Links into the app: `locarr://open?server=<id>&to=<route>`. Local notifications use them so
 * a tap opens the right server and screen. Only the routes below are accepted, so a link from
 * outside the app cannot open anything else.
 */
data class DeepLink(val serverId: String?, val route: String) {
    fun toUri(): String = buildString {
        append("$SCHEME://open?")
        if (serverId != null) append("server=").append(enc(serverId)).append('&')
        append("to=").append(enc(route))
    }

    companion object {
        const val SCHEME = "locarr"
        private val ALLOWED = listOf(
            Regex("""notifications"""),
            Regex("""request/\d+"""),
            Regex("""issue/\d+"""),
            Regex("""requests"""),
            Regex("""title/(movie|series)/\d+"""),
            Regex("""title/artist/[0-9a-fA-F-]{36}"""),
        )

        fun isAllowedRoute(route: String) = ALLOWED.any { it.matches(route) }

        fun parse(uri: String?): DeepLink? {
            if (uri.isNullOrBlank()) return null
            val u = runCatching { URI(uri) }.getOrNull() ?: return null
            if (!u.scheme.equals(SCHEME, true) || u.host != "open") return null
            val q = (u.rawQuery ?: "").split('&').filter { it.contains('=') }.associate {
                val (k, v) = it.split('=', limit = 2)
                k to URLDecoder.decode(v, "UTF-8")
            }
            val route = q["to"]?.trim('/') ?: return null
            if (!isAllowedRoute(route)) return null
            return DeepLink(q["server"]?.ifBlank { null }, route)
        }

        private fun enc(s: String) = URLEncoder.encode(s, "UTF-8")
    }
}
