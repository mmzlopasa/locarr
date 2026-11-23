package net.scyde.locarr.notify

import net.scyde.locarr.api.Notification
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class PollLogicTest {
    private fun n(id: Long, read: Boolean = false, type: String = "request.available", req: Long? = 5, issue: Long? = null) =
        Notification(id = id, type = type, title = "t$id", read = read, requestId = req, issueId = issue)

    @Test fun firstPollOnlyRemembersWhereTheListEnds() {
        val p = PollLogic.plan(0, listOf(n(30)), latestId = 30)
        assertTrue(p.show.isEmpty())
        assertEquals(30L, p.newLastId)
    }

    @Test fun showsNewUnreadOnly() {
        val p = PollLogic.plan(10, listOf(n(9), n(11), n(12, read = true), n(13)), latestId = 13)
        assertEquals(listOf(11L, 13L), p.show.map { it.id })
        assertEquals(0, p.summarized)
        assertEquals(13L, p.newLastId)
    }

    @Test fun manyBecomeASummary() {
        val items = (11L..20L).map { n(it) }
        val p = PollLogic.plan(10, items, latestId = 20)
        assertEquals(PollLogic.MAX_INDIVIDUAL, p.show.size)
        assertEquals(10 - PollLogic.MAX_INDIVIDUAL, p.summarized)
        assertEquals(20L, p.show.last().id)
    }

    @Test fun nothingNewKeepsThePosition() {
        val p = PollLogic.plan(50, emptyList(), latestId = 0)
        assertEquals(50L, p.newLastId)
        assertTrue(p.show.isEmpty())
    }

    @Test fun channelsAndRoutes() {
        assertEquals(Channels.NEW_REQUESTS, PollLogic.channelFor("request.created"))
        assertEquals(Channels.ISSUES, PollLogic.channelFor("issue.comment"))
        assertEquals(Channels.REQUESTS, PollLogic.channelFor("request.declined"))
        assertEquals(Channels.REQUESTS, PollLogic.channelFor("something.new"))
        assertEquals("request/5", PollLogic.routeFor(n(1)))
        assertEquals("issue/9", PollLogic.routeFor(n(1, type = "issue.created", issue = 9)))
        assertEquals("notifications", PollLogic.routeFor(n(1, req = null)))
    }
}
