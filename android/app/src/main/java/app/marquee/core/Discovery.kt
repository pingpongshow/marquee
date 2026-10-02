package app.marquee.core

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.net.wifi.WifiManager
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow

/** A server found on the home network with Bonjour (_marquee._tcp, D49). */
data class FoundServer(val name: String, val url: String)

/** Finds Marquee servers on the LAN. Emits the current list as servers appear and go. */
fun discoverServers(context: Context): Flow<List<FoundServer>> = callbackFlow {
    val nsd = context.getSystemService(Context.NSD_SERVICE) as NsdManager
    val wifi = context.applicationContext.getSystemService(Context.WIFI_SERVICE) as WifiManager
    val lock = wifi.createMulticastLock("marquee-discovery").apply { setReferenceCounted(false); acquire() }
    val found = linkedMapOf<String, FoundServer>()

    fun resolve(info: NsdServiceInfo) {
        @Suppress("DEPRECATION")
        nsd.resolveService(info, object : NsdManager.ResolveListener {
            override fun onResolveFailed(s: NsdServiceInfo, code: Int) {}
            override fun onServiceResolved(s: NsdServiceInfo) {
                @Suppress("DEPRECATION")
                val host = s.host?.hostAddress ?: return
                if (host.contains(':')) return // prefer IPv4 addresses
                val name = s.attributes["name"]?.let { String(it) } ?: s.serviceName
                found[s.serviceName] = FoundServer(name, "http://$host:${s.port}")
                trySend(found.values.toList())
            }
        })
    }

    val listener = object : NsdManager.DiscoveryListener {
        override fun onDiscoveryStarted(type: String) {}
        override fun onDiscoveryStopped(type: String) {}
        override fun onStartDiscoveryFailed(type: String, code: Int) {}
        override fun onStopDiscoveryFailed(type: String, code: Int) {}
        override fun onServiceFound(info: NsdServiceInfo) = resolve(info)
        override fun onServiceLost(info: NsdServiceInfo) {
            found.remove(info.serviceName)
            trySend(found.values.toList())
        }
    }
    nsd.discoverServices("_marquee._tcp", NsdManager.PROTOCOL_DNS_SD, listener)
    awaitClose {
        runCatching { nsd.stopServiceDiscovery(listener) }
        lock.release()
    }
}
