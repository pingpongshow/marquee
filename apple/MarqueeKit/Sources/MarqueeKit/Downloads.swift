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
    }

    /// A play made while offline, waiting to be sent.
    struct PendingProgress: Codable {
        let itemID: Int64
        let positionMs: Int64
        let watched: Bool
        let at: Date
    }

    public private(set) var entries: [Int64: Entry] = [:]
    @ObservationIgnored private var pending: [PendingProgress] = []
    @ObservationIgnored private weak var app: AppSession?
    @ObservationIgnored private var session: URLSession!
    @ObservationIgnored private let bridge = SessionBridge()
    @ObservationIgnored private var polling: Task<Void, Never>?

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
    private static var pendingURL: URL { directory.appending(path: "pending.json") }

    public init() {
        if let data = try? Data(contentsOf: Self.indexURL), let list = try? JSONDecoder().decode([Entry].self, from: data) {
            entries = Dictionary(uniqueKeysWithValues: list.map { ($0.id, $0) })
        }
        if let data = try? Data(contentsOf: Self.pendingURL) {
            pending = (try? JSONDecoder().decode([PendingProgress].self, from: data)) ?? []
        }
        let config = URLSessionConfiguration.background(withIdentifier: "app.marquee.downloads")
        config.sessionSendsLaunchEvents = true
        config.isDiscretionary = false
        session = URLSession(configuration: config, delegate: bridge, delegateQueue: nil)
        bridge.owner = self
    }

    /// Connects to the signed-in server: resumes conversions and sends offline progress.
    public func attach(_ app: AppSession) {
        self.app = app
        // Anything interrupted mid-download restarts (the system may have finished it already).
        for e in entries.values where !(e.state == .done) {
            if case .failed = e.state { continue }
            if e.jobID != nil, e.url == nil { continue }
            if case .downloading = e.state, let path = e.url { fetch(itemID: e.id, path: path) }
        }
        pollConversions()
        Task { await flushProgress() }
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
        Task { await savePoster(item) }
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

    public var totalBytes: Int64 { entries.values.filter { $0.state == .done }.reduce(0) { $0 + $1.size } }

    // MARK: - Offline progress

    /// Records progress made on a downloaded item without a server session.
    public func resumePosition(_ itemID: Int64) -> Int64 {
        entries[itemID]?.resumeMs ?? entries[itemID]?.item.viewOffsetMs ?? 0
    }

    public func recordProgress(itemID: Int64, positionMs: Int64, watched: Bool) {
        entries[itemID]?.resumeMs = watched ? 0 : positionMs
        save()
        pending.removeAll { $0.itemID == itemID && !$0.watched }
        pending.append(PendingProgress(itemID: itemID, positionMs: positionMs, watched: watched, at: Date()))
        if let data = try? JSONEncoder().encode(pending) { try? data.write(to: Self.pendingURL, options: .atomic) }
        Task { await flushProgress() }
    }

    public func flushProgress() async {
        guard let client = app?.client, !pending.isEmpty else { return }
        var left: [PendingProgress] = []
        for p in pending {
            do {
                _ = try await client.syncProgress(path: .init(itemId: p.itemID),
                                                  body: .json(.init(positionMs: p.positionMs, watched: p.watched, playedAt: p.at))).noContent
            } catch {
                left.append(p)
            }
        }
        pending = left
        if let data = try? JSONEncoder().encode(pending) { try? data.write(to: Self.pendingURL, options: .atomic) }
    }

    // MARK: - Conversions

    private func pollConversions() {
        guard polling == nil, entries.values.contains(where: { if case .preparing = $0.state { return true }; return false }) else { return }
        polling = Task { [weak self] in
            while let self, !Task.isCancelled {
                await self.checkConversions()
                let waiting = self.entries.values.contains { if case .preparing = $0.state { return true }; return false }
                if !waiting { break }
                try? await Task.sleep(for: .seconds(5))
            }
            self?.polling = nil
        }
    }

    private func checkConversions() async {
        guard let client = app?.client else { return }
        for e in entries.values {
            guard case .preparing = e.state, let job = e.jobID else { continue }
            guard let info = try? await client.getDownload(path: .init(downloadId: job)).ok.body.json else { continue }
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
    }

    // MARK: - Transfers

    private func fetch(itemID: Int64, path: String) {
        guard let url = app?.authorizedURL(path) else { return }
        let task = session.downloadTask(with: url)
        task.taskDescription = String(itemID)
        task.resume()
    }

    fileprivate func progressed(itemID: Int64, fraction: Double) {
        guard entries[itemID] != nil else { return }
        entries[itemID]?.state = .downloading(fraction)
    }

    fileprivate func finished(itemID: Int64, temp: URL, suggested: String?) {
        guard var e = entries[itemID] else { return }
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
        entries[itemID] = e
        save()
        // The converted copy on the server is no longer needed.
        if let job = e.jobID, let client = app?.client {
            Task { _ = try? await client.deleteDownload(path: .init(downloadId: job)) }
        }
    }

    fileprivate func failed(itemID: Int64, message: String) {
        guard entries[itemID] != nil, entries[itemID]?.state != .done else { return }
        entries[itemID]?.state = .failed(message)
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

    private func save() {
        if let data = try? JSONEncoder().encode(Array(entries.values)) {
            try? data.write(to: Self.indexURL, options: .atomic)
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
        Task { @MainActor in self.owner?.failed(itemID: id, message: msg) }
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
