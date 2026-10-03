import Foundation
import Security

/// A Marquee server this device knows: its addresses and the signed-in token.
public struct ServerRecord: Codable, Hashable, Identifiable, Sendable {
    public var id: String           // serverId from /system/info
    public var name: String
    /// Home-network address, tried first (e.g. http://10.1.1.10:32500).
    public var lanURL: URL?
    /// Away-from-home address over Tailscale (e.g. http://host.tailnet.ts.net:32500).
    public var remoteURL: URL?

    public init(id: String, name: String, lanURL: URL?, remoteURL: URL?) {
        self.id = id
        self.name = name
        self.lanURL = lanURL
        self.remoteURL = remoteURL
    }

    /// Addresses in the order to try them: LAN first, Tailscale only when that fails (WAN-3).
    public var candidates: [URL] { [lanURL, remoteURL].compactMap { $0 } }
}

/// Remembers servers in UserDefaults and their tokens in the Keychain.
public enum ServerStore {
    private static let serversKey = "marquee.servers"
    private static let currentKey = "marquee.currentServer"
    private static let defaults = UserDefaults.standard

    public static var servers: [ServerRecord] {
        get {
            guard let data = defaults.data(forKey: serversKey) else { return [] }
            return (try? JSONDecoder().decode([ServerRecord].self, from: data)) ?? []
        }
        set { defaults.set(try? JSONEncoder().encode(newValue), forKey: serversKey) }
    }

    public static var currentServerID: String? {
        get { defaults.string(forKey: currentKey) }
        set { defaults.set(newValue, forKey: currentKey) }
    }

    public static func save(_ server: ServerRecord) {
        var list = servers.filter { $0.id != server.id }
        list.insert(server, at: 0)
        servers = list
    }

    public static func forget(_ id: String) {
        servers = servers.filter { $0.id != id }
        Keychain.delete(account: "token.\(id)")
        Keychain.delete(account: "imageKey.\(id)")
        if currentServerID == id { currentServerID = nil }
    }

    public static func token(for id: String) -> String? { Keychain.read(account: "token.\(id)") }
    public static func setToken(_ token: String?, for id: String) {
        if let token { Keychain.write(token, account: "token.\(id)") } else { Keychain.delete(account: "token.\(id)") }
    }

    /// The image key (D85) last seen for a server, so image URLs never need the token, even
    /// before /me answers or while the server can't be reached.
    public static func imageKey(for id: String) -> String? { Keychain.read(account: "imageKey.\(id)") }
    public static func setImageKey(_ key: String?, for id: String) {
        if let key, !key.isEmpty { Keychain.write(key, account: "imageKey.\(id)") } else { Keychain.delete(account: "imageKey.\(id)") }
    }

    /// A stable identifier for this install, so the server lists it as one device.
    public static var clientID: String {
        if let id = Keychain.read(account: "clientId") { return id }
        let id = UUID().uuidString.lowercased()
        Keychain.write(id, account: "clientId")
        return id
    }
}

enum Keychain {
    private static let service = "app.marquee"

    static func read(account: String) -> String? {
        let q: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service,
                                kSecAttrAccount as String: account, kSecReturnData as String: true]
        var out: AnyObject?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess, let data = out as? Data else { return nil }
        return String(data: data, encoding: .utf8)
    }

    static func write(_ value: String, account: String) {
        delete(account: account)
        let q: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service,
                                kSecAttrAccount as String: account, kSecValueData as String: Data(value.utf8),
                                kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlock]
        SecItemAdd(q as CFDictionary, nil)
    }

    static func delete(account: String) {
        let q: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service,
                                kSecAttrAccount as String: account]
        SecItemDelete(q as CFDictionary)
    }
}
