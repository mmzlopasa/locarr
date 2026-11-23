package net.scyde.locarr.data

import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import net.scyde.locarr.api.json

/** One server the app knows, with the signed-in account (token sealed by [TokenCipher]). */
@Serializable
data class ServerAccount(
    val id: String,
    val url: String,
    val name: String = "",
    val username: String = "",
    val sealedToken: String? = null,
    /** Largest notification id already seen (shown or skipped) for this account. */
    val lastNotificationId: Long = 0,
)

/** Raw key-value storage, so the server list logic can be tested without Android. */
interface KeyValue {
    suspend fun read(key: String): String?
    suspend fun write(key: String, value: String?)
}

/** The servers on this device and which one is open. Tokens are encrypted at rest. */
class Servers(private val kv: KeyValue, private val cipher: TokenCipher) {
    private val listSer = ListSerializer(ServerAccount.serializer())

    suspend fun all(): List<ServerAccount> =
        kv.read(KEY_SERVERS)?.let { runCatching { json.decodeFromString(listSer, it) }.getOrNull() } ?: emptyList()

    private suspend fun save(list: List<ServerAccount>) = kv.write(KEY_SERVERS, json.encodeToString(listSer, list))

    suspend fun activeId(): String? = kv.read(KEY_ACTIVE)
    suspend fun active(): ServerAccount? { val id = activeId(); return all().firstOrNull { it.id == id } ?: all().firstOrNull() }
    suspend fun setActive(id: String?) = kv.write(KEY_ACTIVE, id)
    suspend fun get(id: String) = all().firstOrNull { it.id == id }

    fun token(a: ServerAccount): String? = a.sealedToken?.let { cipher.decrypt(it) }

    /** Adds the server (or updates the one with the same address) with a fresh sign-in. */
    suspend fun signedIn(url: String, name: String, username: String, token: String): ServerAccount {
        val list = all().toMutableList()
        val i = list.indexOfFirst { it.url == url }
        val prev = list.getOrNull(i)
        val acc = ServerAccount(
            id = prev?.id ?: newId(url), url = url, name = name, username = username, sealedToken = cipher.encrypt(token),
            lastNotificationId = if (prev?.username == username) prev.lastNotificationId else 0,
        )
        if (i >= 0) list[i] = acc else list += acc
        save(list)
        setActive(acc.id)
        return acc
    }

    /** Forgets the token (signed out, or the server refused it) but keeps the server. */
    suspend fun signOut(id: String) = update(id) { it.copy(sealedToken = null) }

    suspend fun remove(id: String) {
        save(all().filterNot { it.id == id })
        if (activeId() == id) setActive(all().firstOrNull()?.id)
    }

    suspend fun setLastNotification(id: String, latest: Long) = update(id) { it.copy(lastNotificationId = maxOf(it.lastNotificationId, latest)) }

    private suspend fun update(id: String, f: (ServerAccount) -> ServerAccount) = save(all().map { if (it.id == id) f(it) else it })

    private fun newId(url: String) = Integer.toHexString(url.hashCode()) + (System.currentTimeMillis() % 100000).toString(36)

    companion object {
        const val KEY_SERVERS = "servers"
        const val KEY_ACTIVE = "active"
    }
}
