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
        app.marquee.music.TrackGains.init(this)
        registerActivityLifecycleCallbacks(object : ActivityLifecycleCallbacks {
            override fun onActivityStarted(activity: android.app.Activity) {
                // Back in the foreground: send what was changed offline (USER-18).
                if (marquee.state.value == Marquee.State.SignedIn) marquee.scope.launch { marquee.sync.syncNow() }
                // Live TV entry points follow whether it's usable now (LIVE-1).
                if (marquee.state.value == Marquee.State.SignedIn) marquee.refreshLiveTv()
                // Rating requests follow the admin's switch (USER-19).
                if (marquee.state.value == Marquee.State.SignedIn) marquee.refreshRatingPrompt()
            }
            override fun onActivityStopped(activity: android.app.Activity) {}
            override fun onActivityCreated(activity: android.app.Activity, savedInstanceState: android.os.Bundle?) {}
            override fun onActivityResumed(activity: android.app.Activity) {}
            override fun onActivityPaused(activity: android.app.Activity) {}
            override fun onActivitySaveInstanceState(activity: android.app.Activity, outState: android.os.Bundle) {}
            override fun onActivityDestroyed(activity: android.app.Activity) {}
        })
        // Back online after being offline: reconnect and send what was changed meanwhile (USER-18).
        getSystemService(android.net.ConnectivityManager::class.java).registerDefaultNetworkCallback(object : android.net.ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: android.net.Network) {
                if (marquee.state.value != Marquee.State.SignedIn) return
                marquee.scope.launch {
                    if (marquee.isOffline) marquee.reconnect(quiet = true)
                    marquee.sync.syncNow()
                    marquee.refreshLiveTv()
                }
            }
        })
    }

    override fun newImageLoader(context: coil3.PlatformContext): ImageLoader =
        ImageLoader.Builder(context).components {
            add(OkHttpNetworkFetcherFactory(callFactory = { marquee.http }))
            add(coil3.svg.SvgDecoder.Factory()) // channel logos are often SVG
        }.build()
}
