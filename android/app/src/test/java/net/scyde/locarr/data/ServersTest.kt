package net.scyde.locarr.data

import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

private class MemoryKv : KeyValue {
    val map = mutableMapOf<String, String>()
    override suspend fun read(key: String) = map[key]
    override suspend fun write(key: String, value: String?) { if (value == null) map.remove(key) else map[key] = value }
}

/** Reverses and tags the text: enough to prove the token is never stored as typed. */
private class FakeCipher : TokenCipher {
    var broken = false
    override fun encrypt(plain: String) = "sealed:" + plain.reversed()
    override fun decrypt(sealed: String) = if (broken) null else sealed.removePrefix("sealed:").reversed()
}

class ServersTest {
    @Test fun tokensAreStoredSealed() = runTest {
        val kv = MemoryKv(); val s = Servers(kv, FakeCipher())
        val acc = s.signedIn("http://a:8484", "Home", "ana", "tok-123")
        assertFalse(kv.map.values.any { it.contains("tok-123") })
        assertEquals("tok-123", s.token(acc))
        assertEquals(acc.id, s.active()?.id)
    }

    @Test fun severalServersAndSwitching() = runTest {
        val s = Servers(MemoryKv(), FakeCipher())
        val a = s.signedIn("http://a:8484", "A", "ana", "1")
        val b = s.signedIn("https://b.example", "B", "ana", "2")
        assertEquals(2, s.all().size)
        assertEquals(b.id, s.active()?.id)
        s.setActive(a.id)
        assertEquals("1", s.token(s.active()!!))
        // Signing in again to the same address replaces the account, keeps the id.
        val a2 = s.signedIn("http://a:8484", "A", "ana", "3")
        assertEquals(a.id, a2.id)
        assertEquals(2, s.all().size)
    }

    @Test fun signOutKeepsServerRemoveForgetsIt() = runTest {
        val s = Servers(MemoryKv(), FakeCipher())
        val a = s.signedIn("http://a", "A", "ana", "1")
        val b = s.signedIn("http://b", "B", "ivo", "2")
        s.signOut(a.id)
        assertNull(s.token(s.get(a.id)!!))
        s.remove(b.id)
        assertEquals(listOf(a.id), s.all().map { it.id })
        assertEquals(a.id, s.active()?.id)
    }

    @Test fun lostKeystoreKeyMeansSignedOut() = runTest {
        val c = FakeCipher(); val s = Servers(MemoryKv(), c)
        val a = s.signedIn("http://a", "A", "ana", "1")
        c.broken = true
        assertNull(s.token(a))
    }

    @Test fun lastNotificationOnlyGrowsAndResetsForAnotherUser() = runTest {
        val s = Servers(MemoryKv(), FakeCipher())
        val a = s.signedIn("http://a", "A", "ana", "1")
        s.setLastNotification(a.id, 10)
        s.setLastNotification(a.id, 4)
        assertEquals(10L, s.get(a.id)!!.lastNotificationId)
        s.signedIn("http://a", "A", "ana", "2")
        assertEquals(10L, s.get(a.id)!!.lastNotificationId)
        s.signedIn("http://a", "A", "ivo", "3")
        assertEquals(0L, s.get(a.id)!!.lastNotificationId)
        assertTrue(s.all().size == 1)
    }
}
