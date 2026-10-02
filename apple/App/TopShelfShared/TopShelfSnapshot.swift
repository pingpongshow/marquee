import Foundation

/// What the Apple TV Top Shelf shows, written by the app when Home loads and read by the
/// Top Shelf extension (shared through the app group).
struct TopShelfSnapshot: Codable {
    struct Item: Codable {
        let id: Int64
        let title: String
        let subtitle: String?
        let imageURL: URL?
        let wide: Bool      // 16:9 artwork (episodes in progress) rather than a poster
        let playable: Bool  // a movie or episode: "play" starts it
    }
    struct Section: Codable {
        let title: String
        let items: [Item]
    }
    var sections: [Section]

    static let appGroup = "group.app.marquee"
    private static var url: URL? {
        FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: appGroup)?.appending(path: "topshelf.json")
    }

    static func load() -> TopShelfSnapshot? {
        guard let url, let data = try? Data(contentsOf: url) else { return nil }
        return try? JSONDecoder().decode(TopShelfSnapshot.self, from: data)
    }

    func save() {
        guard let url = Self.url, let data = try? JSONEncoder().encode(self) else { return }
        try? data.write(to: url, options: .atomic)
    }
}
