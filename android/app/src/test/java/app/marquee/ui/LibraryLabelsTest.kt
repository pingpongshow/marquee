package app.marquee.ui

import app.marquee.api.infrastructure.ClientError
import app.marquee.api.infrastructure.ClientException
import app.marquee.api.infrastructure.ServerError
import app.marquee.api.infrastructure.ServerException
import app.marquee.api.models.LibraryType
import org.junit.Assert.assertEquals
import org.junit.Test

/** Libraries' counts, the library sorts Home's See all asks for, and error messages people read. */
class LibraryLabelsTest {
    @Test fun countsNameWhatALibraryHolds() {
        assertEquals("1 show", libraryCount(LibraryType.ANIME, 1))
        assertEquals("303 movies", libraryCount(LibraryType.MOVIES, 303))
        assertEquals("10 artists", libraryCount(LibraryType.MUSIC, 10))
        assertEquals("0 videos", libraryCount(LibraryType.VIDEOS, 0))
    }

    @Test fun seeAllSortsAreKnown() {
        assertEquals(LibrarySort.Added, LibrarySort.of("added"))
        assertEquals(LibrarySort.Viewed, LibrarySort.of("viewed"))
        // Anything else (or nothing) is the title order.
        assertEquals(LibrarySort.Title, LibrarySort.of(null))
        assertEquals(LibrarySort.Title, LibrarySort.of("nonsense"))
    }

    @Test fun errorsReadAsTheServersMessage() {
        val busy = ServerException("Server error : 503 Service Unavailable {...}", 503,
            ServerError<Any>(body = """{"code":"unavailable","message":"Muse needs the analysis service, which isn't running"}""", statusCode = 503))
        assertEquals("Muse needs the analysis service, which isn't running", serverMessage(busy))
        val conflict = ClientException("Client error : 409 Conflict", 409,
            ClientError<Any>(body = """{"code":"not_analyzed","message":"no analysed tracks from the 2020"}""", statusCode = 409))
        assertEquals("No analysed tracks from the 2020", serverMessage(conflict))
        // No readable body: the fallback, never the bare status line.
        assertEquals("Couldn't start the radio", serverMessage(ClientException("Client error : 409 Conflict", 409, ClientError<Any>(statusCode = 409)), "Couldn't start the radio"))
        // Not a server answer (no connection): its own message.
        assertEquals("timeout", serverMessage(java.io.IOException("timeout")))
    }
}
