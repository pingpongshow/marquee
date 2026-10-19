package app.marquee.core

import android.content.Context
import app.marquee.api.infrastructure.ClientError
import app.marquee.api.infrastructure.ClientException
import app.marquee.api.infrastructure.Serializer
import app.marquee.api.models.TimeLimitStatus
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.withContext
import java.time.LocalTime

/**
 * Kids' time limits: a daily allowance and viewing hours set by an admin. The server refuses to
 * start video past them (403 time_limit) and ends a session when the time runs out; the player
 * then closes and [blocked] shows why, full screen. Downloads playing on the device check the
 * server when it can be reached, and the last status it gave when it can't.
 */
class TimeLimits(context: Context, private val marquee: Marquee) {
    private val prefs = context.getSharedPreferences("time-limits", Context.MODE_PRIVATE)
    private val _blocked = MutableStateFlow<String?>(null)
    /** Why video just stopped (or couldn't start), shown full screen until dismissed. */
    val blocked: StateFlow<String?> = _blocked

    fun block(reason: String) { _blocked.value = reason }
    fun dismiss() { _blocked.value = null }

    private fun key(): String = "${marquee.server?.id}:${marquee.userId}"

    /** The status the server last gave this profile on this device, if any. */
    fun cached(): TimeLimitRules.Cached? {
        val raw = prefs.getString(key(), null) ?: return null
        return TimeLimitRules.Cached.decode(raw)
    }

    /** Asks the server (when reachable) and remembers the answer. Null when it couldn't be asked. */
    suspend fun refresh(): TimeLimitStatus? {
        if (marquee.baseUrl == null || marquee.token == null) return null
        val st = withContext(Dispatchers.IO) { runCatching { marquee.users.myTimeLimit() }.getOrNull() } ?: return null
        prefs.edit().putString(key(), TimeLimitRules.Cached.of(st, System.currentTimeMillis()).encode()).apply()
        return st
    }

    /** Before a download plays on the device: null when it may, else why not. */
    suspend fun refusalForLocal(): String? {
        val st = refresh()
        if (st != null) return if (st.allowedNow) null else st.reason?.takeIf { it.isNotBlank() } ?: TimeLimitRules.DEFAULT_REASON
        val c = cached() ?: return null
        return TimeLimitRules.refusal(c, System.currentTimeMillis(), LocalTime.now())
    }
}

/** The rules behind [TimeLimits], kept apart so they can be tested. */
object TimeLimitRules {
    const val DEFAULT_REASON = "Time's up for now."

    /**
     * The reason in a time_limit refusal (403 with error code time_limit; its message is the
     * reason), or null for any other failure.
     */
    fun reason(e: Throwable?): String? {
        val ce = e as? ClientException ?: return null
        if (ce.statusCode != 403) return null
        val body = (ce.response as? ClientError<*>)?.body?.toString() ?: return null
        return reasonFromBody(body)
    }

    /** The reason in an error body ({code, message}) when its code is time_limit. */
    fun reasonFromBody(body: String): String? {
        val err = runCatching { Serializer.kotlinxSerializationJson.decodeFromString(app.marquee.api.models.Error.serializer(), body) }.getOrNull() ?: return null
        if (err.code != "time_limit") return null
        return err.message.ifBlank { DEFAULT_REASON }
    }

    /** What's remembered of the last status (for playing downloads offline). */
    data class Cached(
        val limited: Boolean,
        val allowedNow: Boolean,
        val reason: String?,
        val resetsAtMs: Long?,
        val allowedFrom: String?,
        val allowedUntil: String?,
        val checkedAtMs: Long,
    ) {
        fun encode(): String = listOf(limited, allowedNow, resetsAtMs ?: "", allowedFrom ?: "", allowedUntil ?: "", checkedAtMs, reason ?: "")
            .joinToString("\u001f")

        companion object {
            fun of(st: TimeLimitStatus, nowMs: Long) = Cached(st.limited, st.allowedNow, st.reason, st.resetsAt?.toInstant()?.toEpochMilli(),
                st.allowedFrom, st.allowedUntil, nowMs)

            fun decode(raw: String): Cached? = runCatching {
                val p = raw.split("\u001f", limit = 7)
                Cached(p[0].toBooleanStrict(), p[1].toBooleanStrict(), p[6].ifEmpty { null }, p[2].ifEmpty { null }?.toLong(),
                    p[3].ifEmpty { null }, p[4].ifEmpty { null }, p[5].toLong())
            }.getOrNull()
        }
    }

    /**
     * Offline, from the last status: refused while it said no and the time it gave for playing
     * again hasn't come; and outside the viewing hours (by the device's clock).
     */
    fun refusal(c: Cached, nowMs: Long, now: LocalTime): String? {
        if (!c.limited) return null
        if (!c.allowedNow && (c.resetsAtMs == null || nowMs < c.resetsAtMs)) return c.reason?.takeIf { it.isNotBlank() } ?: DEFAULT_REASON
        val from = c.allowedFrom?.let(::parseTime)
        val until = c.allowedUntil?.let(::parseTime)
        if (from != null && until != null && !withinHours(now, from, until)) return "Viewing hours are ${c.allowedFrom}–${c.allowedUntil}."
        return null
    }

    /** Whether [t] is within the viewing hours [from]–[until] (which may run past midnight). */
    fun withinHours(t: LocalTime, from: LocalTime, until: LocalTime): Boolean = when {
        from == until -> true
        from < until -> t >= from && t < until
        else -> t >= from || t < until
    }

    private fun parseTime(s: String): LocalTime? = runCatching { LocalTime.parse(s) }.getOrNull()

    /** "45 min left today", "1 hr 5 min left today", or null when there's no daily limit. */
    fun leftLabel(st: TimeLimitStatus?): String? {
        if (st == null || !st.limited) return null
        if (!st.allowedNow) return st.reason?.takeIf { it.isNotBlank() } ?: DEFAULT_REASON
        val m = st.minutesLeft ?: return null
        return "${formatMinutes(m)} left today"
    }

    fun formatMinutes(m: Int): String {
        val h = m / 60
        val r = m % 60
        return when {
            h == 0 -> "$r min"
            r == 0 -> "$h hr"
            else -> "$h hr $r min"
        }
    }
}
