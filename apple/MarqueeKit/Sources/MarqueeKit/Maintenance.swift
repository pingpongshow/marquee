import Foundation
import MarqueeAPI

public typealias BazarrStatus = Components.Schemas.BazarrStatus
public typealias BazarrCandidate = Components.Schemas.BazarrCandidate
public typealias HealthCheck = Components.Schemas.HealthCheck
public typealias HealthIssue = Components.Schemas.HealthIssue
public typealias MatchCandidate = Components.Schemas.MatchCandidate
public typealias MusicMatchCandidate = Components.Schemas.MusicMatchCandidate
public typealias IntegrationSettings = Components.Schemas.IntegrationSettings
public typealias IssueFile = Components.Schemas.IssueFile
public typealias MediaFile = Components.Schemas.MediaFile
public typealias MediaStream = Components.Schemas.MediaStream
public typealias FileProblem = Components.Schemas.FileProblem
public typealias FileRepair = Components.Schemas.FileRepair

/// Bazarr subtitles (META-12), library health (ADM-11), deleting media files, Fix Match, metadata refresh and the
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

    /// Queues repairs of files whose frames are timed out of order (Frame timing errors): these
    /// files, or every one that can be repaired when fileIDs is nil. Returns how many were queued.
    func repairFrameTiming(_ fileIDs: [Int64]?) async throws -> Int {
        let body = Components.Schemas.FrameTimingRepairRequest(fileIds: fileIDs, all: fileIDs == nil ? true : nil)
        switch try await adminAPI.repairFrameTiming(body: .json(body)) {
        case .accepted(let a): return try a.body.json.queued
        case .notFound: throw MarqueeError("That file can't be repaired.")
        case .forbidden: throw MarqueeError("Only admins can repair files.")
        default: throw MarqueeError("Couldn't queue the repair.")
        }
    }

    /// Takes a waiting or failed repair off the list.
    func cancelFrameTimingRepair(_ fileID: Int64) async throws {
        guard case .noContent = try await adminAPI.cancelFrameTimingRepair(path: .init(fileId: fileID)) else {
            throw MarqueeError("Couldn't cancel the repair.")
        }
    }

    // MARK: - Deleting media files (ADM-11)

    /// Whether admins may delete media files from Library Health (Settings → Libraries).
    func mediaDeletionAllowed() async throws -> Bool {
        try await adminAPI.getSettings().ok.body.json.library.allowMediaDeletion ?? false
    }

    func setMediaDeletionAllowed(_ on: Bool) async throws {
        switch try await adminAPI.updateSettings(body: .json(.init(library: .init(allowMediaDeletion: on)))) {
        case .ok: return
        case .badRequest(let r): throw MarqueeError((try? r.body.json.message) ?? "Couldn't save the setting.")
        case .forbidden: throw MarqueeError("Only admins can change this.")
        default: throw MarqueeError("Couldn't save the setting.")
        }
    }

    /// Moves a media file to its library's .marquee-trash folder (kept for 30 days).
    func deleteMediaFile(_ fileID: Int64) async throws {
        switch try await adminAPI.deleteMediaFile(path: .init(fileId: fileID)) {
        case .noContent: return
        case .forbidden(let r): throw MarqueeError((try? r.body.json.message) ?? "Deleting media files is turned off.")
        case .conflict(let r): throw MarqueeError((try? r.body.json.message) ?? "Couldn't move the file to the trash.")
        case .notFound: throw MarqueeError("That file isn't in the library any more.")
        default: throw MarqueeError("Couldn't delete the file.")
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

    // MARK: - Music fixes (META-3)

    /// MusicBrainz candidates for Fix match of a music artist or album (admin). `artist` is for albums.
    func musicMatchCandidates(_ itemID: Int64, query: String? = nil, artist: String? = nil) async throws -> [MusicMatchCandidate] {
        let q = query?.trimmingCharacters(in: .whitespaces)
        let a = artist?.trimmingCharacters(in: .whitespaces)
        switch try await adminAPI.searchMusicMatches(path: .init(itemId: itemID), query: .init(q: q?.isEmpty == false ? q : nil, artist: a?.isEmpty == false ? a : nil)) {
        case .ok(let ok): return try ok.body.json
        case .badRequest(let r): throw MarqueeError((try? r.body.json.message) ?? "This can't be matched.")
        case .forbidden: throw MarqueeError("Only admins can fix matches.")
        case .notFound: throw MarqueeError("That item isn't here any more.")
        default: throw MarqueeError("Couldn't search MusicBrainz.")
        }
    }

    /// Matches a music artist or album to a MusicBrainz artist or release group (admin).
    @discardableResult
    func applyMusicMatch(_ itemID: Int64, mbid: String) async throws -> ItemDetail {
        switch try await adminAPI.applyMusicMatch(path: .init(itemId: itemID), body: .json(.init(mbid: mbid))) {
        case .ok(let ok): return try ok.body.json
        case .badRequest(let r): throw MarqueeError((try? r.body.json.message) ?? "Couldn't use that match.")
        case .forbidden: throw MarqueeError("Only admins can fix matches.")
        case .notFound: throw MarqueeError("That item isn't here any more.")
        default: throw MarqueeError("Couldn't fix the match.")
        }
    }

    /// Moves a track to an album in its library (admin); the file isn't changed.
    @discardableResult
    func moveTrack(_ trackID: Int64, toAlbum albumID: Int64) async throws -> ItemDetail {
        try await placeTrack(trackID, .init(albumId: albumID))
    }

    /// Moves a track to the album with this artist and title, made when there isn't one (admin).
    @discardableResult
    func moveTrack(_ trackID: Int64, albumArtist: String, album: String) async throws -> ItemDetail {
        try await placeTrack(trackID, .init(albumArtist: albumArtist, album: album))
    }

    private func placeTrack(_ trackID: Int64, _ body: Components.Schemas.TrackPlacementRequest) async throws -> ItemDetail {
        switch try await adminAPI.setTrackPlacement(path: .init(itemId: trackID), body: .json(body)) {
        case .ok(let ok): return try ok.body.json
        case .badRequest(let r): throw MarqueeError((try? r.body.json.message) ?? "Couldn't move the track there.")
        case .forbidden: throw MarqueeError("Only admins can move tracks.")
        case .notFound: throw MarqueeError("That track or album isn't here any more.")
        default: throw MarqueeError("Couldn't move the track.")
        }
    }

    /// Puts a moved track back where its file's tags say it belongs (admin).
    @discardableResult
    func clearTrackPlacement(_ trackID: Int64) async throws -> ItemDetail {
        switch try await adminAPI.clearTrackPlacement(path: .init(itemId: trackID)) {
        case .ok(let ok): return try ok.body.json
        case .badRequest(let r): throw MarqueeError((try? r.body.json.message) ?? "Couldn't put the track back.")
        case .forbidden: throw MarqueeError("Only admins can move tracks.")
        case .notFound: throw MarqueeError("That track isn't here any more.")
        default: throw MarqueeError("Couldn't put the track back.")
        }
    }

    /// The item, or nil when it's gone (an album emptied by a move); other failures throw.
    func itemIfPresent(_ id: Int64) async throws -> ItemDetail? {
        switch try await adminAPI.getItem(path: .init(itemId: id)) {
        case .ok(let ok): return try ok.body.json
        case .notFound: return nil
        default: throw MarqueeError("Couldn't load this page.")
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
