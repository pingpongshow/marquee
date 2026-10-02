import CarPlay
import MarqueeKit
import OSLog
import UIKit

private let log = Logger(subsystem: "app.marquee", category: "carplay")

/// The app's shared state, for scenes SwiftUI doesn't create (CarPlay).
@MainActor
enum Shared {
    static var app: AppSession!
    static var music: MusicPlayer!
    static var downloads: Downloads?
}

/// CarPlay (MUSIC-14): mixes, stations and recently played albums, the music library and
/// downloads, with the system Now Playing screen.
@MainActor
final class CarPlaySceneDelegate: UIResponder, @preconcurrency CPTemplateApplicationSceneDelegate {
    private var ui: CPInterfaceController?
    private var musicLibrary: Int64?
    private var forYouTemplate: CPListTemplate?
    private var downloadsTemplate: CPListTemplate?
    /// Whether For You was last loaded with a connection, and which downloads were listed.
    private var loadedConnected: Bool?
    private var listedDownloads: [Int64]?
    /// Bumped per connection, so an observation left from an earlier one stops.
    private var watchGeneration = 0

    func templateApplicationScene(_ scene: CPTemplateApplicationScene, didConnect interfaceController: CPInterfaceController) {
        ui = interfaceController
        log.info("CarPlay connected")
        let forYou = CPListTemplate(title: "For You", sections: [])
        forYou.tabImage = UIImage(systemName: "sparkles")
        let library = CPListTemplate(title: "Library", sections: [librarySection()])
        library.tabImage = UIImage(systemName: "music.note.list")
        let downloads = CPListTemplate(title: "Downloads", sections: [])
        downloads.tabImage = UIImage(systemName: "arrow.down.circle")
        interfaceController.setRootTemplate(CPTabBarTemplate(templates: [forYou, library, downloads]), animated: false, completion: nil)
        forYouTemplate = forYou
        downloadsTemplate = downloads
        loadedConnected = nil
        listedDownloads = nil
        watchGeneration += 1
        refresh(watchGeneration)
    }

    func templateApplicationScene(_ scene: CPTemplateApplicationScene, didDisconnectInterfaceController interfaceController: CPInterfaceController) {
        ui = nil
        forYouTemplate = nil
        downloadsTemplate = nil
    }

    /// The car often connects before the app has reached the server: For You loads again once
    /// it's connected (or lost), and Downloads follows what's on the device.
    private func refresh(_ generation: Int) {
        guard generation == watchGeneration, ui != nil, let forYou = forYouTemplate, let downloads = downloadsTemplate else { return }
        let connected = app.client != nil && app.state == .signedIn
        if connected != loadedConnected {
            loadedConnected = connected
            Task { await loadForYou(forYou) }
        }
        let done = downloadedTracks().map(\.id)
        if done != listedDownloads {
            listedDownloads = done
            downloads.updateSections(downloadSections())
        }
        withObservationTracking {
            _ = app.client
            _ = app.state
            _ = Shared.downloads?.entries
        } onChange: { [weak self] in
            Task { @MainActor in self?.refresh(generation) }
        }
    }

    private var app: AppSession { Shared.app }
    private var music: MusicPlayer { Shared.music }

    private func libraryID() async -> Int64? {
        if let musicLibrary { return musicLibrary }
        musicLibrary = try? await app.libraries().first { $0._type == .music }?.id
        return musicLibrary
    }

    // MARK: - For You

    private func loadForYou(_ t: CPListTemplate) async {
        guard app.client != nil, let lib = await libraryID() else {
            t.updateSections([CPListSection(items: [CPListItem(text: "Can't reach your server", detailText: "Downloads still play")])])
            return
        }
        var sections: [CPListSection] = []
        if let mixes = try? await app.mixes(library: lib), !mixes.isEmpty {
            sections.append(CPListSection(items: mixes.map { m in
                let item = CPListItem(text: m.title, detailText: m.description, image: UIImage(systemName: "dot.radiowaves.left.and.right"))
                item.handler = { [weak self] _, done in
                    self?.music.playStation(m)
                    self?.showNowPlaying()
                    done()
                }
                return item
            }, header: "Mixes for You", sectionIndexTitle: nil))
        }
        var stations: [CPListItem] = [
            station("Library Radio", "dot.radiowaves.left.and.right", .init(seed: .library, libraryId: lib)),
            station("Favourites Radio", "heart", .init(seed: .favourites, libraryId: lib)),
        ]
        stations += musicMoods.map { station($0, "waveform", .init(seed: .mood, value: $0.lowercased(), libraryId: lib)) }
        sections.append(CPListSection(items: stations, header: "Stations", sectionIndexTitle: nil))
        if let hubs = try? await app.hubs(), let played = hubs.first(where: { $0.id == "played-\(lib)" }) {
            sections.append(CPListSection(items: played.items.prefix(12).map(containerItem), header: "Recently Played", sectionIndexTitle: nil))
        }
        t.updateSections(sections)
        log.info("For You loaded: \(sections.map { "\($0.header ?? "") \($0.items.count)" }.joined(separator: ", "), privacy: .public)")
    }

    private func station(_ title: String, _ icon: String, _ req: RadioRequest) -> CPListItem {
        let item = CPListItem(text: title, detailText: nil, image: UIImage(systemName: icon))
        item.handler = { [weak self] _, done in
            guard let self else { done(); return }
            Task {
                var r = req
                r.limit = 50
                if let st = try? await self.app.radio(r) {
                    self.music.playStation(st, radio: r)
                    self.showNowPlaying()
                }
                done()
            }
        }
        return item
    }

    // MARK: - Library

    private func librarySection() -> CPListSection {
        let rows: [(String, String, Schemas.ItemType)] = [("Artists", "music.mic", .artist), ("Albums", "square.stack", .album)]
        var items = rows.map { title, icon, type in
            let item = CPListItem(text: title, detailText: nil, image: UIImage(systemName: icon))
            item.accessoryType = .disclosureIndicator
            item.handler = { [weak self] _, done in
                Task {
                    await self?.pushList(title: title, type: type)
                    done()
                }
            }
            return item
        }
        let playlists = CPListItem(text: "Playlists", detailText: nil, image: UIImage(systemName: "music.note.list"))
        playlists.accessoryType = .disclosureIndicator
        playlists.handler = { [weak self] _, done in
            Task {
                await self?.pushPlaylists()
                done()
            }
        }
        items.append(playlists)
        return CPListSection(items: items)
    }

    private func pushList(title: String, type: Schemas.ItemType) async {
        guard let lib = await libraryID(), let page = try? await app.items(library: lib, sort: .title, limit: CPListTemplate.maximumItemCount, type: type) else { return }
        let t = CPListTemplate(title: title, sections: [CPListSection(items: page.items.map(containerItem))])
        ui?.pushTemplate(t, animated: true, completion: nil)
    }

    private func pushPlaylists() async {
        guard let lists = try? await app.playlists(kind: .audio) else { return }
        let items = lists.map { p in
            let item = CPListItem(text: p.title, detailText: "\(p.itemCount) tracks")
            item.accessoryType = .disclosureIndicator
            item.handler = { [weak self] _, done in
                Task {
                    guard let self, let entries = try? await self.app.playlistItems(p.id) else { done(); return }
                    self.pushTracks(title: p.title, tracks: entries.map(\.item))
                    done()
                }
            }
            return item
        }
        ui?.pushTemplate(CPListTemplate(title: "Playlists", sections: [CPListSection(items: items)]), animated: true, completion: nil)
    }

    /// An artist opens its albums; an album opens its tracks.
    private func containerItem(_ it: Item) -> CPListItem {
        let item = CPListItem(text: it.title, detailText: it._type == .album ? (it.artistCredit ?? it.grandparentTitle) : nil)
        item.accessoryType = .disclosureIndicator
        loadImage(it, into: item)
        item.handler = { [weak self] _, done in
            Task {
                guard let self else { done(); return }
                if it._type == .artist, let albums = try? await self.app.children(it.id) {
                    let t = CPListTemplate(title: it.title, sections: [CPListSection(items: albums.map(self.containerItem))])
                    self.ui?.pushTemplate(t, animated: true, completion: nil)
                } else if let tracks = try? await self.app.leaves(it.id) {
                    self.pushTracks(title: it.title, tracks: tracks)
                }
                done()
            }
        }
        return item
    }

    private func pushTracks(title: String, tracks: [Item]) {
        let play = CPListItem(text: "Play", detailText: nil, image: UIImage(systemName: "play.fill"))
        play.handler = { [weak self] _, done in
            self?.music.play(tracks, source: title)
            self?.showNowPlaying()
            done()
        }
        let shuffle = CPListItem(text: "Shuffle", detailText: nil, image: UIImage(systemName: "shuffle"))
        shuffle.handler = { [weak self] _, done in
            self?.music.play(tracks, shuffle: true, source: title)
            self?.showNowPlaying()
            done()
        }
        let rows = tracks.prefix(CPListTemplate.maximumItemCount - 2).enumerated().map { i, t in
            let item = CPListItem(text: t.title, detailText: t.artistCredit ?? t.grandparentTitle)
            item.handler = { [weak self] _, done in
                self?.music.play(tracks, start: i, source: title)
                self?.showNowPlaying()
                done()
            }
            return item
        }
        let t = CPListTemplate(title: title, sections: [CPListSection(items: [play, shuffle]), CPListSection(items: Array(rows))])
        ui?.pushTemplate(t, animated: true, completion: nil)
    }

    // MARK: - Downloads

    private func downloadedTracks() -> [Item] {
        guard let dl = Shared.downloads else { return [] }
        return dl.entries.values.filter { $0.state == .done && $0.item._type == .track }.map(\.item)
            .sorted { ($0.grandparentTitle ?? "", $0.parentTitle ?? "", $0.index ?? 0) < ($1.grandparentTitle ?? "", $1.parentTitle ?? "", $1.index ?? 0) }
    }

    private func downloadSections() -> [CPListSection] {
        let tracks = downloadedTracks()
        guard !tracks.isEmpty else {
            return [CPListSection(items: [CPListItem(text: "No downloaded music", detailText: "Download albums in the app to play them offline")])]
        }
        let shuffle = CPListItem(text: "Shuffle Downloads", detailText: "\(tracks.count) tracks", image: UIImage(systemName: "shuffle"))
        shuffle.handler = { [weak self] _, done in
            self?.music.play(tracks, shuffle: true, source: "Downloads")
            self?.showNowPlaying()
            done()
        }
        let rows = tracks.prefix(CPListTemplate.maximumItemCount - 1).enumerated().map { i, t in
            let item = CPListItem(text: t.title, detailText: [t.artistCredit ?? t.grandparentTitle, t.parentTitle].compactMap { $0 }.joined(separator: " · "))
            item.handler = { [weak self] _, done in
                self?.music.play(tracks, start: i, source: "Downloads")
                self?.showNowPlaying()
                done()
            }
            return item
        }
        return [CPListSection(items: [shuffle]), CPListSection(items: Array(rows))]
    }

    // MARK: - Helpers

    private func showNowPlaying() {
        guard let ui, ui.topTemplate !== CPNowPlayingTemplate.shared else { return }
        ui.pushTemplate(CPNowPlayingTemplate.shared, animated: true, completion: nil)
    }

    private func loadImage(_ it: Item, into item: CPListItem) {
        guard let url = app.imageURL(it.images?.poster, width: 120) else { return }
        Task {
            guard let (data, _) = try? await URLSession.shared.data(from: url), let img = UIImage(data: data) else { return }
            item.setImage(img)
        }
    }
}
