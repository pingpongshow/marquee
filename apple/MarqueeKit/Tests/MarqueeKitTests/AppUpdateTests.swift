import Foundation
import Testing
@testable import MarqueeKit

struct AppUpdateTests {
    @Test func comparesVersionsByNumber() {
        #expect(AppUpdate.isNewer("1.9.0", than: "1.8.0"))
        #expect(AppUpdate.isNewer("v1.10.0", than: "1.9.0"))
        #expect(AppUpdate.isNewer("2.0", than: "1.9.9"))
        #expect(AppUpdate.isNewer("1.8.1", than: "1.8"))
        #expect(!AppUpdate.isNewer("1.8.0", than: "1.8.0"))
        #expect(!AppUpdate.isNewer("1.8", than: "1.8.0"))
        #expect(!AppUpdate.isNewer("1.7.9", than: "1.8.0"))
        #expect(!AppUpdate.isNewer("nightly", than: "1.8.0"))
        #expect(!AppUpdate.isNewer("1.9.0-beta", than: "1.8.0"))
        #expect(!AppUpdate.isNewer("1.9.0", than: "unknown"))
    }

    private func release(tag: String = "v1.9.0", draft: Bool = false, prerelease: Bool = false,
                         url: String = "https://github.com/pingpongshow/marquee/releases/tag/v1.9.0",
                         assets: [String] = ["Marquee-macOS-1.9.0.dmg", "Marquee-1.9.0.apk"]) -> String {
        let names = assets.map { "{\"name\":\"\($0)\"}" }.joined(separator: ",")
        return "{\"tag_name\":\"\(tag)\",\"html_url\":\"\(url)\",\"draft\":\(draft),\"prerelease\":\(prerelease),\"assets\":[\(names)],\"body\":\"notes\"}"
    }

    private func found(_ releases: String..., current: String = "1.8.0") -> String? {
        AppUpdate.from(releases: Data("[\(releases.joined(separator: ","))]".utf8), current: current)?.version
    }

    @Test func offersTheNewestPublishedReleaseWithAMacFile() {
        #expect(found(release()) == "1.9.0")
        #expect(found(release(), current: "1.9.0") == nil)
        #expect(found(release(assets: ["Marquee-1.9.0.apk"])) == nil)
        #expect(found() == nil)
        #expect(AppUpdate.from(releases: Data("{\"message\":\"rate limited\"}".utf8), current: "1.8.0") == nil)
    }

    @Test func aNewerReleaseWithoutOneDoesntHideIt() {
        let serverOnly = release(tag: "v1.9.1", assets: ["Marquee-1.9.1.apk"])
        #expect(found(serverOnly, release()) == "1.9.0")
        #expect(found(release(tag: "v2.0.0", draft: true), release()) == "1.9.0")
        #expect(found(release(tag: "v2.0.0", prerelease: true), release()) == "1.9.0")
    }

    @Test func onlyOpensThisProjectsPages() {
        for bad in ["file:///Applications/Evil.app", "https://example.com/r", "marquee://x",
                    "https://github.com/someone/else/releases/tag/v1.9.0", "http://github.com/pingpongshow/marquee/releases"] {
            #expect(found(release(url: bad)) == nil, "\(bad)")
        }
    }
}
