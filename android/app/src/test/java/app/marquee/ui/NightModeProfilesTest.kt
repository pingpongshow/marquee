package app.marquee.ui

import app.marquee.api.models.Profile
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** Night mode's player switch, the VOD Recommended route and what a profile switch asks for. */
class NightModeProfilesTest {
    @Test fun nightModeFollowsTheChoiceThenTheSessionThenThePreference() {
        assertTrue(nightModeOn(chosen = true, session = false, preference = false))
        assertFalse(nightModeOn(chosen = false, session = true, preference = true))
        assertTrue(nightModeOn(chosen = null, session = true, preference = false))
        assertTrue(nightModeOn(chosen = null, session = null, preference = true))
        assertFalse(nightModeOn(chosen = null, session = null, preference = null))
    }

    @Test fun recommendedRouteNamesTheKind() {
        assertEquals("vodrecommended?kind=movie", vodRecommendedRoute(shows = false))
        assertEquals("vodrecommended?kind=series", vodRecommendedRoute(shows = true))
    }

    @Test fun adminsOpenManagedProfilesDirectly() {
        val kid = Profile(id = 2, displayName = "Kid", isManaged = true, requires = Profile.Requires.PIN)
        val adult = Profile(id = 3, displayName = "Sam", isManaged = false, requires = Profile.Requires.PASSWORD)
        assertEquals(Profile.Requires.NONE, profileNeeds(kid, isAdmin = true))
        assertEquals(Profile.Requires.PIN, profileNeeds(kid, isAdmin = false))
        assertEquals(Profile.Requires.PASSWORD, profileNeeds(adult, isAdmin = true))
    }
}
