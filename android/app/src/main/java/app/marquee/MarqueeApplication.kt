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
            private var started = 0
            override fun onActivityStarted(activity: android.app.Activity) {
                // While offline it keeps trying to get back, in the foreground only.
                if (started++ == 0) marquee.setForeground(true)
                // Back in the foreground: send what was changed offline (USER-18).
                if (marquee.state.value == Marquee.State.SignedIn) marquee.scope.launch { marquee.sync.syncNow() }
                // Live TV entry points follow whether it's usable now (LIVE-1).
                if (marquee.state.value == Marquee.State.SignedIn) marquee.refreshLiveTv()
                // Rating requests follow the admin's switch (USER-19).
                if (marquee.state.value == Marquee.State.SignedIn) marquee.refreshRatingPrompt()
            }
            override fun onActivityStopped(activity: android.app.Activity) {
                if (--started == 0) marquee.setForeground(false)
            }
            override fun onActivityCreated(activity: android.app.Activity, savedInstanceState: android.os.Bundle?) {}
            override fun onActivityResumed(activity: android.app.Activity) {}
            override fun onActivityPaused(activity: android.app.Activity) {}
            override fun onActivitySaveInstanceState(activity: android.app.Activity, outState: android.os.Bundle) {}
            override fun onActivityDestroyed(activity: android.app.Activity) {}
        })
        // Back online after being offline: reconnect and send what was changed meanwhile (USER-18).
        getSystemService(android.net.ConnectivityManager::class.java).registerDefaultNetworkCallback(object : android.net.ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: android.net.Network) = marquee.networkAvailable()
            // Gone with nothing to replace it: offline (phones and tablets).
            override fun onLost(network: android.net.Network) = marquee.networkLost()
        })
    }

    override fun newImageLoader(context: coil3.PlatformContext): ImageLoader =
        ImageLoader.Builder(context).components {
            add(OkHttpNetworkFetcherFactory(callFactory = { marquee.http }))
            add(coil3.svg.SvgDecoder.Factory()) // channel logos are often SVG
        }.build()
}
