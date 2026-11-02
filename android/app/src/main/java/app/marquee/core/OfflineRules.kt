package app.marquee.core

/**
 * When a failed request makes the app ask whether the server has gone. One network error says
 * little (a dropped connection, a timeout on a busy server), so it only starts a check, and only
 * checks in a row that all fail count. HTTP errors never come here: the server answered them.
 * Plain logic, so it can be tested without a device or a server; [Marquee] does the checking.
 */
class OfflineRules(private val quietMs: Long = 15_000, private val failedChecks: Int = 2) {
    enum class Next { Reachable, CheckAgain, Lost }

    private var checking = false
    private var failures = 0
    private var reachableAt: Long? = null

    /**
     * A request failed by network error at [nowMs]: true when a check should start. Not while one
     * is on its way, not right after one found the server there, and not when already offline.
     */
    @Synchronized fun requestFailed(nowMs: Long, offline: Boolean): Boolean {
        if (offline || checking) return false
        reachableAt?.let { if (nowMs - it < quietMs) return false }
        checking = true
        failures = 0
        return true
    }

    /** A check given up on, the app having gone offline meanwhile. */
    @Synchronized fun checkDropped() {
        checking = false
        failures = 0
    }

    /** A check's answer: the server's there, ask once more, or it's gone (try every address). */
    @Synchronized fun checked(reachable: Boolean, nowMs: Long): Next {
        if (reachable) {
            checking = false
            failures = 0
            reachableAt = nowMs
            return Next.Reachable
        }
        if (++failures < failedChecks) return Next.CheckAgain
        checking = false
        failures = 0
        return Next.Lost
    }
}
