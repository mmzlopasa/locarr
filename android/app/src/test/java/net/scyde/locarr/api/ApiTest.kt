package net.scyde.locarr.api

import kotlinx.coroutines.test.runTest
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Before
import org.junit.Test

class ApiTest {
    private lateinit var server: MockWebServer

    @Before fun start() { server = MockWebServer().apply { start() } }
    @After fun stop() { server.shutdown() }

    private fun api(token: String? = null) = Api(server.url("/").toString(), token)

    @Test fun signInSendsCredentialsWithoutCookieAndReturnsToken() = runTest {
        server.enqueue(MockResponse().setBody("""{"user":{"id":1,"username":"ana","role":"admin","permissions":{}},"token":"s3cret"}"""))
        val res = api().login("ana", "pw", null, null, DeviceInfo("Pixel", "Locarr Android"))
        assertEquals("s3cret", res.token)
        assertTrue(res.user.isAdmin)
        val req = server.takeRequest()
        assertEquals("/api/v1/auth/login", req.path)
        val body = req.body.readUtf8()
        assertTrue(body.contains("\"username\":\"ana\""))
        assertFalse(body.contains("useCookie"))
        assertFalse(body.contains("totpCode"))
        assertNull(req.getHeader("Authorization"))
    }

    @Test fun twoFactorStepIsAnErrorCode() = runTest {
        server.enqueue(MockResponse().setResponseCode(401).setBody("""{"code":"totp_required","message":"code needed"}"""))
        var expired = false
        val a = api().also { it.onUnauthorized = { expired = true } }
        try { a.login("ana", "pw", null, null, DeviceInfo()); fail() } catch (e: ApiException) {
            assertEquals("totp_required", e.code)
            assertEquals(401, e.status)
        }
        assertFalse(expired)
    }

    @Test fun providerSignInWithCode() = runTest {
        server.enqueue(MockResponse().setBody("""{"user":{"id":2,"username":"ivo"},"token":"t"}"""))
        api().providerLogin("streamero", "ivo", "pw", "123456", DeviceInfo())
        val req = server.takeRequest()
        assertEquals("/api/v1/auth/providers/streamero/login", req.path)
        assertTrue(req.body.readUtf8().contains("\"totpCode\":\"123456\""))
    }

    @Test fun bearerTokenAndExpiredSession() = runTest {
        server.enqueue(MockResponse().setBody("""{"id":1,"username":"ana","role":"requester","permissions":{"requestMovies":true}}"""))
        server.enqueue(MockResponse().setResponseCode(401).setBody("""{"code":"unauthorized","message":"not signed in"}"""))
        var expired = 0
        val a = api("tok").also { it.onUnauthorized = { expired++ } }
        assertTrue(a.me().permissions.requestMovies)
        assertEquals("Bearer tok", server.takeRequest().getHeader("Authorization"))
        try { a.me(); fail() } catch (e: ApiException) { assertTrue(isSessionRejected(e)) }
        assertEquals(1, expired)
    }

    @Test fun pollUsesSince() = runTest {
        server.enqueue(MockResponse().setBody("""{"items":[],"unread":0,"latestId":40}"""))
        api("t").notifications(since = 40, limit = 50)
        assertEquals("/api/v1/notifications?since=40&limit=50", server.takeRequest().path)
    }

    @Test fun nonJsonErrorsStillBecomeApiExceptions() = runTest {
        server.enqueue(MockResponse().setResponseCode(502).setBody("<html>bad gateway</html>"))
        try { api("t").queue(); fail() } catch (e: ApiException) { assertEquals(502, e.status) }
    }

    @Test fun imageUrls() {
        val a = Api("http://10.0.0.2:8484")
        assertEquals("http://10.0.0.2:8484/api/v1/images/tmdb/w342/abc.jpg", a.imageUrl("/abc.jpg"))
        assertEquals("http://10.0.0.2:8484/api/v1/images/coverart/500/rg-1.jpg", a.imageUrl("rg-1.jpg", music = true))
        assertNull(a.imageUrl(""))
        assertEquals("http://10.0.0.2:8484/settings/media", a.webUrl("/settings/media"))
    }

    @Test fun serverAddresses() {
        assertEquals("http://192.168.1.10:8484", normalizeServerUrl(" 192.168.1.10:8484/ "))
        assertEquals("https://locarr.example.com", normalizeServerUrl("https://locarr.example.com/api/v1"))
        assertEquals("http://nas.local:8484/locarr", normalizeServerUrl("nas.local:8484/locarr/"))
        assertNull(normalizeServerUrl("   "))
        assertNull(normalizeServerUrl("http://"))
    }
}
