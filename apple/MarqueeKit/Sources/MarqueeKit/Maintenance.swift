import Foundation
import MarqueeAPI

public typealias BazarrStatus = Components.Schemas.BazarrStatus
public typealias BazarrCandidate = Components.Schemas.BazarrCandidate
public typealias HealthCheck = Components.Schemas.HealthCheck
public typealias HealthIssue = Components.Schemas.HealthIssue
public typealias MatchCandidate = Components.Schemas.MatchCandidate
public typealias IntegrationSettings = Components.Schemas.IntegrationSettings

/// Bazarr subtitles (META-12), library health (ADM-11), Fix Match, metadata refresh and the
/// integration settings they need (admin).
@MainActor
public extension AppSession {
    private var adminAPI: Client {
        get throws {
            guard let client else { throw MarqueeError("Not connected") }
            return client
        }
    }

    // MARK: - Bazarr (META-12)

    func bazarrStatus(_ itemID: Int64) async throws -> BazarrStatus {
        switch try await adminAPI.bazarrStatus(path: .init(itemId: itemID)) {
        case .ok(let ok): return try ok.body.json
        case .notFound: throw MarqueeError("That item isn't here any more.")
        default: throw MarqueeError("Couldn't ask Bazarr.")
        }
    }

    /// Asks Bazarr for the best subtitle in a language; it arrives on the item a little later.
    func bazarrDownload(_ itemID: Int64, language: String, forced: Bool = false, hi: Bool = false) async throws {
        switch try await adminAPI.bazarrDownload(path: .init(itemId: itemID), body: .json(.init(language: language, forced: forced, hi: hi))) {
        case .accepted: return
        case .badRequest(let r): throw MarqueeError((try? r.body.json.message) ?? "Bazarr can't do that.")
        case .badGateway(let r): throw MarqueeError((try? r.body.json.message) ?? "Bazarr didn't answer.")
        case .notFound: throw MarqueeError("Bazarr doesn't know this title.")
        default: throw MarqueeError("Couldn't ask Bazarr.")
        }
    }

    /// Searches every Bazarr provider (can take up to a minute).
    func bazarrSearch(_ itemID: Int64) async throws -> [BazarrCandidate] {
        switch try await adminAPI.bazarrSearch(path: .init(itemId: itemID)) {
        case .ok(let ok): return try ok.body.json
        case .badGateway(let r): throw MarqueeError((try? r.body.json.message) ?? "Bazarr didn't answer.")
        case .notFound: throw MarqueeError("Bazarr doesn't know this title.")
        default: throw MarqueeError("Couldn't search.")
        }
    }

    func bazarrPick(_ itemID: Int64, _ c: BazarrCandidate) async throws {
        let body = Components.Schemas.BazarrPick(provider: c.provider, subtitle: c.subtitle, hi: c.hi, forced: c.forced, originalFormat: c.originalFormat)
        switch try await adminAPI.bazarrPick(path: .init(itemId: itemID), body: .json(body)) {
        case .accepted: return
        case .badGateway(let r): throw MarqueeError((try? r.body.json.message) ?? "Bazarr didn't answer.")
        case .notFound: throw MarqueeError("Bazarr doesn't know this title.")
        default: throw MarqueeError("Couldn't download the subtitle.")
        }
    }

    // MARK: - Library health (ADM-11)

    func libraryHealth() async throws -> [HealthCheck] {
        switch try await adminAPI.libraryHealth() {
        case .ok(let ok): return try ok.body.json
        case .forbidden: throw MarqueeError("Only admins can see library health.")
        default: throw MarqueeError("Couldn't load library health.")
        }
    }

    func healthIssues(_ check: String, offset: Int, limit: Int = 50) async throws -> Schemas.HealthIssuePage {
        switch try await adminAPI.libraryHealthIssues(path: .init(checkId: check), query: .init(offset: offset, limit: limit)) {
        case .ok(let ok): return try ok.body.json
        case .notFound: throw MarqueeError("There's no such check.")
        case .forbidden: throw MarqueeError("Only admins can see library health.")
        default: throw MarqueeError("Couldn't load the issues.")
        }
    }

    /// Stops reporting an item for a check, or (ignored false) reports it again.
    func setHealthIgnored(_ check: String, itemID: Int64, _ ignored: Bool) async throws {
        if ignored {
            guard case .noContent = try await adminAPI.ignoreHealthIssue(path: .init(checkId: check, itemId: itemID)) else {
                throw MarqueeError("Couldn't ignore it.")
            }
        } else {
            guard case .noContent = try await adminAPI.unignoreHealthIssue(path: .init(checkId: check, itemId: itemID)) else {
                throw MarqueeError("Couldn't undo.")
            }
        }
    }

    // MARK: - Metadata

    /// Re-downloads a title's metadata (admin).
    func refreshMetadata(_ itemID: Int64) async throws {
        switch try await adminAPI.refreshItem(path: .init(itemId: itemID)) {
        case .ok: return
        case .badRequest(let r): throw MarqueeError((try? r.body.json.message) ?? "This title can't be refreshed.")
        case .forbidden: throw MarqueeError("Only admins can refresh metadata.")
        default: throw MarqueeError("Couldn't refresh the metadata.")
        }
    }

    /// Fix Match candidates for a movie or show (admin).
    func matchCandidates(_ itemID: Int64, title: String? = nil, year: Int? = nil) async throws -> [MatchCandidate] {
        switch try await adminAPI.searchMatches(path: .init(itemId: itemID), query: .init(title: title, year: year)) {
        case .ok(let ok): return try ok.body.json
        case .badRequest(let r): throw MarqueeError((try? r.body.json.message) ?? "This title can't be matched.")
        case .forbidden: throw MarqueeError("Only admins can fix matches.")
        default: throw MarqueeError("Couldn't search for matches.")
        }
    }

    func applyMatch(_ itemID: Int64, _ c: MatchCandidate) async throws {
        switch try await adminAPI.applyMatch(path: .init(itemId: itemID), body: .json(.init(provider: .tmdb, id: c.id))) {
        case .ok: return
        case .badRequest(let r): throw MarqueeError((try? r.body.json.message) ?? "Couldn't use that match.")
        default: throw MarqueeError("Couldn't fix the match.")
        }
    }

    // MARK: - Integrations (admin)

    func integrationSettings() async throws -> IntegrationSettings {
        try await adminAPI.getSettings().ok.body.json.integrations ?? .init()
    }

    /// Saves Seerr and Bazarr. Keys are write-only: nil leaves a key as it is.
    func saveIntegrations(seerrURL: String, seerrKey: String?, bazarrURL: String, bazarrKey: String?) async throws {
        let body = Components.Schemas.IntegrationSettingsUpdate(seerrUrl: seerrURL, seerrApiKey: seerrKey, bazarrUrl: bazarrURL, bazarrApiKey: bazarrKey)
        switch try await adminAPI.updateSettings(body: .json(.init(integrations: body))) {
        case .ok: return
        case .badRequest(let r): throw MarqueeError((try? r.body.json.message) ?? "Couldn't save the settings.")
        case .forbidden: throw MarqueeError("Only admins can change this.")
        default: throw MarqueeError("Couldn't save the settings.")
        }
    }
}

/// Common subtitle languages (ISO 639-1) for "Download another language".
public let commonSubtitleLanguages: [(code: String, name: String)] = [
    ("en", "English"), ("es", "Spanish"), ("fr", "French"), ("de", "German"), ("it", "Italian"), ("pt", "Portuguese"),
    ("nl", "Dutch"), ("sv", "Swedish"), ("da", "Danish"), ("no", "Norwegian"), ("fi", "Finnish"), ("pl", "Polish"),
    ("ru", "Russian"), ("ja", "Japanese"), ("ko", "Korean"), ("zh", "Chinese"), ("ar", "Arabic"), ("hi", "Hindi"),
]
