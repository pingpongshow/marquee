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
import kotlinx.coroutines.launch
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
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
    /** Watched marks shown at once, and a signal for rows and pages to reload (USER-6). */
    val watch = WatchState()
    val favorites = FavoriteState()
    /** Kids' time limits: why video stopped, and the last status for playing downloads offline. */
    val timeLimits: TimeLimits by lazy { TimeLimits(appContext, this) }
    /**
     * Videos playing in order (a video playlist, USER-7): the player moves on to the next of
     * these when one ends. Cleared when a video outside it is played.
     */
    @Volatile var videoQueue: List<Long> = emptyList()
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
            // The sign-in token goes only to this server's own addresses: the same client loads
            // images from elsewhere too (TMDB posters in Discover), which must never receive it.
            val own = t != null && isServerUrl(chain.request().url, listOfNotNull(baseUrl) + server?.candidates.orEmpty())
            val req = if (own) chain.request().newBuilder().header("Authorization", "Bearer $t").build() else chain.request()
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

    private val _liveTv = MutableStateFlow(false)
    /**
     * Whether Live TV is usable: a source with channels this profile may watch (LIVE-1). Every
     * Live TV entry point hides while it's false. Re-checked on (re)connecting and when the app
     * returns to the foreground.
     */
    val liveTv: StateFlow<Boolean> = _liveTv
    /** Test hook (debug builds only): a fixed Live TV status instead of the server's. */
    @Volatile var fakeLiveTv: Boolean? = null
        set(value) { field = value.takeIf { debuggable }; refreshLiveTv() }

    private val _vod = MutableStateFlow(false)
    /**
     * Whether movies on demand are usable (LIVE-6): turned on, a source has films, and this
     * profile may watch Live TV. They live under Live TV, which is listed while either is on.
     * Checked with Live TV's status.
     */
    val vod: StateFlow<Boolean> = _vod
    private val _vodShows = MutableStateFlow(false)
    private val _vodMovies = MutableStateFlow(false)
    /** VOD Movies is listed: VOD is usable and a source has films. */
    val vodMovies: StateFlow<Boolean> = _vodMovies
    /** VOD Shows is listed: VOD is usable and a source has series (Xtream Codes). */
    val vodShows: StateFlow<Boolean> = _vodShows
    /** Test hook (debug builds only): a fixed movies-on-demand status instead of the server's. */
    @Volatile var fakeVod: Boolean? = null
        set(value) { field = value.takeIf { debuggable }; refreshLiveTv() }

    fun refreshLiveTv() {
        val fakeLive = fakeLiveTv
        val fakeV = fakeVod
        fakeLive?.let { _liveTv.value = it }
        fakeV?.let { _vod.value = it; _vodMovies.value = it; _vodShows.value = false }
        if (fakeLive != null && fakeV != null) return
        if (token == null || baseUrl == null) { if (fakeLive == null) _liveTv.value = false; if (fakeV == null) { _vod.value = false; _vodMovies.value = false; _vodShows.value = false }; return }
        if (fakeLive == null) scope.launch {
            val on = runCatching { livetv.liveTvStatus().enabled }.getOrNull() ?: return@launch
            _liveTv.value = fakeLiveTv ?: on
        }
        if (fakeV == null) scope.launch {
            val st = runCatching { livetv.vodStatus() }.getOrNull() ?: return@launch
            if (fakeVod != null) return@launch
            _vod.value = st.enabled
            _vodMovies.value = st.enabled && st.movies > 0
            _vodShows.value = st.enabled && st.series > 0
        }
    }

    private val _ratingPrompt = MutableStateFlow(false)
    /**
     * Whether the admin turned on rating requests when a movie or episode ends (USER-19), from
     * system info. Read on (re)connecting and refreshed when the app returns to the foreground.
     */
    val ratingPrompt: StateFlow<Boolean> = _ratingPrompt

    fun refreshRatingPrompt() {
        if (baseUrl == null) return
        scope.launch {
            val i = runCatching { system.getSystemInfo() }.getOrNull() ?: return@launch
            info = i
            _ratingPrompt.value = i.ratingPrompt == true
        }
    }

    /** Items already asked about in this run of the app, so one watch asks at most once. */
    private val ratingAsked = java.util.Collections.synchronizedSet(mutableSetOf<Long>())

    /**
     * Whether to ask for a rating of this item now that it has been watched (USER-19): the
     * server has it on, the person hasn't turned it off, it's a movie or episode (not an
     * extra or trailer), it isn't rated yet, and it hasn't been asked about already.
     */
    fun wantsRating(d: app.marquee.api.models.ItemDetail): Boolean =
        ratingPrompt.value && me.value?.preferences?.ratingPrompt != false &&
            (d.type == app.marquee.api.models.ItemType.MOVIE || d.type == app.marquee.api.models.ItemType.EPISODE) &&
            d.extraType == null && (d.userRating ?: 0.0) <= 0.0 &&
            sync.queued(d.id, PendingChange.Kind.Rating) == null && d.id !in ratingAsked

    /** Records that the person was asked to rate this item. */
    fun askedRating(id: Long) { ratingAsked += id }

    private val _ratingRequest = MutableStateFlow<RatingRequest?>(null)
    /** A rating request to show over the item page or home after the player closed (USER-19). */
    val ratingRequest: StateFlow<RatingRequest?> = _ratingRequest
    fun requestRating(r: RatingRequest?) { _ratingRequest.value = r }

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

    /** Adds a server by address ("192.168.1.20:32500" or a full URL) and connects to it. */
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
        _ratingPrompt.value = chosen.second.ratingPrompt == true
        _connection.value++
        if (token == null) { _state.value = State.SignedOut; return }
        val me = withContext(Dispatchers.IO) { runCatching { auth.getMe() } }
        me.onSuccess { _me.value = it; store.setUserId(record.id, it.id); _state.value = State.SignedIn; refreshLiveTv() }
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

    private val _profileSwitches = MutableStateFlow(0)
    /** Bumped when this device switched to another profile: the app's screens start afresh for it. */
    val profileSwitches: StateFlow<Int> = _profileSwitches

    /**
     * Switches this device to another household profile (its PIN or password when it has one).
     * [onSwitched] runs once the server agreed, before the new profile takes over.
     */
    suspend fun switchProfile(userId: Long, pin: String?, password: String?, onSwitched: () -> Unit = {}) {
        val result = withContext(Dispatchers.IO) {
            users.switchProfile(userId, app.marquee.api.models.SwitchProfileRequest(device = device, pin = pin, password = password))
        }
        onSwitched()
        finish(result)
        _profileSwitches.value++
    }

    suspend fun finish(result: AuthResult) {
        token = result.token
        server?.let { store.setToken(it.id, result.token); store.setUserId(it.id, result.user.id) }
        _me.value = withContext(Dispatchers.IO) { runCatching { auth.getMe() }.getOrNull() }
        _state.value = State.SignedIn
        refreshLiveTv()
        refreshRatingPrompt()
    }

    suspend fun signOut() {
        withContext(Dispatchers.IO) { runCatching { auth.logout() } }
        signOutLocally()
    }

    private fun signOutLocally() {
        token = null
        server?.let { store.setToken(it.id, null); store.setUserId(it.id, null) }
        _me.value = null
        _liveTv.value = false
        _vod.value = false
        _vodMovies.value = false
        _vodShows.value = false
        _ratingRequest.value = null
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
        info = null
        // Nothing of this server's account carries over to the next one (its image key would
        // otherwise go out in the next server's avatar and artwork URLs).
        _me.value = null
        _liveTv.value = false
        _vod.value = false
        _vodMovies.value = false
        _vodShows.value = false
        _ratingRequest.value = null
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

/** What a rating request shows (USER-19): the item, its title line and artwork. */
data class RatingRequest(val itemId: Long, val title: String, val imageUrl: String?)

/**
 * Whether a request goes to one of the server's own addresses (same scheme, host and port as
 * one of [origins]), the only place the sign-in token may be sent.
 */
fun isServerUrl(url: okhttp3.HttpUrl, origins: List<String>): Boolean = origins.any { o ->
    val h = o.toHttpUrlOrNull()
    h != null && h.scheme == url.scheme && h.host == url.host && h.port == url.port
}
