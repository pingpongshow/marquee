import Foundation
import Network
import Observation

/// A server found on the LAN with Bonjour (`_marquee._tcp`).
public struct DiscoveredServer: Identifiable, Hashable, Sendable {
    public var id: String       // serverId from the TXT record (or the service name)
    public var name: String
    public var url: URL
    public var version: String?
}

/// Browses the local network for Marquee servers.
@MainActor @Observable
public final class ServerDiscovery {
    public private(set) var servers: [DiscoveredServer] = []
    public private(set) var searching = false
    private var browser: NWBrowser?
    private var latest: Set<NWBrowser.Result> = []
    private var retry: Task<Void, Never>?

    public init() {}

    public func start() {
        guard browser == nil else { return }
        let params = NWParameters()
        params.includePeerToPeer = false
        let b = NWBrowser(for: .bonjourWithTXTRecord(type: "_marquee._tcp", domain: nil), using: params)
        b.browseResultsChangedHandler = { [weak self] results, _ in
            Task { @MainActor in
                self?.latest = results
                await self?.resolve(results)
            }
        }
        b.stateUpdateHandler = { [weak self] state in
            Task { @MainActor in
                switch state {
                case .ready: self?.searching = true
                case .failed, .cancelled: self?.searching = false
                default: break
                }
            }
        }
        b.start(queue: .main)
        browser = b
        // Resolving can fail transiently (the first attempt often races the network
        // stack), so keep retrying anything not yet resolved.
        retry = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(3))
                guard let self, self.browser != nil else { return }
                if self.servers.count < self.latest.count { await self.resolve(self.latest) }
            }
        }
    }

    public func stop() {
        retry?.cancel()
        retry = nil
        browser?.cancel()
        browser = nil
        searching = false
    }

    private func resolve(_ results: Set<NWBrowser.Result>) async {
        var found: [DiscoveredServer] = []
        for r in results {
            guard case let .service(name, _, _, _) = r.endpoint else { continue }
            var txt: [String: String] = [:]
            if case let .bonjour(record) = r.metadata { txt = record.dictionary }
            guard let host = await Self.resolveHost(r.endpoint) else { continue }
            found.append(DiscoveredServer(id: txt["id"] ?? name, name: txt["name"] ?? name, url: host, version: txt["version"]))
        }
        servers = found.sorted { $0.name < $1.name }
    }

    /// Resolves a Bonjour endpoint to an http:// URL with the IPv4 address.
    nonisolated static func resolveHost(_ endpoint: NWEndpoint) async -> URL? {
        await withCheckedContinuation { cont in
            let params = NWParameters.tcp
            if let ip = params.defaultProtocolStack.internetProtocol as? NWProtocolIP.Options { ip.version = .v4 }
            let conn = NWConnection(to: endpoint, using: params)
            let resumed = ResumeOnce()
            conn.stateUpdateHandler = { state in
                switch state {
                case .ready:
                    var url: URL?
                    if case let .hostPort(host, port)? = conn.currentPath?.remoteEndpoint {
                        var h = "\(host)"
                        if let pct = h.firstIndex(of: "%") { h = String(h[..<pct]) }
                        if h.contains(":") { h = "[\(h)]" }
                        url = URL(string: "http://\(h):\(port.rawValue)")
                    }
                    conn.cancel()
                    if resumed.claim() { cont.resume(returning: url) }
                case .failed, .cancelled:
                    if resumed.claim() { cont.resume(returning: nil) }
                default: break
                }
            }
            conn.start(queue: .global())
            DispatchQueue.global().asyncAfter(deadline: .now() + 4) {
                conn.cancel()
                if resumed.claim() { cont.resume(returning: nil) }
            }
        }
    }
}

final class ResumeOnce: @unchecked Sendable {
    private let lock = NSLock()
    private var done = false
    func claim() -> Bool {
        lock.lock(); defer { lock.unlock() }
        if done { return false }
        done = true
        return true
    }
}
