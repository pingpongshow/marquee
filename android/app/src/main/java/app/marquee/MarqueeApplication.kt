package app.marquee

import android.app.Application
import app.marquee.core.Marquee
import coil3.ImageLoader
import coil3.SingletonImageLoader
import coil3.network.okhttp.OkHttpNetworkFetcherFactory

class MarqueeApplication : Application(), SingletonImageLoader.Factory {
    lateinit var marquee: Marquee
        private set
    val music by lazy { app.marquee.music.MusicController(this, marquee) }

    override fun onCreate() {
        super.onCreate()
        marquee = Marquee(this)
    }

    override fun newImageLoader(context: coil3.PlatformContext): ImageLoader =
        ImageLoader.Builder(context).components { add(OkHttpNetworkFetcherFactory(callFactory = { marquee.http })) }.build()
}
