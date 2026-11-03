import Foundation

/// A newer version of the app than this one, from the project's GitHub releases.
public struct AppUpdate: Equatable, Sendable {
    public let version: String
    /// The release's page, to download it from.
    public let url: URL

    public init(version: String, url: URL) {
        self.version = version
        self.url = url
    }

    /// The newest releases, newest first. Not /releases/latest: that's only the newest, which may
    /// be a server-only release, or one whose files are still uploading.
    public static let releasesURL = URL(string: "https://api.github.com/repos/pingpongshow/marquee/releases?per_page=30")!

    /// Only ever this project's own pages: what comes back gets opened.
    private static let pagePrefix = "https://github.com/pingpongshow/marquee/"

    /// Whether `latest` is a later version than `current`, by number: 1.10.0 is after 1.9.0, and
    /// 1.8 is the same as 1.8.0. A version that isn't numbers is never newer.
    public static func isNewer(_ latest: String, than current: String) -> Bool {
        func parts(_ v: String) -> [Int]? {
            let s = v.hasPrefix("v") ? String(v.dropFirst()) : v
            let p = s.split(separator: ".", omittingEmptySubsequences: false).map { Int($0) }
            return p.isEmpty || p.contains(nil) ? nil : p.compactMap { $0 }
        }
        guard let a = parts(latest), let b = parts(current) else { return false }
        for i in 0..<max(a.count, b.count) {
            let x = i < a.count ? a[i] : 0, y = i < b.count ? b[i] : 0
            if x != y { return x > y }
        }
        return false
    }

    /// The update GitHub's list of releases (newest first) offers this app, if any: the newest
    /// published release with a file for this app, when it's later than `current`.
    public static func from(releases json: Data, current: String, assetPrefix: String = "Marquee-macOS-") -> AppUpdate? {
        struct Release: Decodable {
            struct Asset: Decodable { let name: String }
            let tag_name: String
            let html_url: String
            let draft: Bool?
            let prerelease: Bool?
            let assets: [Asset]?
        }
        guard let list = try? JSONDecoder().decode([Release].self, from: json),
              let r = list.first(where: { r in
                  r.draft != true && r.prerelease != true && r.assets?.contains { $0.name.hasPrefix(assetPrefix) } == true
              }),
              isNewer(r.tag_name, than: current),
              r.html_url.hasPrefix(pagePrefix), let url = URL(string: r.html_url) else { return nil }
        return AppUpdate(version: r.tag_name.hasPrefix("v") ? String(r.tag_name.dropFirst()) : r.tag_name, url: url)
    }
}
