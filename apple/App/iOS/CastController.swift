import GoogleCast
import MarqueeKit
import SwiftUI

/// Chromecast (D82): TVs, Google TV, Nest speakers and speaker groups play Marquee through the
/// Default Media Receiver. A device is picked with the Cast button; the server makes a stream
/// the receiver plays (AppleDeviceProfile.chromecast) and the receiver fetches it directly on
/// the home network (the session id in the URL is its credential). The receiver's position is
/// reported, so resume, play counts and scrobbles work. Music hands its queue over track by
/// track; video hands over at the current position. When casting stops, playback carries on
/// on the phone.
@MainActor @Observable
final class CastController: NSObject {
    static let shared = CastController()

    private(set) var device: String?
    private(set) var playing = false
    private(set) var position: Double = 0
    private(set) var duration: Double = 0

    @ObservationIgnored var app: AppSession?
    @ObservationIgnored weak var music: MusicPlayer?
    /// Set while the video player is open: a device connected then is for the video.
    @ObservationIgnored var videoActive = false
    /// The video player: told when a device connects, and when casting ends (with the position).
    @ObservationIgnored var onVideoConnected: (() -> Void)?
    @ObservationIgnored var onVideoEnded: ((Double) -> Void)?
    @ObservationIgnored var onVideoFinished: (() -> Void)?

    @ObservationIgnored private var session: PlaybackSession?
    @ObservationIgnored private var castingMusic = false
    @ObservationIgnored private var ticker: Timer?
    @ObservationIgnored private var lastReport = Date.distantPast
    /// Bumped per load, so a slow load for an earlier track can't replace a newer one.
    @ObservationIgnored private var loadGeneration = 0

    static func setUp(app: AppSession, music: MusicPlayer) {
        let options = GCKCastOptions(discoveryCriteria: GCKDiscoveryCriteria(applicationID: kGCKDefaultMediaReceiverApplicationID))
        options.physicalVolumeButtonsWillControlDeviceVolume = true
        GCKCastContext.setSharedInstanceWith(options)
        shared.app = app
        shared.music = music
        GCKCastContext.sharedInstance().sessionManager.add(shared)
    }

    private var remote: GCKRemoteMediaClient? { GCKCastContext.sharedInstance().sessionManager.currentCastSession?.remoteMediaClient }

    func disconnect() { GCKCastContext.sharedInstance().sessionManager.endSessionAndStopCasting(true) }

    // MARK: Connecting

    private func connected(_ s: GCKCastSession) {
        device = s.device.friendlyName ?? "Chromecast"
        s.remoteMediaClient?.add(self)
        startTicker()
        if videoActive {
            onVideoConnected?()
        } else if let music, music.current != nil {
            castingMusic = true
            music.remote = self
        }
    }

    private func ended() {
        let at = position
        device = nil
        playing = false
        ticker?.invalidate()
        report(playing: false)
        closeSession()
        if castingMusic {
            castingMusic = false
            music?.remoteUpdate(playing: false, time: at, duration: duration)
            music?.remote = nil // carries on here
        } else {
            onVideoEnded?(at)
        }
    }

    // MARK: Loading

    /// Plays an item on the connected device. Returns an error message, or nil.
    @discardableResult
    func load(itemID: Int64, at seconds: Double, title: String, subtitle: String?, artwork: URL?, music: Bool,
              fileID: Int64? = nil, audio: Int64? = nil, subtitleStream: Int64? = nil) async -> String? {
        guard let app, remote != nil else { return "Not connected to a Cast device" }
        loadGeneration += 1
        let generation = loadGeneration
        let s: PlaybackSession
        do {
            s = try await app.castSession(itemID: itemID, startMs: Int64(seconds * 1000), fileID: fileID, audio: audio, subtitle: subtitleStream)
        } catch {
            return generation == loadGeneration ? error.localizedDescription : nil
        }
        // Another load started meanwhile (a quick skip): this one's session isn't needed.
        guard generation == loadGeneration else {
            app.stopCastSession(s.id)
            return nil
        }
        guard let remote else {
            app.stopCastSession(s.id)
            return "Not connected to a Cast device"
        }
        guard let url = app.castURL(s.url) else { return "No server address" }
        let meta = GCKMediaMetadata(metadataType: music ? .musicTrack : .movie)
        meta.setString(title, forKey: kGCKMetadataKeyTitle)
        if let subtitle { meta.setString(subtitle, forKey: music ? kGCKMetadataKeyArtist : kGCKMetadataKeySubtitle) }
        if let artwork { meta.addImage(GCKImage(url: artwork, width: 512, height: 512)) }
        let info = GCKMediaInformationBuilder(contentURL: url)
        info.streamType = .buffered
        info.contentType = s.contentType ?? (music ? "audio/mp4" : "video/mp4")
        info.metadata = meta
        info.streamDuration = Double(s.durationMs) / 1000
        if s._protocol == .hls {
            info.hlsSegmentFormat = .FMP4
            info.hlsVideoSegmentFormat = .FMP4
        }
        var active: [NSNumber] = []
        if let sub = s.subtitleUrl, s.subtitleFormat == .vtt, let subURL = app.castURL(sub),
           let track = GCKMediaTrack(identifier: 1, contentIdentifier: subURL.absoluteString, contentType: "text/vtt", type: .text,
                                     textSubtype: .subtitles, name: "Subtitles", languageCode: nil, customData: nil) {
            info.mediaTracks = [track]
            active = [1]
        }
        let request = GCKMediaLoadRequestDataBuilder()
        request.mediaInformation = info.build()
        request.autoplay = true
        request.startTime = Double(s.startMs) / 1000
        if !active.isEmpty { request.activeTrackIDs = active }
        closeSession()
        session = s
        _ = remote.loadMedia(with: request.build())
        return nil
    }

    func toggle() { _ = playing ? remote?.pause() : remote?.play() }

    func seekTo(_ seconds: Double) {
        let o = GCKMediaSeekOptions()
        o.interval = seconds
        remote?.seek(with: o)
    }

    // MARK: Progress

    /// Keeps the position current and reports it every 10 s while playing.
    private func startTicker() {
        ticker?.invalidate()
        ticker = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated {
                guard let self, let r = self.remote else { return }
                self.position = r.approximateStreamPosition()
                if self.castingMusic { self.music?.remoteUpdate(playing: self.playing, time: self.position, duration: self.duration) }
                if self.playing, Date().timeIntervalSince(self.lastReport) > 10 { self.report(playing: true) }
            }
        }
    }

    private func report(playing: Bool, finished: Bool = false) {
        guard let s = session, let app else { return }
        lastReport = Date()
        let ms = finished ? s.durationMs : Int64(position * 1000)
        app.reportCast(s.id, positionMs: ms, playing: playing)
    }

    private func closeSession() {
        guard let s = session else { return }
        session = nil
        app?.stopCastSession(s.id)
    }

    private func statusChanged(_ status: GCKMediaStatus?) {
        guard let status else { return }
        let was = playing
        playing = status.playerState == .playing || status.playerState == .buffering
        position = status.streamPosition
        if let d = status.mediaInformation?.streamDuration, d > 0 { duration = d }
        if was != playing { report(playing: playing) }
        if castingMusic { music?.remoteUpdate(playing: playing, time: position, duration: duration) }
        if status.playerState == .idle, status.idleReason == .finished, session != nil {
            report(playing: false, finished: true)
            closeSession()
            if castingMusic { music?.remoteFinished() } else { onVideoFinished?() }
        }
    }
}

extension CastController: @preconcurrency GCKSessionManagerListener {
    func sessionManager(_ sessionManager: GCKSessionManager, didStart session: GCKCastSession) { connected(session) }
    func sessionManager(_ sessionManager: GCKSessionManager, didResumeCastSession session: GCKCastSession) { connected(session) }
    func sessionManager(_ sessionManager: GCKSessionManager, didEnd session: GCKCastSession, withError error: Error?) { ended() }
    func sessionManager(_ sessionManager: GCKSessionManager, didFailToStart session: GCKCastSession, withError error: Error) { device = nil }
}

extension CastController: @preconcurrency GCKRemoteMediaClientListener {
    func remoteMediaClient(_ client: GCKRemoteMediaClient, didUpdate mediaStatus: GCKMediaStatus?) { statusChanged(mediaStatus) }
}

/// Music's queue plays on the Cast device track by track.
extension CastController: RemotePlayback {
    func play(_ item: Item, at seconds: Double) {
        let artist = item.artistCredit ?? item.grandparentTitle
        let art = app?.imageURL(item.images?.poster, width: 512)
        Task {
            guard let err = await load(itemID: item.id, at: seconds, title: item.title, subtitle: artist, artwork: art, music: true),
                  castingMusic else { return }
            // Couldn't cast: say why and carry on here.
            castingMusic = false
            music?.showError("Couldn't cast: \(err)")
            music?.remote = nil
        }
    }

    func resume() { _ = remote?.play() }
    func pause() { _ = remote?.pause() }
    func seek(_ seconds: Double) { seekTo(seconds) }
}

/// The Cast button (Google's): shows nearby TVs and speakers, and controls while casting.
struct CastButton: UIViewRepresentable {
    var tint: UIColor = .label

    func makeUIView(context: Context) -> GCKUICastButton {
        let b = GCKUICastButton(frame: CGRect(x: 0, y: 0, width: 28, height: 28))
        b.tintColor = tint
        b.accessibilityIdentifier = "castButton"
        return b
    }

    func updateUIView(_ uiView: GCKUICastButton, context: Context) { uiView.tintColor = tint }
}
