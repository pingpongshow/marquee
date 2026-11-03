import Foundation
import MarqueeAPI

public typealias InTownPage = Components.Schemas.InTownPage
public typealias InTownShow = Components.Schemas.InTownShow
public typealias InTownArtist = Components.Schemas.InTownArtist
public typealias InTownLocation = Components.Schemas.InTownLocation
public typealias InTownAttribution = Components.Schemas.InTownAttribution
public typealias ItemShows = Components.Schemas.ItemShows

/// In Town (MUSIC-27): concerts for you, from the admin's own Ticketmaster key.
@MainActor
public extension AppSession {
    private var inTownAPI: Client {
        get throws {
            guard let client else { throw MarqueeError("Not connected") }
            return client
        }
    }

    /// Your artists on tour and the shows near you. May ask Ticketmaster, and the server waits up
    /// to 20 s for it the first time: on the client that doesn't give up at 20 s itself.
    func inTown() async throws -> InTownPage {
        guard let longPollClient else { throw MarqueeError("Not connected") }
        return try await longPollClient.getInTown().ok.body.json
    }

    /// An artist's upcoming shows, soonest first. Answers from what the server has stored.
    func itemShows(_ id: Int64) async throws -> ItemShows {
        try await inTownAPI.getItemShows(path: .init(itemId: id)).ok.body.json
    }

    func myLocation() async throws -> InTownLocation? {
        try await inTownAPI.getMyLocation().ok.body.json.location
    }

    /// Saves where this person is. What's wrong with it comes back from the server as a sentence.
    func setMyLocation(_ location: InTownLocation) async throws -> InTownLocation {
        switch try await inTownAPI.setMyLocation(body: .json(location)) {
        case let .ok(ok): return try ok.body.json
        case let .badRequest(e): throw MarqueeError((try? e.body.json.message) ?? "Couldn't save your location.")
        default: throw MarqueeError("Couldn't save your location.")
        }
    }

    func removeMyLocation() async throws {
        switch try await inTownAPI.deleteMyLocation() {
        case .noContent: return
        default: throw MarqueeError("Couldn't remove your location.")
        }
    }
}

/// In Town's wording and dates, the same as on the web. A show's date and time are the venue's,
/// whichever zone the person is in, so they're read as they are rather than converted.
public enum InTownFormat {
    /// Only to hold a date while it's formatted: read back in the same zone, it can't shift.
    private static let zone = TimeZone(secondsFromGMT: 0)!

    private static var calendar: Calendar {
        var c = Calendar(identifier: .gregorian)
        c.timeZone = zone
        return c
    }

    /// A venue-local date, as "Sat, Nov 7", with the year when it isn't this one.
    public static func date(_ localDate: String, today: Date = Date(), locale: Locale = .current) -> String {
        let p = localDate.split(separator: "-").map { Int($0) }
        guard p.count == 3, let y = p[0], let m = p[1], let d = p[2] else { return localDate }
        let parts = DateComponents(calendar: calendar, year: y, month: m, day: d)
        guard parts.isValidDate, let at = parts.date else { return localDate }
        var style = Date.FormatStyle(locale: locale, timeZone: zone).weekday(.abbreviated).month(.abbreviated).day()
        if y != Calendar.current.component(.year, from: today) { style = style.year() }
        return at.formatted(style)
    }

    /// A venue-local "HH:mm" in the person's own time format; nil without one.
    public static func time(_ localTime: String?, locale: Locale = .current) -> String? {
        guard let localTime, !localTime.isEmpty else { return nil }
        let p = localTime.split(separator: ":").map { Int($0) }
        guard p.count >= 2, let h = p[0], let m = p[1] else { return localTime }
        let parts = DateComponents(calendar: calendar, year: 2000, month: 1, day: 1, hour: h, minute: m)
        guard parts.isValidDate, let at = parts.date else { return localTime }
        return at.formatted(Date.FormatStyle(date: .omitted, time: .shortened, locale: locale, timeZone: zone))
    }

    /// "Sat, Nov 7, 7:30 PM", or only the date when the time isn't known.
    public static func when(_ show: InTownShow, today: Date = Date(), locale: Locale = .current) -> String {
        [date(show.localDate, today: today, locale: locale), time(show.localTime, locale: locale)].compactMap { $0 }.joined(separator: ", ")
    }

    /// "August Hall, San Francisco, CA".
    public static func place(_ show: InTownShow) -> String {
        [show.venueName, show.city, show.region ?? ""].filter { !$0.isEmpty }.joined(separator: ", ")
    }

    /// "4 upcoming shows".
    public static func showCount(_ count: Int) -> String {
        "\(count) upcoming \(count == 1 ? "show" : "shows")"
    }

    /// "<Artist> has 3 upcoming shows".
    public static func headline(artist: String, count: Int) -> String {
        "\(artist) has \(showCount(count))"
    }

    /// How many of someone's shows are near them; nil for none.
    public static func nearLine(_ nearCount: Int) -> String? {
        if nearCount == 0 { return nil }
        return nearCount == 1 ? "One near you" : "\(nearCount) near you"
    }

    /// The rest of the artist banner after its headline: "Next: Sat, Nov 7, San Francisco · 2 near you".
    public static func nextLine(_ next: InTownShow, nearCount: Int, today: Date = Date(), locale: Locale = .current) -> String {
        let line = "Next: \(date(next.localDate, today: today, locale: locale)), \(next.city)"
        return nearLine(nearCount).map { "\(line) · \($0)" } ?? line
    }

    /// The distance a show is away, in the person's unit.
    public static func distance(_ distance: Double?, unit: String?) -> String? {
        guard let distance, let unit else { return nil }
        let rounded = distance < 10 ? String(format: "%.1f", distance) : String(Int(distance.rounded()))
        return unit == "km" ? "\(rounded) km away" : "\(rounded) \(rounded == "1.0" ? "mile" : "miles") away"
    }

    /// The lower area's title: "Near Oakland", from the city or else the postcode.
    public static func nearTitle(_ location: InTownLocation?) -> String {
        let city = location?.city ?? "", place = city.isEmpty ? location?.postalCode ?? "" : city
        return place.isEmpty ? "Near you" : "Near \(place)"
    }

    /// The countries offered for a location, those Ticketmaster covers most.
    public static let countries = [
        "US", "GB", "CA", "IE", "AU", "NZ", "AT", "BE", "CZ", "DK", "FI", "FR", "DE", "IT", "MX", "NL", "NO", "PL", "PT", "ES", "SE", "CH",
    ]

    /// This Mac's country when it's one of them, else the US.
    public static func guessCountry(region: String? = Locale.current.region?.identifier) -> String {
        if let region = region?.uppercased(), countries.contains(region) { return region }
        return "US"
    }

    /// Miles where road signs use them.
    public static func unit(forCountry country: String) -> InTownLocation.UnitPayload {
        country == "GB" || country == "US" ? .miles : .km
    }

    /// How far someone would go until they say.
    public static func defaultDistance(_ unit: InTownLocation.UnitPayload) -> Int {
        unit == .km ? 80 : 50
    }

    /// A link from the server to open in the browser: only ever a web page, as what comes back gets opened.
    public static func webURL(_ string: String) -> URL? {
        guard let url = URL(string: string), let scheme = url.scheme?.lowercased(), scheme == "https" || scheme == "http", url.host() != nil else { return nil }
        return url
    }
}
