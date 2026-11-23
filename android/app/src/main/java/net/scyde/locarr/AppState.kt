package net.scyde.locarr

import android.app.Application
import android.os.Build
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import coil.ImageLoader
import coil.disk.DiskCache
import coil.memory.MemoryCache
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import net.scyde.locarr.api.Api
import net.scyde.locarr.api.ApiException
import net.scyde.locarr.api.AuthResult
import net.scyde.locarr.api.DeviceInfo
import net.scyde.locarr.api.User
import net.scyde.locarr.data.DataStoreKeyValue
import net.scyde.locarr.data.KeystoreTokenCipher
import net.scyde.locarr.data.ServerAccount
import net.scyde.locarr.data.Servers
import net.scyde.locarr.notify.DeepLink
import net.scyde.locarr.notify.NotificationWorker

class LocarrApp : Application() {
    lateinit var state: AppState
    override fun onCreate() {
        super.onCreate()
        state = AppState(this)
        NotificationWorker.createChannels(this)
        NotificationWorker.schedule(this)
    }
}

/** How opening the active server went. */
enum class Connection { Loading, Ok, SignedOut, Unreachable, NoServer }

/** What every screen shares: the servers, the open one, who is signed in. */
class AppState(val app: Application) {
    val servers = Servers(DataStoreKeyValue(app), KeystoreTokenCipher())
    var accounts by mutableStateOf<List<ServerAccount>>(emptyList())
    var account by mutableStateOf<ServerAccount?>(null)
    var api by mutableStateOf<Api?>(null)
    var me by mutableStateOf<User?>(null)
    var connection by mutableStateOf(Connection.Loading)
    var images by mutableStateOf<ImageLoader?>(null)
    var unread by mutableIntStateOf(0)
    /** A link from a notification waiting for the screens to be ready. */
    var pendingLink by mutableStateOf<DeepLink?>(null)
    /** The server rejected the session after the app had opened it. */
    var sessionExpired by mutableStateOf(false)

    val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

    val device get() = DeviceInfo(name = "${Build.MANUFACTURER} ${Build.MODEL}".trim(), client = "Locarr Android ${BuildConfig.VERSION_NAME}")

    private fun attach(a: Api?) {
        api = a
        a?.onUnauthorized = { scope.launch { if (api === a) expire() } }
        images = a?.let {
            ImageLoader.Builder(app).okHttpClient(it.http).crossfade(true)
                .memoryCache { MemoryCache.Builder(app).maxSizePercent(0.2).build() }
                .diskCache { DiskCache.Builder().directory(app.cacheDir.resolve("images")).maxSizeBytes(200L * 1024 * 1024).build() }
                .build()
        }
    }

    /** Opens the active server (at start and after switching). */
    suspend fun start() {
        accounts = servers.all()
        val acc = servers.active()
        account = acc
        if (acc == null) { attach(null); me = null; connection = Connection.NoServer; return }
        val token = servers.token(acc)
        attach(Api(acc.url, token))
        if (token == null) { me = null; connection = Connection.SignedOut; return }
        connection = Connection.Loading
        connection = try {
            me = api!!.me()
            refreshUnread()
            Connection.Ok
        } catch (e: ApiException) {
            if (e.status == 401) { servers.signOut(acc.id); accounts = servers.all(); me = null; Connection.SignedOut } else Connection.Unreachable
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            Connection.Unreachable
        }
        NotificationWorker.runNow(app)
    }

    suspend fun refreshUnread() {
        unread = runCatching { api?.notifications(limit = 1)?.unread }.getOrNull() ?: unread
    }

    /** Stores a successful sign-in on [url] and opens it. */
    suspend fun signedIn(url: String, result: AuthResult) {
        val token = result.token ?: throw ApiException(500, "no_token", "The server returned no session token")
        val name = runCatching { Api(url).info().instanceName }.getOrNull().orEmpty()
        servers.signedIn(url, name, result.user.username, token)
        sessionExpired = false
        start()
    }

    suspend fun switchTo(id: String) {
        servers.setActive(id)
        start()
    }

    /** Opens the sign-in for a new server without forgetting the others. */
    suspend fun addServer() {
        servers.setActive(null)
        account = null; attach(null); me = null
        connection = Connection.NoServer
    }

    suspend fun signOut() {
        val acc = account ?: return
        runCatching { api?.logout() }
        servers.signOut(acc.id)
        start()
    }

    suspend fun removeServer(id: String) {
        if (id == account?.id) runCatching { api?.logout() }
        servers.remove(id)
        start()
    }

    private suspend fun expire() {
        val acc = account ?: return
        if (me == null) return
        servers.signOut(acc.id)
        accounts = servers.all()
        me = null
        sessionExpired = true
        connection = Connection.SignedOut
    }
}
