package app.marquee.core

import android.content.Context
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

/** A server this device knows: its home and Tailscale addresses (same as the Apple apps). */
@Serializable
data class ServerRecord(val id: String, val name: String, val lanUrl: String?, val remoteUrl: String?) {
    /** Addresses to try, home network first. */
    val candidates: List<String> get() = listOfNotNull(lanUrl, remoteUrl).distinct()
}

/** Remembers servers, the current one and each server's sign-in token (app-private storage). */
class ServerStore(context: Context) {
    private val prefs = context.getSharedPreferences("marquee.servers", Context.MODE_PRIVATE)
    private val json = Json { ignoreUnknownKeys = true }

    var servers: List<ServerRecord>
        get() = prefs.getString("servers", null)?.let { runCatching { json.decodeFromString<List<ServerRecord>>(it) }.getOrNull() } ?: emptyList()
        private set(value) = prefs.edit().putString("servers", json.encodeToString(value)).apply()

    var currentId: String?
        get() = prefs.getString("current", null)
        set(value) = prefs.edit().putString("current", value).apply()

    val current: ServerRecord? get() = servers.firstOrNull { it.id == currentId }

    fun save(record: ServerRecord) {
        servers = servers.filterNot { it.id == record.id } + record
    }

    fun forget(id: String) {
        servers = servers.filterNot { it.id == id }
        prefs.edit().remove("token.$id").apply()
        if (currentId == id) currentId = null
    }

    fun token(id: String): String? = prefs.getString("token.$id", null)

    fun setToken(id: String, token: String?) {
        prefs.edit().apply { if (token == null) remove("token.$id") else putString("token.$id", token) }.apply()
    }

    /** A stable id for this install, sent as the device's client id. */
    val clientId: String
        get() = prefs.getString("clientId", null) ?: java.util.UUID.randomUUID().toString().also { prefs.edit().putString("clientId", it).apply() }
}
