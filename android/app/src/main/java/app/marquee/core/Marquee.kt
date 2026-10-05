package app.marquee.core

import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import app.marquee.api.apis.AuthApi
import app.marquee.api.apis.HubsApi
import app.marquee.api.apis.ItemsApi
import app.marquee.api.apis.LibrariesApi
import app.marquee.api.apis.MusicApi
import app.marquee.api.apis.PlaybackApi
import app.marquee.api.apis.PlaylistsApi
import app.marquee.api.apis.SearchApi
import app.marquee.api.apis.SystemApi
import app.marquee.api.apis.DownloadsApi
import app.marquee.api.apis.LivetvApi
import app.marquee.api.apis.RequestsApi
import app.marquee.api.apis.SyncplayApi
import app.marquee.api.apis.UsersApi
import app.marquee.api.models.AuthResult
import app.marquee.api.models.DeviceInfo
import app.marquee.api.models.LoginRequest
import app.marquee.api.models.User
import app.marquee.api.models.NetworkClass
import app.marquee.api.models.PinLoginRequest
import app.marquee.api.models.SystemInfo
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.withContext
import okhttp3.OkHttpClient
import java.util.concurrent.TimeUnit

/**
 * The connection to a Marquee server: which server, which address (home network first,
 * then Tailscale), and who is signed in. Mirrors the Apple apps' AppSession.
 */
class Marquee(context: Context) {
    enum class State { NoServer, Connecting, SignedOut, SignedIn }

    val store = ServerStore(context)
    private val appContext: Context = context.applicationContext
    /** Changes made while the server couldn't be reached, sent when it's back (USER-18). */
    val sync: OfflineSync by lazy { OfflineSync(appContext, this) }
    /**
     * Test hook (debug builds only): every request fails as if the network were down, while the
     * app keeps its address, so UI tests can make changes offline and then sync them.
     */
    @Volatile var simulateOffline = false
        set(value) { field = value && debuggable }
    private val debuggable = (context.applicationInfo.flags and android.content.pm.ApplicationInfo.FLAG_DEBUGGABLE) != 0
    /** For fire-and-forget calls that must outlive a screen (progress reports, stopping sessions). */
    val scope = kotlinx.coroutines.CoroutineScope(kotlinx.coroutines.SupervisorJob() + Dispatchers.IO)
    val isTv: Boolean = context.packageManager.hasSystemFeature(PackageManager.FEATURE_LEANBACK)
    /** Video screens open (main thread only); see VideoWindow. */
    var videoScreens = 0
    /** Chromecast (phones and tablets; a TV is itself the screen). */
    val cast: MarqueeCast by lazy { MarqueeCast(context.applicationContext, this) }

    private val _state = MutableStateFlow(State.NoServer)
    val state: StateFlow<State> = _state
    private val _me = MutableStateFlow<User?>(null)
    val me: StateFlow<User?> = _me

    var server: ServerRecord? = store.current; private set
    var baseUrl: String? = null; private set
    var info: SystemInfo? = null; private set
    var lastError: String? = null; private set
    @Volatile var token: String? = server?.let { store.token(it.id) }; private set

    val http: OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(5, TimeUnit.SECONDS)
        .readTimeout(60, TimeUnit.SECONDS)
        .addInterceptor { chain ->
            if (simulateOffline) throw java.io.IOException("Offline (test)")
            val t = token
            val req = if (t != null) chain.request().newBuilder().header("Authorization", "Bearer $t").build() else chain.request()
            chain.proceed(req)
        }
        .build()

    private val base get() = (baseUrl ?: "http://localhost") + "/api/v1"
    val auth get() = AuthApi(base, http)
    val system get() = SystemApi(base, http)
    val hubs get() = HubsApi(base, http)
    val libraries get() = LibrariesApi(base, http)
    val items get() = ItemsApi(base, http)
    val search get() = SearchApi(base, http)
    val playback get() = PlaybackApi(base, http)
    val music get() = MusicApi(base, http)
    val playlists get() = PlaylistsApi(base, http)
    val users get() = UsersApi(base, http)
    val downloads get() = DownloadsApi(base, http)
    val requests get() = RequestsApi(base, http)
    val livetv get() = LivetvApi(base, http)
    val syncplay get() = SyncplayApi(base, http)
    val activity get() = app.marquee.api.apis.ActivityApi(base, http)
    val settings get() = app.marquee.api.apis.SettingsApi(base, http)

    val isRemote: Boolean get() = info?.networkClass == NetworkClass.REMOTE
    /** Signed in but the server can't be reached. */
    val isOffline: Boolean get() = token != null && baseUrl == null

    /** Who is signed in on the current server, even offline (remembered from the last sign-in). */
    val userId: Long? get() = me.value?.id ?: server?.let { store.userId(it.id) }

    /** Screens that follow [connection] reload (after offline changes were sent). */
    fun refresh() { _connection.value++ }

    val device: DeviceInfo
        get() = DeviceInfo(
            clientId = store.clientId,
            name = Build.MODEL ?: "Android",
            platform = if (isTv) DeviceInfo.Platform.ANDROIDTV else DeviceInfo.Platform.ANDROID,
            product = "Marquee for Android",
            version = "0.19",
        )

    init {
        _state.value = if (server == null) State.NoServer else State.Connecting
    }

    private suspend fun probe(url: String): SystemInfo? = withContext(Dispatchers.IO) {
        runCatching { SystemApi("$url/api/v1", http).getSystemInfo() }.getOrNull()
    }

    /**
     * Runs connection work on the session's own scope: these calls change [state], which
     * swaps out the screen that started them and would cancel its coroutines midway.
     */
    private suspend fun <T> detached(block: suspend () -> T): T = scope.async { block() }.await()

    /** Adds a server by address ("10.1.1.10:32500" or a full URL) and connects to it. */
    suspend fun addServer(address: String): Unit = detached {
        var a = address.trim().trimEnd('/')
        if (!a.startsWith("http://") && !a.startsWith("https://")) a = "http://$a"
        if (!Regex(":\\d+$").containsMatchIn(a.substringAfter("://"))) a += ":32500"
        val i = probe(a) ?: throw IllegalStateException("Couldn't reach a Marquee server at that address.")
        val known = store.servers.firstOrNull { it.id == i.serverId }
        val record = ServerRecord(i.serverId, i.serverName,
            lanUrl = if (i.networkClass == NetworkClass.LOCAL) a else known?.lanUrl ?: i.lanUrl,
            remoteUrl = if (i.networkClass == NetworkClass.REMOTE) a else known?.remoteUrl ?: i.remoteUrl)
        use(record)
        reconnectNow()
    }

    private fun use(record: ServerRecord) {
        store.save(record)
        store.currentId = record.id
        server = record
        token = store.token(record.id)
    }

    /** Picks the best address for the current network and checks the session. */
    /** Picks the best address; quiet keeps the current screens up (a background retry). */
    suspend fun reconnect(quiet: Boolean = false): Unit = detached { reconnectNow(quiet) }

    private val _connection = MutableStateFlow(0)
    /** Bumped on every successful (re)connection, so screens can reload. */
    val connection: StateFlow<Int> = _connection

    private suspend fun reconnectNow(quiet: Boolean = false) {
        val record = server ?: run { _state.value = State.NoServer; return }
        if (!quiet) _state.value = State.Connecting
        var chosen: Pair<String, SystemInfo>? = null
        for (url in record.candidates) {
            val i = probe(url)
            if (i != null) { chosen = url to i; break }
        }
        if (chosen == null) {
            lastError = "Can't reach ${record.name}. Check that you're on the home network or connected to Tailscale."
            baseUrl = null // offline: downloads still play
            _state.value = if (token == null) State.SignedOut else State.SignedIn
            return
        }
        lastError = null
        baseUrl = chosen.first
        info = chosen.second
        _connection.value++
        if (token == null) { _state.value = State.SignedOut; return }
        val me = withContext(Dispatchers.IO) { runCatching { auth.getMe() } }
        me.onSuccess { _me.value = it; store.setUserId(record.id, it.id); _state.value = State.SignedIn }
            .onFailure { e ->
                if ((e as? app.marquee.api.infrastructure.ClientException)?.statusCode == 401) signOutLocally() else _state.value = State.SignedIn
            }
    }

    /** After the signed-in user changed their own account (preferences). */
    fun updated(user: User) { _me.value = user }

    suspend fun signIn(username: String, password: String, totpCode: String? = null) = finish(withContext(Dispatchers.IO) {
        auth.login(LoginRequest(username, password, device, totpCode = totpCode))
    })

    suspend fun signInProfile(userId: Long, pin: String?, password: String?, totpCode: String? = null) = finish(withContext(Dispatchers.IO) {
        auth.pinLogin(PinLoginRequest(userId, device, pin = pin, password = password, totpCode = totpCode))
    })

    suspend fun finish(result: AuthResult) {
        token = result.token
        server?.let { store.setToken(it.id, result.token); store.setUserId(it.id, result.user.id) }
        _me.value = withContext(Dispatchers.IO) { runCatching { auth.getMe() }.getOrNull() }
        _state.value = State.SignedIn
    }

    suspend fun signOut() {
        withContext(Dispatchers.IO) { runCatching { auth.logout() } }
        signOutLocally()
    }

    private fun signOutLocally() {
        token = null
        server?.let { store.setToken(it.id, null); store.setUserId(it.id, null) }
        _me.value = null
        _state.value = State.SignedOut
    }

    /** Forgets every server and sign-in (UI tests start from a clean slate). */
    fun reset() {
        store.servers.forEach { store.forget(it.id) }
        forgetServer()
        _me.value = null
    }

    fun forgetServer() {
        server?.let { store.forget(it.id) }
        server = null
        token = null
        baseUrl = null
        _state.value = State.NoServer
    }

    // URLs

    /**
     * The query parameter that authenticates image, person photo and avatar URLs: the
     * image key from /me, which grants images and nothing else (D85), so the sign-in token
     * never ends up in artwork URIs handed to other apps. Falls back to the token until
     * /me has loaded.
     */
    private val imageAuth: String
        get() = me.value?.imageKey?.let { "key=$it" } ?: "token=${token ?: ""}"

    /** Artwork URL (images authenticate with the image key). */
    fun imageUrl(artworkId: Long?, width: Int): String? {
        if (artworkId == null || baseUrl == null) return null
        return "$baseUrl/api/v1/images/$artworkId?w=${width * 2}&$imageAuth"
    }

    /** Resolves a server-relative URL (streams, avatars, person photos). */
    fun absolute(path: String?): String? {
        if (path == null || baseUrl == null) return null
        if (path.startsWith("http")) return path
        val needsKey = path.startsWith("/api/v1/users/") || path.startsWith("/api/v1/people/") || path.startsWith("/api/v1/images/")
        val keyParam = if (needsKey) (if (path.contains("?")) "&" else "?") + imageAuth else ""
        return baseUrl + path + keyParam
    }
}
