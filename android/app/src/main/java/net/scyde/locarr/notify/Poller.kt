package net.scyde.locarr.notify

import net.scyde.locarr.api.Notification

/** What one poll of `GET /notifications?since=` should show. */
data class PollPlan(val show: List<Notification>, val summarized: Int, val newLastId: Long)

object PollLogic {
    /** At most this many separate notifications per poll; the rest become one summary. */
    const val MAX_INDIVIDUAL = 4

    /**
     * [lastSeen] is the largest id handled before. The first poll for an account (0) shows
     * nothing and only remembers where the list ends, so signing in never floods the shade
     * with old news. Read items (seen in the web or another device) are skipped.
     */
    fun plan(lastSeen: Long, items: List<Notification>, latestId: Long): PollPlan {
        val newest = maxOf(latestId, items.maxOfOrNull { it.id } ?: 0L, lastSeen)
        if (lastSeen <= 0L) return PollPlan(emptyList(), 0, newest)
        val fresh = items.filter { it.id > lastSeen && !it.read }.sortedBy { it.id }
        val shown = fresh.takeLast(MAX_INDIVIDUAL)
        return PollPlan(shown, fresh.size - shown.size, newest)
    }

    /** The notification channel for a notification type. */
    fun channelFor(type: String): String = when {
        type == "request.created" -> Channels.NEW_REQUESTS
        type.startsWith("issue.") -> Channels.ISSUES
        else -> Channels.REQUESTS
    }

    /** Where tapping the notification goes. */
    fun routeFor(n: Notification): String = when {
        n.issueId != null -> "issue/${n.issueId}"
        n.requestId != null -> "request/${n.requestId}"
        else -> "notifications"
    }
}

object Channels {
    const val REQUESTS = "requests"
    const val NEW_REQUESTS = "new_requests"
    const val ISSUES = "issues"
}
