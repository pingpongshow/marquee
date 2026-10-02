package app.marquee

import android.app.Application
import app.marquee.core.Marquee
import coil3.ImageLoader
import coil3.SingletonImageLoader
import coil3.network.okhttp.OkHttpNetworkFetcherFactory
import kotlinx.coroutines.launch

class MarqueeApplication : Application(), SingletonImageLoader.Factory {
    lateinit var marquee: Marquee
        private set
    val music by lazy { app.marquee.music.MusicController(this, marquee) }
    val downloads by lazy { app.marquee.core.Downloads(this, marquee) }

    override fun onCreate() {
        super.onCreate()
        marquee = Marquee(this)
        // Back online after being offline: reconnect and send plays made meanwhile.
        getSystemService(android.net.ConnectivityManager::class.java).registerDefaultNetworkCallback(object : android.net.ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: android.net.Network) {
                if (!marquee.isOffline || marquee.state.value != Marquee.State.SignedIn) return
                marquee.scope.launch {
                    marquee.reconnect(quiet = true)
                    downloads.flushProgress()
                }
            }
        })
    }

    override fun newImageLoader(context: coil3.PlatformContext): ImageLoader =
        ImageLoader.Builder(context).components { add(OkHttpNetworkFetcherFactory(callFactory = { marquee.http })) }.build()
}
