package net.scyde.locarr.notify

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class DeepLinkTest {
    @Test fun roundTrip() {
        val l = DeepLink("ab12 x&y", "request/42")
        assertEquals(l, DeepLink.parse(l.toUri()))
        assertEquals(DeepLink(null, "notifications"), DeepLink.parse("locarr://open?to=notifications"))
        assertEquals(DeepLink("s1", "title/artist/5b11f4ce-a62d-471e-81fc-a69a8278c7da"),
            DeepLink.parse(DeepLink("s1", "title/artist/5b11f4ce-a62d-471e-81fc-a69a8278c7da").toUri()))
    }

    @Test fun onlyKnownRoutesAndOurScheme() {
        assertNull(DeepLink.parse("locarr://open?to=settings/users"))
        assertNull(DeepLink.parse("locarr://open?to=request/abc"))
        assertNull(DeepLink.parse("https://open?to=request/1"))
        assertNull(DeepLink.parse("locarr://other?to=request/1"))
        assertNull(DeepLink.parse("locarr://open"))
        assertNull(DeepLink.parse(null))
        assertNull(DeepLink.parse("::not a uri"))
    }
}
