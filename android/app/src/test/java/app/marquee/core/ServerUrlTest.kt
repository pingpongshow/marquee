package app.marquee.core

import okhttp3.HttpUrl.Companion.toHttpUrl
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** The sign-in token is only sent to the server's own addresses. */
class ServerUrlTest {
    private val origins = listOf("http://192.168.1.20:32500", "https://media.example.ts.net:32500")

    @Test fun ownAddresses() {
        assertTrue(isServerUrl("http://192.168.1.20:32500/api/v1/me".toHttpUrl(), origins))
        assertTrue(isServerUrl("https://media.example.ts.net:32500/api/v1/images/4?w=200".toHttpUrl(), origins))
    }

    @Test fun elsewhere() {
        assertFalse(isServerUrl("https://image.tmdb.org/t/p/w342/abc.jpg".toHttpUrl(), origins))
        // Same host, another port or scheme.
        assertFalse(isServerUrl("http://192.168.1.20:8080/".toHttpUrl(), origins))
        assertFalse(isServerUrl("https://192.168.1.20:32500/".toHttpUrl(), origins))
        // No server yet.
        assertFalse(isServerUrl("http://192.168.1.20:32500/".toHttpUrl(), emptyList()))
    }
}
