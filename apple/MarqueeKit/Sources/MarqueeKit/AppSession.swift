import Foundation
import HTTPTypes
import MarqueeAPI
import Network
import Observation
import OpenAPIRuntime
import OpenAPIURLSession
#if canImport(UIKit)
import UIKit
#endif

public typealias Schemas = Components.Schemas
public typealias Item = Components.Schemas.ItemSummary
public typealias ItemDetail = Components.Schemas.ItemDetail
public typealias User = Components.Schemas.User

/// Errors shown to people, with the server's own message when it sent one.
public struct MarqueeError: LocalizedError, Sendable {
    public let message: String
    public init(_ message: String) { self.message = message }
    public var errorDescription: String? { message }
}

/// Sign-in needs the code from an authenticator app (two-factor, USER-9).
public struct TwoFactorRequired: LocalizedError, Sendable {
    public var errorDescription: String? { "Enter the code from your authenticator app." }
}

/// The signed-in token, readable from the networking threads.
final class TokenBox: @unchecked Sendable {
    private let lock = NSLock()
    private var value: String?
    var token: String? {
        get { lock.lock(); defer { lock.unlock() }; return value }
        set { lock.lock(); value = newValue; lock.unlock() }
    }
}

/// Adds the bearer token to every API request.
struct AuthMiddleware: ClientMiddleware {
    let token: @Sendable () -> String?
    func intercept(_ request: HTTPRequest, body: HTTPBody?, baseURL: URL, operationID: String,
                   next: @Sendable (HTTPRequest, HTTPBody?, URL) async throws -> (HTTPResponse, HTTPBody?)) async throws -> (HTTPResponse, HTTPBody?) {
        var request = request
        if let t = token() { request.headerFields[.authorization] = "Bearer \(t)" }
        return try await next(request, body, baseURL)
    }
}

/// The server sends RFC 3339 dates with or without fractional seconds.
struct FlexibleDateTranscoder: DateTranscoder {
    func encode(_ date: Date) throws -> String { date.formatted(.iso8601) }
    func decode(_ string: String) throws -> Date {
        if let d = try? Date(string, strategy: Date.ISO8601FormatStyle(includingFractionalSeconds: true)) { return d }
        if let d = try? Date(string, strategy: .iso8601) { return d }
        throw MarqueeError("Unreadable date \(string)")
    }
}

/// Everything about the current connection: which server, which address, who's signed in.
@MainActor @Observable
public final class AppSession {
    public enum State: Equatable { case noServer, connecting, signedOut, signedIn }

    public private(set) var state: State = .noServer
    public private(set) var server: ServerRecord?
    /// The address in use (LAN or Tailscale).
    public private(set) var baseURL: URL?
    public private(set) var info: Schemas.SystemInfo?
    public private(set) var me: User?
    public private(set) var lastError: String?
    public private(set) var client: Client?
    /// For requests the server holds open (remote control's long polls, USER-14).
    public private(set) var longPollClient: Client?
    /// True while the address is being re-checked in the background (the app stays usable).
    public private(set) var isReconnecting = false
    /// Called when the signed-in person goes away or changes (sign out, profile switch,
    /// forgetting the server), so playback that belongs to them can stop.
    @ObservationIgnored public var onUserChange: (() -> Void)?

    private let tokenBox = TokenBox()
    private var token: String? {
        get { tokenBox.token }
        set { tokenBox.token = newValue }
    }
    private let pathMonitor = NWPathMonitor()
    /// The last network seen: its status and the kinds of interface in use.
    @ObservationIgnored private var lastPath: String?
    @ObservationIgnored private var pathDebounce: Task<Void, Never>?
    @ObservationIgnored private var reconnectTask: Task<Void, Never>?

    public init() {
        if let id = ServerStore.currentServerID, let s = ServerStore.servers.first(where: { $0.id == id }) {
            server = s
            token = ServerStore.token(for: s.id)
            state = .connecting
            Task { await reconnect() }
        }
        // Re-check the address when the network changes (Wi-Fi ↔ cellular, leaving home).
        // Path updates come in bursts and often repeat the same network, so they're
        // debounced and only a real change re-probes.
        pathMonitor.pathUpdateHandler = { [weak self] path in
            let kinds: [NWInterface.InterfaceType] = [.wifi, .cellular, .wiredEthernet, .other, .loopback]
            let signature = "\(path.status)|" + kinds.filter { path.usesInterfaceType($0) }.map { "\($0)" }.joined(separator: ",")
                + "|" + path.availableInterfaces.map(\.name).joined(separator: ",")
            Task { @MainActor in self?.pathChanged(signature) }
        }
        pathMonitor.start(queue: .global(qos: .utility))
    }

    private func pathChanged(_ signature: String) {
        let first = lastPath == nil
        guard signature != lastPath else { return }
        lastPath = signature
        guard !first, server != nil else { return }
        pathDebounce?.cancel()
        pathDebounce = Task { [weak self] in
            try? await Task.sleep(for: .seconds(1.5))
            guard !Task.isCancelled, let self, self.server != nil else { return }
            await self.reconnect()
        }
    }

    // MARK: - Devices

    public static var deviceInfo: Schemas.DeviceInfo {
        #if os(tvOS)
        let platform: Schemas.DeviceInfo.PlatformPayload = .tvos
        let name = UIDevice.current.name
        #elseif os(iOS)
        let platform: Schemas.DeviceInfo.PlatformPayload = UIDevice.current.userInterfaceIdiom == .pad ? .ipados : .ios
        let name = UIDevice.current.name
        #else
        let platform: Schemas.DeviceInfo.PlatformPayload = .other
        let name = Host.current().localizedName ?? "Mac"
        #endif
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String
        return .init(clientId: ServerStore.clientID, name: name, platform: platform, product: "Marquee", version: version)
    }

    // MARK: - Connecting

    private func makeClient(_ base: URL, timeout: TimeInterval = 20) -> Client {
        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = timeout
        config.waitsForConnectivity = false
        return Client(serverURL: base.appending(path: "api/v1"),
                      configuration: .init(dateTranscoder: FlexibleDateTranscoder()),
                      transport: URLSessionTransport(configuration: .init(session: URLSession(configuration: config))),
                      middlewares: [AuthMiddleware(token: { [box = tokenBox] in box.token })])
    }

    /// Fetches /system/info from a URL with a short timeout.
    nonisolated static func probe(_ base: URL, timeout: TimeInterval = 2.5) async -> Schemas.SystemInfo? {
        var req = URLRequest(url: base.appending(path: "api/v1/system/info"))
        req.timeoutInterval = timeout
        guard let (data, resp) = try? await URLSession.shared.data(for: req),
              (resp as? HTTPURLResponse)?.statusCode == 200 else { return nil }
        let dec = JSONDecoder()
        return try? dec.decode(Schemas.SystemInfo.self, from: data)
    }

    /// Adds a server by address (LAN or Tailscale) and makes it current.
    public func addServer(lan: String, remote: String) async throws {
        func parse(_ s: String) -> URL? {
            let t = s.trimmingCharacters(in: .whitespaces)
            guard !t.isEmpty else { return nil }
            var withScheme = t.contains("://") ? t : "http://\(t)"
            if URL(string: withScheme)?.port == nil, !t.contains(":") { withScheme += ":32500" }
            return URL(string: withScheme)
        }
        let lanURL = parse(lan), remoteURL = parse(remote)
        for url in [lanURL, remoteURL].compactMap({ $0 }) {
            if let info = await Self.probe(url, timeout: 6) {
                var record = ServerStore.servers.first(where: { $0.id == info.serverId }) ?? ServerRecord(id: info.serverId, name: info.serverName, lanURL: nil, remoteURL: nil)
                record.name = info.serverName
                if let lanURL { record.lanURL = lanURL }
                if let remoteURL { record.remoteURL = remoteURL }
                if info.networkClass == .remote, record.remoteURL == nil { record.remoteURL = url }
                if server?.id != record.id, me != nil { onUserChange?() }
                use(record)
                await reconnect()
                return
            }
        }
        throw MarqueeError("Couldn't reach a Marquee server at that address.")
    }

    /// Adds a server found with Bonjour.
    public func addDiscovered(_ found: DiscoveredServer) async throws {
        try await addServer(lan: found.url.absoluteString, remote: "")
    }

    private func use(_ record: ServerRecord) {
        ServerStore.save(record)
        ServerStore.currentServerID = record.id
        server = record
        token = ServerStore.token(for: record.id)
    }

    /// Picks the best address for the current network and checks the session. Calls while
    /// one is already running wait for it rather than starting another.
    public func reconnect() async {
        if let running = reconnectTask { await running.value; return }
        let task = Task { await performReconnect() }
        reconnectTask = task
        await task.value
        reconnectTask = nil
    }

    private func performReconnect() async {
        guard var record = server else { state = .noServer; return }
        // Only the first connection shows "Connecting…"; once there's a client the app stays
        // up (a playing video, where you are) while the address is re-checked.
        if client == nil { state = .connecting }
        isReconnecting = true
        defer { isReconnecting = false }
        var chosen: (URL, Schemas.SystemInfo)?
        for url in record.candidates {
            if let info = await Self.probe(url) { chosen = (url, info); break }
        }
        guard let (url, info) = chosen, server?.id == record.id else {
            if server?.id == record.id {
                lastError = "Can't reach \(record.name). Check that you're on the home network or connected to Tailscale."
                state = token == nil ? .signedOut : .signedIn
            }
            return
        }
        // Learn the remote address the server advertises (Settings → Remote Access).
        if record.remoteURL == nil, let r = info.remoteUrl, let u = URL(string: r) {
            record.remoteURL = u
            ServerStore.save(record)
            server = record
        }
        lastError = nil
        baseURL = url
        self.info = info
        client = makeClient(url)
        longPollClient = makeClient(url, timeout: 45) // the server waits up to 25 s
        guard token != nil else { state = .signedOut; return }
        do {
            me = try await client!.getMe().ok.body.json
            state = .signedIn
        } catch {
            if case .unauthorized = (try? await client!.getMe()) { signOutLocally() } else { state = .signedIn }
        }
    }

    // MARK: - Signing in

    private func signedIn(_ auth: Schemas.AuthResult) {
        if let old = me?.id, old != auth.user.id { onUserChange?() }
        token = auth.token
        if let id = server?.id { ServerStore.setToken(auth.token, for: id) }
        me = auth.user
        state = .signedIn
        // The image key only comes with /me.
        Task { await refreshMe() }
    }

    public func signIn(username: String, password: String, totpCode: String? = nil) async throws {
        guard let client else { throw MarqueeError("Not connected") }
        switch try await client.login(body: .json(.init(username: username, password: password, totpCode: totpCode, device: Self.deviceInfo))) {
        case .ok(let ok): signedIn(try ok.body.json)
        case .unauthorized(let r):
            switch try? r.body.json.code {
            case "totp_required": throw TwoFactorRequired()
            case "invalid_totp": throw MarqueeError("That code didn't work. Try the newest one.")
            default: throw MarqueeError("Incorrect username or password.")
            }
        case .tooManyRequests(let r): throw MarqueeError((try? r.body.json.message) ?? "Too many attempts. Wait a minute.")
        default: throw MarqueeError("Sign-in failed.")
        }
    }

    public func profiles() async throws -> [Schemas.Profile] {
        guard let client else { return [] }
        return try await client.listSignInProfiles().ok.body.json
    }

    /// PIN sign-in from the "Who's watching?" picker.
    public func pinSignIn(userID: Int64, pin: String?, password: String? = nil, totpCode: String? = nil) async throws {
        guard let client else { throw MarqueeError("Not connected") }
        switch try await client.pinLogin(body: .json(.init(userId: userID, pin: pin, password: password, totpCode: totpCode, device: Self.deviceInfo))) {
        case .ok(let ok): signedIn(try ok.body.json)
        case .unauthorized(let r):
            if (try? r.body.json.code) == "totp_required" { throw TwoFactorRequired() }
            throw MarqueeError((try? r.body.json.message) ?? "Incorrect PIN.")
        case .forbidden(let r): throw MarqueeError((try? r.body.json.message) ?? "PIN sign-in isn't allowed here.")
        case .tooManyRequests(let r): throw MarqueeError((try? r.body.json.message) ?? "Too many attempts.")
        default: throw MarqueeError("Sign-in failed.")
        }
    }

    /// Quick Connect: returns the code to show; call `pollQuickConnect` until it succeeds.
    public func startQuickConnect() async throws -> Schemas.QuickConnectStart {
        guard let client else { throw MarqueeError("Not connected") }
        return try await client.startQuickConnect(body: .json(.init(device: Self.deviceInfo))).ok.body.json
    }

    /// Returns true once approved (and signs in), false while pending; throws when expired.
    public func pollQuickConnect(secret: String) async throws -> Bool {
        guard let client else { throw MarqueeError("Not connected") }
        let st = try await client.pollQuickConnect(path: .init(secret: secret)).ok.body.json
        switch st.status {
        case .approved:
            if let auth = st.auth { signedIn(auth); return true }
            throw MarqueeError("Approval failed.")
        case .pending: return false
        case .expired: throw MarqueeError("The code expired.")
        }
    }

    /// Switches to another profile on this device (PIN where needed).
    public func switchProfile(to userID: Int64, pin: String?, password: String?) async throws {
        guard let client else { return }
        switch try await client.switchProfile(path: .init(userId: userID), body: .json(.init(pin: pin, password: password, device: Self.deviceInfo))) {
        case .ok(let ok): signedIn(try ok.body.json)
        case .unauthorized(let r): throw MarqueeError((try? r.body.json.message) ?? "Incorrect PIN.")
        case .tooManyRequests(let r): throw MarqueeError((try? r.body.json.message) ?? "Too many attempts.")
        default: throw MarqueeError("Couldn't switch profile.")
        }
    }

    public func signOut() async {
        _ = try? await client?.logout()
        signOutLocally()
    }

    private func signOutLocally() {
        onUserChange?()
        token = nil
        me = nil
        if let id = server?.id { ServerStore.setToken(nil, for: id) }
        state = server == nil ? .noServer : .signedOut
    }

    public func forgetServer() {
        onUserChange?()
        if let id = server?.id { ServerStore.forget(id) }
        server = nil
        client = nil
        longPollClient = nil
        token = nil
        me = nil
        state = .noServer
    }

    public func refreshMe() async {
        if let me = try? await client?.getMe().ok.body.json { self.me = me }
    }

    // MARK: - URLs

    /// How image, person photo and avatar URLs authenticate: the image key from /me (D85),
    /// which grants those images and nothing else, or the token until the key is known.
    private var imageAuth: URLQueryItem {
        if let key = me?.imageKey, !key.isEmpty { return .init(name: "key", value: key) }
        return .init(name: "token", value: token)
    }

    /// Artwork URL usable directly (images authenticate with the image key).
    public func imageURL(_ artworkID: Int64?, width: Int) -> URL? {
        guard let artworkID, let baseURL else { return nil }
        #if canImport(UIKit)
        let scale = Int(UITraitCollection.current.displayScale.rounded())
        #else
        let scale = 2
        #endif
        var c = URLComponents(url: baseURL.appending(path: "api/v1/images/\(artworkID)"), resolvingAgainstBaseURL: false)!
        c.queryItems = [.init(name: "w", value: String(width * max(scale, 1))), imageAuth]
        return c.url
    }

    public func personPhotoURL(_ personID: Int64, width: Int) -> URL? {
        guard let baseURL else { return nil }
        var c = URLComponents(url: baseURL.appending(path: "api/v1/people/\(personID)/photo"), resolvingAgainstBaseURL: false)!
        c.queryItems = [.init(name: "w", value: String(width * 2)), imageAuth]
        return c.url
    }

    /// Resolves a server-relative URL (avatars, stream URLs) against the current address.
    public func absolute(_ path: String?) -> URL? {
        guard let path, let baseURL else { return nil }
        if let u = URL(string: path), u.scheme != nil { return u }
        var c = URLComponents(url: baseURL, resolvingAgainstBaseURL: false)!
        let parts = path.split(separator: "?", maxSplits: 1)
        c.path = String(parts[0])
        var items = parts.count > 1 ? (URLComponents(string: "?" + parts[1])?.queryItems ?? []) : []
        if path.hasPrefix("/api/v1/users/") { items.append(imageAuth) } // avatars
        c.queryItems = items.isEmpty ? nil : items
        return c.url
    }

    /// A server URL that authenticates with the token parameter (downloads, which run in
    /// the background where the API middleware doesn't apply).
    public func authorizedURL(_ path: String) -> URL? {
        guard let u = absolute(path), var c = URLComponents(url: u, resolvingAgainstBaseURL: false) else { return nil }
        c.queryItems = (c.queryItems ?? []) + [.init(name: "token", value: token)]
        return c.url
    }

    public var isRemote: Bool { info?.networkClass == .remote }
}
