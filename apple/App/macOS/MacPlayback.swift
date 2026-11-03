import MarqueeKit
import SwiftUI

/// What outlives the windows: the session, the music, downloads, where the sound goes.
@MainActor @Observable
final class MacPlayback {
    static let shared = MacPlayback()

    let app: AppSession
    let music: MusicPlayer
    let downloads: Downloads
    let smart: SmartDownloads
    let sonos: SonosSession
    let outputs: MacAudioOutputs
    let video = VideoPresenter()

    private init() {
        // Artwork responses are immutable, so a large cache makes browsing instant.
        URLCache.shared = URLCache(memoryCapacity: 128 << 20, diskCapacity: 2 << 30)
        let args = ProcessInfo.processInfo.arguments
        // UI tests start from a clean slate.
        if args.contains("-marquee-reset") {
            for s in ServerStore.servers { ServerStore.forget(s.id) }
            for key in ["marquee.showAudioQuality", "marquee.dj", "marquee.downloads.songQuality", "marquee.downloads.videoQuality",
                        MacAudioOutputs.chosenKey, MacAudioOutputs.chosenNameKey, MacPlayback.volumeKey] {
                UserDefaults.standard.removeObject(forKey: key)
            }
            for key in UserDefaults.standard.dictionaryRepresentation().keys where key.hasPrefix("marquee.vod.") {
                UserDefaults.standard.removeObject(forKey: key)
            }
        }
        let session = AppSession()
        let player = MusicPlayer(app: session)
        let dl = Downloads()
        player.downloads = dl
        app = session
        music = player
        downloads = dl
        smart = SmartDownloads(downloads: dl)
        #if DEBUG
        // Tests never reach a real speaker: a room that only exists in this process.
        let api: any SpeakerAPI = args.contains("-marquee-test-speakers") ? SimulatedSpeakers() : ServerSpeakerAPI(app: session)
        #else
        let api: any SpeakerAPI = ServerSpeakerAPI(app: session)
        #endif
        sonos = SonosSession(api: api, host: player)
        outputs = MacAudioOutputs(music: player)
        // The saved level, or a test's: tests stay quiet.
        if let i = args.firstIndex(of: "-marquee-test-volume"), args.indices.contains(i + 1), let v = Float(args[i + 1]) {
            player.appVolume = v
        } else if UserDefaults.standard.object(forKey: Self.volumeKey) != nil {
            player.appVolume = UserDefaults.standard.float(forKey: Self.volumeKey)
        }
        player.mediaCommandOverride = { command in
            guard let p = MacVideo.shared.activePlayer else { return false }
            switch command {
            case .play: p.play()
            case .pause: p.pause()
            case .toggle: p.timeControlStatus == .paused ? p.play() : p.pause()
            case .seek(let at): p.seek(to: .init(seconds: at, preferredTimescale: 600))
            case .next, .previous: break // a video has no next here
            }
            return true
        }
    }

    static let volumeKey = "marquee.mac.volume"

    /// Debug builds can be driven from a folder (MacTestDriver).
    func startTestDriver() {
        #if DEBUG
        MacTestDriver.startIfAsked()
        #endif
    }

    /// This Mac's level for Marquee, or the room's while playing on Sonos.
    func setVolume(_ v: Float) {
        music.appVolume = min(1, max(0, v))
        UserDefaults.standard.set(music.appVolume, forKey: Self.volumeKey)
        MacVideo.shared.applyVolume(music.appVolume)
    }

    // MARK: Transport for menus (a video first, then the music)

    var isPlayingAnything: Bool {
        if let p = MacVideo.shared.activePlayer { return p.timeControlStatus != .paused }
        return music.playing
    }

    func togglePlayPause() {
        if let p = MacVideo.shared.activePlayer {
            p.timeControlStatus == .paused ? p.play() : p.pause()
        } else {
            music.toggle()
        }
    }

    func next() { if MacVideo.shared.activePlayer == nil { music.next() } }
    func previous() { if MacVideo.shared.activePlayer == nil { music.previous() } }
}

extension View {
    /// The objects every shared view reads, for any window.
    func macEnvironment() -> some View {
        let p = MacPlayback.shared
        return environment(p.app)
            .environment(p.music)
            .environment(p.downloads)
            .environment(p.smart)
            .environment(p.video)
    }
}
