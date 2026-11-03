import Foundation
import Testing
@testable import MarqueeKit

struct InTownFormatTests {
    private let us = Locale(identifier: "en_US")
    private let uk = Locale(identifier: "en_GB")
    /// Midsummer 2026 anywhere.
    private let today = Date(timeIntervalSince1970: 1_782_000_000)

    private func show(date: String = "2026-11-07", time: String? = "19:30", city: String = "San Francisco", region: String? = "CA") -> InTownShow {
        InTownShow(id: "e1", title: "Tour", artistName: "Test Artist 1", lineup: ["Test Artist 1"], support: false, localDate: date, localTime: time,
                   venueName: "August Hall", city: city, region: region, countryCode: "US", status: .scheduled,
                   ticketsUrl: "https://www.ticketmaster.com/event/e1", source: "ticketmaster")
    }

    /// Foundation puts a narrow space before AM and PM.
    private func plain(_ s: String?) -> String? { s?.replacingOccurrences(of: "\u{202F}", with: " ") }

    @Test func datesAreTheVenuesAsTheyAre() {
        #expect(InTownFormat.date("2026-11-07", today: today, locale: us) == "Sat, Nov 7")
        #expect(InTownFormat.date("2026-12-31", today: today, locale: us) == "Thu, Dec 31")
        #expect(InTownFormat.date("2026-11-07", today: today, locale: uk).contains("7 Nov"))
        #expect(InTownFormat.date("2027-01-02", today: today, locale: us) == "Sat, Jan 2, 2027")
    }

    @Test func datesItCantReadComeBackAsTheyAre() {
        for bad in ["soon", "2026-11", "2026-13-01", "2026-02-31", ""] {
            #expect(InTownFormat.date(bad, today: today, locale: us) == bad)
        }
    }

    @Test func timesAreTheVenuesInThePersonsFormat() {
        #expect(plain(InTownFormat.time("19:30", locale: us)) == "7:30 PM")
        #expect(InTownFormat.time("19:30", locale: uk) == "19:30")
        #expect(plain(InTownFormat.time("00:05:00", locale: us)) == "12:05 AM")
        #expect(InTownFormat.time(nil, locale: us) == nil)
        #expect(InTownFormat.time("", locale: us) == nil)
        #expect(InTownFormat.time("late", locale: us) == "late")
        #expect(InTownFormat.time("25:00", locale: us) == "25:00")
    }

    @Test func whenAndWhere() {
        #expect(plain(InTownFormat.when(show(), today: today, locale: us)) == "Sat, Nov 7, 7:30 PM")
        #expect(InTownFormat.when(show(time: nil), today: today, locale: us) == "Sat, Nov 7")
        #expect(InTownFormat.place(show()) == "August Hall, San Francisco, CA")
        #expect(InTownFormat.place(show(region: nil)) == "August Hall, San Francisco")
        #expect(InTownFormat.place(show(region: "")) == "August Hall, San Francisco")
    }

    @Test func theBannersWordingMatchesTheWeb() {
        #expect(InTownFormat.headline(artist: "Test Artist 1", count: 4) == "Test Artist 1 has 4 upcoming shows")
        #expect(InTownFormat.headline(artist: "Test Artist 1", count: 1) == "Test Artist 1 has 1 upcoming show")
        #expect(InTownFormat.nextLine(show(), nearCount: 2, today: today, locale: us) == "Next: Sat, Nov 7, San Francisco · 2 near you")
        #expect(InTownFormat.nextLine(show(), nearCount: 1, today: today, locale: us) == "Next: Sat, Nov 7, San Francisco · One near you")
        #expect(InTownFormat.nextLine(show(), nearCount: 0, today: today, locale: us) == "Next: Sat, Nov 7, San Francisco")
        #expect(InTownFormat.nearLine(0) == nil)
    }

    @Test func distances() {
        #expect(InTownFormat.distance(1.0, unit: "miles") == "1.0 mile away")
        #expect(InTownFormat.distance(4.26, unit: "miles") == "4.3 miles away")
        #expect(InTownFormat.distance(12.6, unit: "miles") == "13 miles away")
        #expect(InTownFormat.distance(4.2, unit: "km") == "4.2 km away")
        #expect(InTownFormat.distance(nil, unit: "km") == nil)
        #expect(InTownFormat.distance(4, unit: nil) == nil)
    }

    @Test func nearTitlePrefersTheCity() {
        #expect(InTownFormat.nearTitle(nil) == "Near you")
        #expect(InTownFormat.nearTitle(InTownLocation(postalCode: "94102", countryCode: "US")) == "Near 94102")
        #expect(InTownFormat.nearTitle(InTownLocation(postalCode: "94102", city: "Oakland", countryCode: "US")) == "Near Oakland")
        #expect(InTownFormat.nearTitle(InTownLocation(postalCode: "94102", city: "", countryCode: "US")) == "Near 94102")
    }

    @Test func countryAndUnitDefaults() {
        #expect(InTownFormat.guessCountry(region: "GB") == "GB")
        #expect(InTownFormat.guessCountry(region: "fr") == "FR")
        #expect(InTownFormat.guessCountry(region: "JP") == "US")
        #expect(InTownFormat.guessCountry(region: nil) == "US")
        #expect(InTownFormat.unit(forCountry: "GB") == .miles)
        #expect(InTownFormat.unit(forCountry: "US") == .miles)
        #expect(InTownFormat.unit(forCountry: "DE") == .km)
        #expect(InTownFormat.defaultDistance(.miles) == 50)
        #expect(InTownFormat.defaultDistance(.km) == 80)
    }

    @Test func onlyWebPagesOpen() {
        #expect(InTownFormat.webURL("https://www.ticketmaster.com/event/e1") != nil)
        #expect(InTownFormat.webURL("http://example.com/e1") != nil)
        for bad in ["file:///Applications/Evil.app", "marquee://item/1", "javascript:alert(1)", "", "not a url", "https://"] {
            #expect(InTownFormat.webURL(bad) == nil, "\(bad)")
        }
    }
}
