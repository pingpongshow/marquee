import Foundation
import MarqueeAPI
import Observation

public typealias DownloadInfo = Components.Schemas.Download

/// Offline downloads (M7, MUSIC-14): videos (original or converted on the server) and music
/// tracks kept on this device, plus progress made while offline, synced when the server is
/// reachable again. Files live in Application Support so the system doesn't purge them.
@MainActor @Observable
public final class Downloads {
    public enum Quality: String, Codable, CaseIterable, Sendable {
        case original, high, medium, low
        public var label: String {
            switch self {
            case .original: "Original"
            case .high: "High (1080p)"
            case .medium: "Medium (720p)"
            case .low: "Low (480p)"
            }
        }
    }

    public enum State: Codable, Equatable, Sendable {
        case preparing(Double) // server conversion
        case downloading(Double)
        case done
        case failed(String)
    }

    public struct Entry: Codable, Identifiable, Sendable {
        public var id: Int64 { item.id }
        public var item: Item
        public var quality: Quality
        public var state: State
        public var file: String? // name inside the downloads folder
        public var poster: String?
        public var size: Int64
        public var jobID: String?
        public var url: String? // server path to fetch
        public var added: Date
        public var resumeMs: Int64?
        /// Downloaded because a synced playlist has it (removed when no synced playlist does).
        public var viaPlaylist: Bool?
        /// The transfer failed for want of the network (leaving home, say): it's tried again,
        /// at the address then in use, when the server is reachable.
        public var networkFailure: Bool?
        /// Music: loudness for volume levelling (MUSIC-9), from the server once it's known;
        /// `gainsChecked` once asked (a track can have none).
        public var trackGainDb: Double?
        public var albumGainDb: Double?
        public var peak: Double?
        public var gainsChecked: Bool?
    }

    /// A play made while offline, from the queue Downloads kept before offline sync (USER-18)
    /// took it over; read once to fold into that queue.
    struct PendingProgress: Codable, Equatable {
        let itemID: Int64
        let positionMs: Int64
        let watched: Bool
        let at: Date
        var server: String? = nil
        var user: Int64? = nil
    }

    /// Transfer errors that come from the network (worth retrying at another address), not
    /// from the server or the file.
    nonisolated static func isNetworkError(_ error: Error) -> Bool {
        let e = error as NSError
        guard e.domain == NSURLErrorDomain else { return false }
        return [NSURLErrorTimedOut, NSURLErrorCannotFindHost, NSURLErrorCannotConnectToHost, NSURLErrorNetworkConnectionLost,
                NSURLErrorDNSLookupFailed, NSURLErrorNotConnectedToInternet, NSURLErrorInternationalRoamingOff,
                NSURLErrorCallIsActive, NSURLErrorDataNotAllowed, NSURLErrorSecureConnectionFailed,
                NSURLErrorBackgroundSessionWasDisconnected].contains(e.code)
    }

    /// Resume data kept from a failed transfer, with the URL it belongs to (it only resumes
    /// against that same address).
    private struct ResumeRecord: Codable {
        let url: String
        let data: Data
    }

    public private(set) var entries: [Int64: Entry] = [:]
    @ObservationIgnored private weak var app: AppSession?
    @ObservationIgnored private var session: URLSession!
    @ObservationIgnored private let bridge = SessionBridge()
    @ObservationIgnored private var polling: Task<Void, Never>?
    /// Guards against overlapping work when attach runs again (every reconnect).
    @ObservationIgnored private var resuming = false
    @ObservationIgnored private var syncing = false
    @ObservationIgnored private var fetchingGains = false
    /// When each transfer last updated its progress (updates are throttled).
    @ObservationIgnored private var lastProgress: [Int64: (at: Date, fraction: Double)] = [:]
    /// A save of the index waiting to happen (saves are coalesced and written off the main thread).
    @ObservationIgnored private var saveTask: Task<Void, Never>?
    private static let io = DispatchQueue(label: "app.marquee.downloads.io", qos: .utility)

    public static let directory: URL = {
        let d = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0].appending(path: "Downloads", directoryHint: .isDirectory)
        try? FileManager.default.createDirectory(at: d, withIntermediateDirectories: true)
        var v = URLResourceValues()
        v.isExcludedFromBackup = true
        var dir = d
        try? dir.setResourceValues(v)
        return d
    }()

    private static var indexURL: URL { directory.appending(path: "index.json") }
    private static var playlistsURL: URL { directory.appending(path: "playlists.json") }

    /// Playlists kept on the device (MUSIC-19): playlist id -> its items when last synced.
    public private(set) var syncedPlaylists: [Int64: Set<Int64>] = [:]

    public init() {
        if let data = try? Data(contentsOf: Self.indexURL), let list = try? JSONDecoder().decode([Entry].self, from: data) {
            entries = Dictionary(uniqueKeysWithValues: list.map { ($0.id, $0) })
        }
        if let data = try? Data(contentsOf: Self.playlistsURL), let m = try? JSONDecoder().decode([Int64: Set<Int64>].self, from: data) {
            syncedPlaylists = m
        }
        let config = URLSessionConfiguration.background(withIdentifier: "app.marquee.downloads")
        config.sessionSendsLaunchEvents = true
        config.isDiscretionary = false
        session = URLSession(configuration: config, delegate: bridge, delegateQueue: nil)
        bridge.owner = self
    }

    /// Connects to the signed-in server: resumes conversions and sends offline progress.
    /// Safe to call again (it runs on every reconnect): only what isn't already under way starts.
    public func attach(_ app: AppSession) {
        self.app = app
        resumeTransfers()
        pollConversions()
        Task {
            await OfflineSync.shared.flush()
            // Playlists kept on the device follow their changes.
            guard !syncing else { return }
            syncing = true
            defer { syncing = false }
            for id in syncedPlaylists.keys { await syncPlaylist(id) }
            saveNow()
            // Tracks downloaded before loudness was kept with them.
            await fetchGains()
        }
    }

    /// Restarts downloads interrupted mid-transfer, skipping any the background session is
    /// still running (it carries on across launches by itself), and those that failed for want
    /// of the network, at the address now in use.
    private func resumeTransfers() {
        let wanted = entries.values.compactMap { e -> (Int64, String)? in
            guard let path = e.url else { return nil }
            switch e.state {
            case .downloading: return (e.id, path)
            case .failed where e.networkFailure == true: return (e.id, path)
            default: return nil
            }
        }
        guard !resuming, !wanted.isEmpty else { return }
        resuming = true
        session.getAllTasks { tasks in
            let live = Set(tasks.filter { $0.state == .running || $0.state == .suspended }.compactMap { $0.taskDescription.flatMap(Int64.init) })
            Task { @MainActor in
                self.resuming = false
                for (id, path) in wanted where !live.contains(id) {
                    guard let e = self.entries[id] else { continue }
                    switch e.state {
                    case .downloading: break
                    case .failed where e.networkFailure == true:
                        self.entries[id]?.state = .downloading(0)
                        self.entries[id]?.networkFailure = nil
                    default: continue
                    }
                    self.fetch(itemID: id, path: path)
                }
                self.save()
            }
        }
    }

    // MARK: - Playlists

    /// Keeps a playlist on the device: downloads what's new in it and drops what left it
    /// (tracks downloaded on their own are never removed). Videos come at Medium quality.
    public func syncPlaylist(_ id: Int64) async {
        guard let app, let entriesNow = try? await app.playlistItems(id) else { return }
        let items = entriesNow.map(\.item).filter { [.track, .movie, .episode, .video].contains($0._type) }
        let now = Set(items.map(\.id))
        let before = syncedPlaylists[id] ?? []
        syncedPlaylists[id] = now
        savePlaylists()
        for it in items where entries[it.id] == nil {
            try? await download(it, quality: it._type == .track ? .original : .medium)
            entries[it.id]?.viaPlaylist = true
        }
        save()
        dropOrphans(Array(before.subtracting(now)))
    }

    /// Stops keeping a playlist; its tracks go unless another synced playlist has them.
    public func unsyncPlaylist(_ id: Int64) {
        guard let ids = syncedPlaylists.removeValue(forKey: id) else { return }
        savePlaylists()
        dropOrphans(Array(ids))
    }

    public func isSynced(_ playlist: Int64) -> Bool { syncedPlaylists[playlist] != nil }

    /// How many of a synced playlist's items are on the device.
    public func syncedCount(_ playlist: Int64) -> (done: Int, total: Int) {
        let ids = syncedPlaylists[playlist] ?? []
        return (ids.filter { entries[$0]?.state == .done }.count, ids.count)
    }

    private func dropOrphans(_ ids: [Int64]) {
        let kept = syncedPlaylists.values.reduce(into: Set<Int64>()) { $0.formUnion($1) }
        for id in ids where !kept.contains(id) && entries[id]?.viaPlaylist == true {
            remove(id)
        }
    }

    private func savePlaylists() {
        if let data = try? JSONEncoder().encode(syncedPlaylists) { try? data.write(to: Self.playlistsURL, options: .atomic) }
    }

    // MARK: - Starting and removing

    /// Downloads an item (a movie, episode or track) at a quality. Tracks are always original.
    public func download(_ item: Item, quality: Quality) async throws {
        guard let app, let client = app.client else { throw MarqueeError("Not connected") }
        let q: Quality = item._type == .track ? .original : quality
        let info = try await client.createDownload(body: .json(.init(itemId: item.id, quality: .init(rawValue: q.rawValue)!))).ok.body.json
        var entry = Entry(item: item, quality: q, state: .preparing(0), file: nil, poster: nil, size: info.size ?? 0,
                          jobID: info.id.hasPrefix("original-") ? nil : info.id, url: info.url, added: Date())
        if info.status == .ready, let url = info.url {
            entry.state = .downloading(0)
            entries[item.id] = entry
            fetch(itemID: item.id, path: url)
        } else {
            entry.state = .preparing(Double(info.progress))
            entries[item.id] = entry
            pollConversions()
        }
        save()
        Task {
            await savePoster(item)
            if item._type == .track { await fetchGains() }
        }
    }

    /// Downloads every playable item under a container (season, show, album, artist, playlist).
    public func download(children items: [Item], quality: Quality) async {
        for it in items where entries[it.id] == nil {
            try? await download(it, quality: quality)
        }
    }

    public func remove(_ itemID: Int64) {
        guard let e = entries.removeValue(forKey: itemID) else { return }
        session.getAllTasks { tasks in
            for t in tasks where t.taskDescription == String(itemID) { t.cancel() }
        }
        for name in [e.file, e.poster].compactMap({ $0 }) {
            try? FileManager.default.removeItem(at: Self.directory.appending(path: name))
        }
        deleteResume(itemID)
        if let job = e.jobID, let client = app?.client {
            Task { _ = try? await client.deleteDownload(path: .init(downloadId: job)) }
        }
        save()
    }

    public func localURL(_ itemID: Int64) -> URL? {
        guard let e = entries[itemID], e.state == .done, let f = e.file else { return nil }
        let u = Self.directory.appending(path: f)
        return FileManager.default.fileExists(atPath: u.path) ? u : nil
    }

    public func posterURL(_ itemID: Int64) -> URL? {
        guard let p = entries[itemID]?.poster else { return nil }
        return Self.directory.appending(path: p)
    }

    /// A downloaded track's loudness (nil values when the server has none or wasn't asked yet).
    public func gains(_ itemID: Int64) -> (track: Double?, album: Double?, peak: Double?)? {
        guard let e = entries[itemID] else { return nil }
        return (e.trackGainDb, e.albumGainDb, e.peak)
    }

    /// Asks the server for the loudness of downloaded tracks that don't have it yet, so they're
    /// levelled offline too. The download API doesn't carry it: a playback session started
    /// (as a preload, which isn't counted as a play) and stopped at once does.
    func fetchGains() async {
        guard !fetchingGains else { return }
        fetchingGains = true
        defer { fetchingGains = false }
        var changed = false
        while let client = app?.client,
              let id = entries.values.first(where: { $0.item._type == .track && $0.gainsChecked != true })?.id {
            let response: Operations.StartPlayback.Output
            do {
                response = try await client.startPlayback(body: .json(.init(itemId: id, startMs: 0, preload: true,
                                                                            profile: AppleDeviceProfile.current())))
            } catch {
                break // the server can't be reached: next time
            }
            switch response {
            case .ok(let ok):
                guard let s = try? ok.body.json else { break }
                _ = try? await client.stopPlayback(path: .init(sessionId: s.id))
                entries[id]?.trackGainDb = s.trackGainDb
                entries[id]?.albumGainDb = s.albumGainDb
                entries[id]?.peak = s.peak
            case .notFound, .forbidden, .badRequest:
                break // gone from the server, or not playable there: nothing to ask for
            default:
                if changed { save() }
                return // busy or signed out: next time
            }
            // Answered (with or without loudness): not asked again.
            entries[id]?.gainsChecked = true
            changed = true
        }
        if changed { save() }
    }

    public var totalBytes: Int64 { entries.values.filter { $0.state == .done }.reduce(0) { $0 + $1.size } }

    // MARK: - Offline progress

    /// Where to resume a downloaded item: progress made offline, else the server's.
    public func resumePosition(_ itemID: Int64) -> Int64 {
        entries[itemID]?.resumeMs ?? entries[itemID]?.item.viewOffsetMs ?? 0
    }

    /// Records progress made on a downloaded item without a server session; it reaches the
    /// server through the offline sync queue (USER-18) with the time it was made.
    public func recordProgress(itemID: Int64, positionMs: Int64, watched: Bool) {
        entries[itemID]?.resumeMs = watched ? 0 : positionMs
        save()
        OfflineSync.shared.enqueue(.progress(itemID, positionMs: positionMs, watched: watched))
        Task { await OfflineSync.shared.flush() }
    }

    // MARK: - Conversions

    private func pollConversions() {
        guard polling == nil, entries.values.contains(where: { if case .preparing = $0.state { return true }; return false }) else { return }
        polling = Task { [weak self] in
            while let self, !Task.isCancelled {
                // The server can't be reached: stop, and start again when it can (attach).
                guard await self.checkConversions() else { break }
                let waiting = self.entries.values.contains { if case .preparing = $0.state { return true }; return false }
                if !waiting { break }
                try? await Task.sleep(for: .seconds(5))
            }
            self?.polling = nil
        }
    }

    /// Checks the conversions under way; false when the server couldn't be reached.
    private func checkConversions() async -> Bool {
        guard let client = app?.client else { return false }
        var asked = false, reached = false
        for e in entries.values {
            guard case .preparing = e.state, let job = e.jobID else { continue }
            asked = true
            let response: Operations.GetDownload.Output
            do { response = try await client.getDownload(path: .init(downloadId: job)) } catch { continue }
            reached = true
            guard let info = try? response.ok.body.json else { continue }
            switch info.status {
            case .ready:
                entries[e.id]?.url = info.url
                entries[e.id]?.size = info.size ?? 0
                entries[e.id]?.state = .downloading(0)
                if let u = info.url { fetch(itemID: e.id, path: u) }
            case .failed:
                entries[e.id]?.state = .failed(info.error ?? "Conversion failed")
            default:
                entries[e.id]?.state = .preparing(Double(info.progress))
            }
        }
        save()
        return reached || !asked
    }

    // MARK: - Transfers

    private func fetch(itemID: Int64, path: String) {
        guard let url = app?.authorizedURL(path) else { return }
        let task: URLSessionDownloadTask
        if let r = loadResume(itemID), r.url == url.absoluteString {
            // Carries on where it stopped (same address).
            task = session.downloadTask(withResumeData: r.data)
            deleteResume(itemID)
        } else {
            // Another address: resume data only works where it came from, so it's kept (for
            // coming back) and this transfer starts over.
            task = session.downloadTask(with: url)
        }
        task.taskDescription = String(itemID)
        task.resume()
    }

    private static func resumeURL(_ itemID: Int64) -> URL { directory.appending(path: "\(itemID).resume") }

    private func loadResume(_ itemID: Int64) -> ResumeRecord? {
        guard let data = try? Data(contentsOf: Self.resumeURL(itemID)) else { return nil }
        return try? JSONDecoder().decode(ResumeRecord.self, from: data)
    }

    private func deleteResume(_ itemID: Int64) {
        try? FileManager.default.removeItem(at: Self.resumeURL(itemID))
    }

    /// Progress comes many times a second; the list redraws about twice a second, or per 1%.
    fileprivate func progressed(itemID: Int64, fraction: Double) {
        guard entries[itemID] != nil else { return }
        let now = Date()
        if let last = lastProgress[itemID], now.timeIntervalSince(last.at) < 0.5, fraction - last.fraction < 0.01, fraction < 1 { return }
        lastProgress[itemID] = (now, fraction)
        entries[itemID]?.state = .downloading(fraction)
    }

    fileprivate func finished(itemID: Int64, temp: URL, suggested: String?) {
        lastProgress[itemID] = nil
        guard var e = entries[itemID] else {
            // Removed while it downloaded: the file isn't wanted.
            try? FileManager.default.removeItem(at: temp)
            return
        }
        let ext = (suggested as NSString?)?.pathExtension.nilIfEmpty ?? (e.quality == .original ? "mkv" : "mp4")
        let name = "\(itemID).\(ext)"
        let dest = Self.directory.appending(path: name)
        try? FileManager.default.removeItem(at: dest)
        do {
            try FileManager.default.moveItem(at: temp, to: dest)
            e.file = name
            e.state = .done
            e.size = ((try? FileManager.default.attributesOfItem(atPath: dest.path)[.size]) as? Int64) ?? e.size
        } catch {
            e.state = .failed(error.localizedDescription)
        }
        e.networkFailure = nil
        entries[itemID] = e
        deleteResume(itemID)
        saveNow()
        // The converted copy on the server is no longer needed.
        if let job = e.jobID, let client = app?.client {
            Task { _ = try? await client.deleteDownload(path: .init(downloadId: job)) }
        }
    }

    fileprivate func failed(itemID: Int64, message: String, network: Bool = false, resumeData: Data? = nil, url: URL? = nil) {
        guard entries[itemID] != nil, entries[itemID]?.state != .done else { return }
        entries[itemID]?.state = .failed(message)
        entries[itemID]?.networkFailure = network ? true : nil
        if let resumeData, let url, let data = try? JSONEncoder().encode(ResumeRecord(url: url.absoluteString, data: resumeData)) {
            try? data.write(to: Self.resumeURL(itemID), options: .atomic)
        }
        save()
    }

    private func savePoster(_ item: Item) async {
        let art = item.images?.poster ?? item.images?.thumb
        guard let url = app?.imageURL(art, width: 300), let (data, _) = try? await URLSession.shared.data(from: url) else { return }
        let name = "\(item.id)-poster.jpg"
        try? data.write(to: Self.directory.appending(path: name), options: .atomic)
        entries[item.id]?.poster = name
        save()
    }

    /// Saves the index soon: changes in quick succession (a playlist sync, progress) are
    /// written once, encoded off the main thread.
    private func save() {
        guard saveTask == nil else { return }
        saveTask = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(500))
            guard let self, !Task.isCancelled else { return }
            self.saveTask = nil
            self.writeIndex()
        }
    }

    /// Saves the index now (a finished download, the end of a sync).
    private func saveNow() {
        saveTask?.cancel()
        saveTask = nil
        writeIndex()
    }

    private func writeIndex() {
        let list = Array(entries.values), url = Self.indexURL
        Self.io.async {
            if let data = try? JSONEncoder().encode(list) { try? data.write(to: url, options: .atomic) }
        }
    }

    /// Called by the app delegate when the system relaunches the app for finished downloads.
    public var backgroundCompletion: (() -> Void)? {
        get { bridge.completion }
        set { bridge.completion = newValue }
    }
}

/// URLSession delegate (called off the main thread) forwarding to Downloads.
private final class SessionBridge: NSObject, URLSessionDownloadDelegate, @unchecked Sendable {
    weak var owner: Downloads?
    var completion: (() -> Void)?

    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask, didFinishDownloadingTo location: URL) {
        guard let id = downloadTask.taskDescription.flatMap(Int64.init) else { return }
        if let http = downloadTask.response as? HTTPURLResponse, http.statusCode >= 300 {
            let code = http.statusCode
            Task { @MainActor in self.owner?.failed(itemID: id, message: "Server error \(code)") }
            return
        }
        // The temporary file is deleted when this returns: move it somewhere safe first.
        let keep = FileManager.default.temporaryDirectory.appending(path: "marquee-\(id)-\(UUID().uuidString)")
        do { try FileManager.default.moveItem(at: location, to: keep) } catch { return }
        let suggested = downloadTask.response?.suggestedFilename
        Task { @MainActor in self.owner?.finished(itemID: id, temp: keep, suggested: suggested) }
    }

    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask, didWriteData bytesWritten: Int64,
                    totalBytesWritten: Int64, totalBytesExpectedToWrite: Int64) {
        guard let id = downloadTask.taskDescription.flatMap(Int64.init), totalBytesExpectedToWrite > 0 else { return }
        let f = Double(totalBytesWritten) / Double(totalBytesExpectedToWrite)
        Task { @MainActor in self.owner?.progressed(itemID: id, fraction: f) }
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: Error?) {
        guard let error, let id = task.taskDescription.flatMap(Int64.init) else { return }
        if (error as NSError).code == NSURLErrorCancelled { return }
        let msg = error.localizedDescription
        let network = Downloads.isNetworkError(error)
        // Kept so the transfer can carry on rather than start over.
        let resume = (error as NSError).userInfo[NSURLSessionDownloadTaskResumeData] as? Data
        let url = task.originalRequest?.url
        Task { @MainActor in self.owner?.failed(itemID: id, message: msg, network: network, resumeData: resume, url: url) }
    }

    func urlSessionDidFinishEvents(forBackgroundURLSession session: URLSession) {
        let done = completion
        completion = nil
        DispatchQueue.main.async { done?() }
    }
}

private extension String {
    var nilIfEmpty: String? { isEmpty ? nil : self }
}
