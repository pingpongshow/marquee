import Foundation
import MarqueeAPI

/// Chromecast support shared with the app (D82): what the Default Media Receiver plays, and
/// server sessions for it. The Cast SDK itself lives in the iPhone/iPad app.
public extension AppleDeviceProfile {
    /// H.264 with AAC/MP3 (HLS in fMP4, or MP4 files) and common audio files.
    static func chromecast() -> Schemas.DeviceProfile {
        .init(
            containers: ["mp4", "m4v", "mp3", "m4a", "flac", "ogg", "opus", "wav"],
            videoCodecs: ["h264"],
            audioCodecs: ["aac", "mp3", "flac", "opus", "vorbis"],
            maxAudioChannels: 6,
            hls: true,
            hlsVideoCodecs: ["h264"],
            hlsAudioCodecs: ["aac", "mp3"],
            textSubtitles: true
        )
    }
}


@MainActor
public extension AppSession {
    /// A playback session in the Chromecast profile.
    func castSession(itemID: Int64, startMs: Int64, fileID: Int64? = nil, audio: Int64? = nil, subtitle: Int64? = nil) async throws -> PlaybackSession {
        guard let client else { throw MarqueeError("Not connected") }
        return try await client.startPlayback(body: .json(.init(
            itemId: itemID, fileId: fileID, audioStreamId: audio, subtitleStreamId: subtitle, startMs: startMs,
            profile: AppleDeviceProfile.chromecast()))).ok.body.json
    }

    /// A stream URL a Cast device can reach: the server's home-network address when known.
    func castURL(_ path: String) -> URL? {
        if path.hasPrefix("http") { return URL(string: path) }
        guard let base = server?.lanURL ?? baseURL else { return nil }
        return URL(string: base.absoluteString.trimmingCharacters(in: CharacterSet(charactersIn: "/")) + path)
    }

    func reportCast(_ session: String, positionMs: Int64, playing: Bool) {
        let c = client
        Task { _ = try? await c?.reportPlayback(path: .init(sessionId: session), body: .json(.init(positionMs: positionMs, state: playing ? .playing : .paused))) }
    }

    func stopCastSession(_ session: String) {
        let c = client
        Task { _ = try? await c?.stopPlayback(path: .init(sessionId: session)) }
    }
}
