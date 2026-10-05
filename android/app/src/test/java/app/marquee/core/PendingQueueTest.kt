package app.marquee.core

import app.marquee.api.infrastructure.ClientException
import app.marquee.api.infrastructure.ServerException
import app.marquee.core.PendingChange.Kind
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.io.IOException

/** The offline changes queue (USER-18). */
class PendingQueueTest {
    private class FileStorage(val f: File) : PendingQueue.Storage {
        override fun read(): String? = if (f.exists()) f.readText() else null
        override fun write(text: String) = f.writeText(text)
    }

    private fun storage() = FileStorage(File.createTempFile("pending", ".json").apply { delete(); deleteOnExit() })
    private fun c(item: Long, kind: Kind, at: Long, user: Long = 1, server: String = "s1") = PendingChange(server, user, item, kind, at)

    @Test fun coalescesByItemAndKind() {
        val q = PendingQueue(storage())
        q.add(c(10, Kind.Rating, 100).copy(rating = 6.0))
        q.add(c(10, Kind.Watched, 110))
        q.add(c(10, Kind.Rating, 120).copy(rating = 8.0))
        q.add(c(11, Kind.Rating, 130).copy(rating = 2.0))
        val p = q.pending("s1", 1)
        assertEquals(3, p.size)
        assertEquals(8.0, p.single { it.item == 10L && it.kind == Kind.Rating }.rating)
        // An older change than the one queued doesn't replace it.
        q.add(c(10, Kind.Rating, 50).copy(rating = 1.0))
        assertEquals(8.0, q.latest("s1", 1, 10, Kind.Rating)!!.rating)
        // Watched then unwatched: only the last.
        q.add(c(10, Kind.Watched, 140).copy(on = false))
        assertEquals(false, q.latest("s1", 1, 10, Kind.Watched)!!.on)
        assertEquals(3, q.pending("s1", 1).size)
    }

    @Test fun progressKeepsTheFurthest() {
        val q = PendingQueue(storage())
        q.add(c(5, Kind.Progress, 100).copy(positionMs = 1000))
        q.add(c(5, Kind.Progress, 200).copy(positionMs = 5000))
        assertEquals(5000, q.latest("s1", 1, 5, Kind.Progress)!!.positionMs)
        q.add(c(5, Kind.Progress, 300).copy(positionMs = 9000, watched = true))
        // A later partial play doesn't undo the finished one.
        q.add(c(5, Kind.Progress, 400).copy(positionMs = 2000))
        val p = q.latest("s1", 1, 5, Kind.Progress)!!
        assertTrue(p.watched)
        assertEquals(400, p.atMs)
        assertEquals(1, q.pending("s1", 1).size)
    }

    @Test fun scopedToServerAndPerson() {
        val q = PendingQueue(storage())
        q.add(c(1, Kind.Rating, 100, user = 1).copy(rating = 4.0))
        q.add(c(1, Kind.Rating, 110, user = 2).copy(rating = 10.0))
        q.add(c(1, Kind.Rating, 120, server = "s2").copy(rating = 2.0))
        assertEquals(4.0, q.pending("s1", 1).single().rating)
        assertEquals(10.0, q.pending("s1", 2).single().rating)
        assertEquals(1, q.count("s2", 1))
        assertEquals(0, q.count(null, 1))
        val sent = mutableListOf<PendingChange>()
        runBlocking { q.flush("s1", 1) { sent += it } }
        assertEquals(listOf(4.0), sent.map { it.rating })
        assertEquals(2, q.changes.value.size)
    }

    @Test fun sendsInOrderMade() {
        val q = PendingQueue(storage())
        q.add(c(1, Kind.Watched, 100))
        q.add(c(2, Kind.Watchlist, 110))
        q.add(c(3, Kind.Rating, 120))
        q.add(c(1, Kind.Watched, 130).copy(on = false)) // replaces the first, so it now goes last
        val sent = mutableListOf<Long>()
        runBlocking { q.flush("s1", 1) { sent += it.item } }
        assertEquals(listOf(2L, 3L, 1L), sent)
        assertTrue(q.changes.value.isEmpty())
    }

    @Test fun keepsOrDropsByResponse() {
        assertEquals(PendingQueue.Outcome.Sent, PendingQueue.outcome(null))
        assertEquals(PendingQueue.Outcome.Drop, PendingQueue.outcome(ClientException("gone", 404)))
        assertEquals(PendingQueue.Outcome.KeepSignedOut, PendingQueue.outcome(ClientException("auth", 401)))
        assertEquals(PendingQueue.Outcome.KeepOffline, PendingQueue.outcome(IOException("down")))
        assertEquals(PendingQueue.Outcome.KeepOffline, PendingQueue.outcome(ServerException("busy", 503)))

        val q = PendingQueue(storage())
        q.add(c(1, Kind.Rating, 100)) // 204
        q.add(c(2, Kind.Rating, 110)) // 404: dropped
        q.add(c(3, Kind.Rating, 120)) // network: stops here
        q.add(c(4, Kind.Rating, 130))
        val r = runBlocking {
            q.flush("s1", 1) {
                when (it.item) { 2L -> throw ClientException("gone", 404); 3L -> throw IOException("down") }
            }
        }
        assertEquals(1, r.sent); assertEquals(1, r.dropped); assertEquals(PendingQueue.Outcome.KeepOffline, r.stopped)
        assertEquals(listOf(3L, 4L), q.pending("s1", 1).map { it.item })

        val r2 = runBlocking { q.flush("s1", 1) { throw ClientException("auth", 401) } }
        assertEquals(PendingQueue.Outcome.KeepSignedOut, r2.stopped)
        assertEquals(2, q.pending("s1", 1).size)
    }

    @Test fun aDirectSaveSupersedesTheQueued() {
        val q = PendingQueue(storage())
        q.add(c(1, Kind.Rating, 100).copy(rating = 2.0))
        q.superseded(c(1, Kind.Rating, 200).copy(rating = 8.0))
        assertNull(q.latest("s1", 1, 1, Kind.Rating))
    }

    @Test fun survivesARestart() {
        val st = storage()
        val q = PendingQueue(st)
        q.add(c(1, Kind.Comment, 100).copy(comment = "Nice"))
        q.add(c(2, Kind.Watchlist, 110).copy(on = false))
        q.add(c(3, Kind.Progress, 120).copy(positionMs = 42_000))
        val again = PendingQueue(st)
        assertEquals(q.pending("s1", 1), again.pending("s1", 1))
        assertEquals("Nice", again.latest("s1", 1, 1, Kind.Comment)!!.comment)
        // New changes after the restart still go after the old ones.
        again.add(c(4, Kind.Rating, 130))
        assertEquals(listOf(1L, 2L, 3L, 4L), again.pending("s1", 1).map { it.item })
    }
}
