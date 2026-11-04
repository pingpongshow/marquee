import AVFoundation
import AVKit
import CoreAudio
import MarqueeKit
import SwiftUI

/// One output device from Core Audio.
struct AudioOutputDevice: Identifiable, Equatable, Sendable {
    enum Transport: Sendable { case builtIn, bluetooth, usb, hdmi, other }
    let uid: String
    let name: String
    let transport: Transport
    var id: String { uid }

    var icon: String {
        switch transport {
        case .builtIn: "laptopcomputer"
        case .bluetooth: "headphones"
        case .usb: "cable.connector"
        case .hdmi: "tv"
        case .other: "speaker.wave.2"
        }
    }
}

/// Which Mac output Marquee plays on, for this app alone: others stay on the system default. The
/// list keeps itself up to date as devices come and go. The choice is remembered by UID; when that
/// device isn't there the player uses the system default, and it's used again when it's back.
@MainActor @Observable
final class MacAudioOutputs {
    static let chosenKey = "marquee.mac.outputUID"
    static let chosenNameKey = "marquee.mac.outputName"

    private(set) var devices: [AudioOutputDevice] = []
    /// The system default output's name, for its row.
    private(set) var defaultName: String?
    /// The device asked for; it may be away just now.
    private(set) var chosenUID: String?
    /// Its name when it was picked, to say which one is away.
    private(set) var chosenName: String?

    /// What's actually in use: the chosen device while it's present, else nil (system default).
    var activeUID: String? {
        guard let chosenUID, devices.contains(where: { $0.uid == chosenUID }) else { return nil }
        return chosenUID
    }

    @ObservationIgnored private let music: MarqueeKit.MusicPlayer
    @ObservationIgnored private var videoPlayers: [WeakPlayer] = []

    private struct WeakPlayer { weak var player: AVPlayer? }

    init(music: MarqueeKit.MusicPlayer) {
        self.music = music
        chosenUID = UserDefaults.standard.string(forKey: Self.chosenKey)
        chosenName = UserDefaults.standard.string(forKey: Self.chosenNameKey)
        reload()
        Self.listen(kAudioHardwarePropertyDevices) { [weak self] in self?.reload() }
        Self.listen(kAudioHardwarePropertyDefaultOutputDevice) { [weak self] in self?.reload() }
    }

    /// nil is the system default.
    func choose(_ uid: String?) {
        chosenUID = uid
        UserDefaults.standard.set(uid, forKey: Self.chosenKey)
        chosenName = devices.first { $0.uid == uid }?.name
        UserDefaults.standard.set(chosenName, forKey: Self.chosenNameKey)
        apply()
    }

    /// A video's player follows the same choice.
    func register(_ player: AVPlayer) {
        videoPlayers.removeAll { $0.player == nil }
        if !videoPlayers.contains(where: { $0.player === player }) { videoPlayers.append(WeakPlayer(player: player)) }
        player.audioOutputDeviceUniqueID = activeUID
    }

    /// What the music player itself says it's using (for tests and the picker's checkmark).
    var playerUID: String? { music.audioOutputDeviceUID }

    private func reload() {
        devices = Self.outputDevices()
        defaultName = Self.defaultOutputName()
        apply()
    }

    private func apply() {
        let uid = activeUID
        if music.audioOutputDeviceUID != uid { music.audioOutputDeviceUID = uid }
        for p in videoPlayers { p.player?.audioOutputDeviceUniqueID = uid }
    }

    // MARK: Core Audio

    private static func address(_ selector: AudioObjectPropertySelector, scope: AudioObjectPropertyScope = kAudioObjectPropertyScopeGlobal) -> AudioObjectPropertyAddress {
        AudioObjectPropertyAddress(mSelector: selector, mScope: scope, mElement: kAudioObjectPropertyElementMain)
    }

    private static func listen(_ selector: AudioObjectPropertySelector, _ changed: @escaping @MainActor () -> Void) {
        var a = address(selector)
        AudioObjectAddPropertyListenerBlock(AudioObjectID(kAudioObjectSystemObject), &a, DispatchQueue.main) { _, _ in
            MainActor.assumeIsolated { changed() }
        }
    }

    private static func outputDevices() -> [AudioOutputDevice] {
        var a = address(kAudioHardwarePropertyDevices)
        var size: UInt32 = 0
        guard AudioObjectGetPropertyDataSize(AudioObjectID(kAudioObjectSystemObject), &a, 0, nil, &size) == noErr else { return [] }
        var ids = [AudioDeviceID](repeating: 0, count: Int(size) / MemoryLayout<AudioDeviceID>.size)
        guard AudioObjectGetPropertyData(AudioObjectID(kAudioObjectSystemObject), &a, 0, nil, &size, &ids) == noErr else { return [] }
        return ids.compactMap(device).sorted { ($0.transport == .builtIn ? 0 : 1, $0.name) < ($1.transport == .builtIn ? 0 : 1, $1.name) }
    }

    /// Nil for what isn't an output a person would pick: no output streams, hidden, the system's
    /// own aggregates, and AirPlay (which has its own picker).
    private static func device(_ id: AudioDeviceID) -> AudioOutputDevice? {
        var a = address(kAudioDevicePropertyStreams, scope: kAudioDevicePropertyScopeOutput)
        var size: UInt32 = 0
        guard AudioObjectGetPropertyDataSize(id, &a, 0, nil, &size) == noErr, size > 0 else { return nil }
        var hidden: UInt32 = 0
        var hiddenSize = UInt32(MemoryLayout<UInt32>.size)
        var ha = address(kAudioDevicePropertyIsHidden)
        if AudioObjectGetPropertyData(id, &ha, 0, nil, &hiddenSize, &hidden) == noErr, hidden != 0 { return nil }
        guard let uid = string(id, kAudioDevicePropertyDeviceUID), let name = string(id, kAudioObjectPropertyName) else { return nil }
        if uid.hasPrefix("CADefaultDeviceAggregate") || name.hasPrefix("CADefaultDeviceAggregate") { return nil }
        var transport: UInt32 = 0
        var tSize = UInt32(MemoryLayout<UInt32>.size)
        var ta = address(kAudioDevicePropertyTransportType)
        AudioObjectGetPropertyData(id, &ta, 0, nil, &tSize, &transport)
        let kind: AudioOutputDevice.Transport
        switch transport {
        case kAudioDeviceTransportTypeAirPlay: return nil
        case kAudioDeviceTransportTypeBuiltIn: kind = .builtIn
        case kAudioDeviceTransportTypeBluetooth, kAudioDeviceTransportTypeBluetoothLE: kind = .bluetooth
        case kAudioDeviceTransportTypeUSB: kind = .usb
        case kAudioDeviceTransportTypeHDMI, kAudioDeviceTransportTypeDisplayPort: kind = .hdmi
        default: kind = .other
        }
        return AudioOutputDevice(uid: uid, name: name, transport: kind)
    }

    private static func string(_ id: AudioObjectID, _ selector: AudioObjectPropertySelector) -> String? {
        var a = address(selector)
        var size = UInt32(MemoryLayout<Unmanaged<CFString>?>.size)
        var value: Unmanaged<CFString>?
        let status = withUnsafeMutablePointer(to: &value) { AudioObjectGetPropertyData(id, &a, 0, nil, &size, $0) }
        guard status == noErr, let value else { return nil }
        return value.takeRetainedValue() as String
    }

    private static func defaultOutputName() -> String? {
        var a = address(kAudioHardwarePropertyDefaultOutputDevice)
        var id: AudioDeviceID = 0
        var size = UInt32(MemoryLayout<AudioDeviceID>.size)
        guard AudioObjectGetPropertyData(AudioObjectID(kAudioObjectSystemObject), &a, 0, nil, &size, &id) == noErr else { return nil }
        return string(id, kAudioObjectPropertyName)
    }
}

// MARK: - The picker

/// The system's AirPlay route picker, on a given player.
struct MacRoutePicker: NSViewRepresentable {
    let player: AVPlayer?

    func makeNSView(context: Context) -> AVRoutePickerView {
        let v = AVRoutePickerView(frame: CGRect(x: 0, y: 0, width: 24, height: 24))
        v.isRoutePickerButtonBordered = false
        v.player = player
        v.setRoutePickerButtonColor(.white, for: .normal)
        v.setRoutePickerButtonColor(NSColor(Color.marqueeGold), for: .active)
        return v
    }

    func updateNSView(_ v: AVRoutePickerView, context: Context) {
        if v.player !== player { v.player = player }
    }
}

/// The output button: in the bar, the mini bar, Now Playing and the video player. Music offers
/// Mac outputs, AirPlay and Sonos rooms; a video's sound offers the first two.
struct MacOutputButton: View {
    enum Scope { case music, video }

    let scope: Scope
    @State private var showing = false
    @State private var playback = MacPlayback.shared

    private var sonos: SonosSession { playback.sonos }

    var body: some View {
        Button { showing.toggle() } label: {
            Image(systemName: icon)
                .foregroundStyle(active ? Color.marqueeGold : .primary)
                .frame(minWidth: 28, minHeight: 28)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(scope == .music ? "Choose where the music plays" : "Choose where the sound plays")
        .accessibilityLabel("Output")
        .accessibilityValue(valueText)
        .accessibilityIdentifier("output.button")
        .popover(isPresented: $showing, arrowEdge: .top) {
            MacOutputPicker(scope: scope).macEnvironment().preferredColorScheme(.dark)
        }
    }

    private var active: Bool {
        (scope == .music && (sonos.activeRoomName ?? sonos.waitingRoom?.name) != nil) || playback.outputs.activeUID != nil || playback.music.airPlaying
    }

    private var icon: String {
        if scope == .music, (sonos.activeRoomName ?? sonos.waitingRoom?.name) != nil { return "hifispeaker.fill" }
        if playback.music.airPlaying { return "airplayaudio" }
        return "speaker.wave.2.fill"
    }

    private var valueText: String {
        if scope == .music, let room = sonos.activeRoomName ?? sonos.waitingRoom?.name { return room }
        if let uid = playback.outputs.activeUID { return playback.outputs.devices.first { $0.uid == uid }?.name ?? uid }
        return "System default"
    }
}

/// The list itself.
struct MacOutputPicker: View {
    let scope: MacOutputButton.Scope
    @State private var playback = MacPlayback.shared

    private var outputs: MacAudioOutputs { playback.outputs }
    private var sonos: SonosSession { playback.sonos }
    private var music: MarqueeKit.MusicPlayer { playback.music }
    private var app: AppSession { playback.app }
    private var inRoom: Bool { scope == .music && sonos.activeRoomName != nil }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            volumeRow
            Divider()
            header("This Mac")
            row(title: "System Default", subtitle: outputs.defaultName, icon: "macbook.and.iphone",
                selected: !inRoom && outputs.chosenUID == nil, identifier: "output.default") { pick(nil) }
            ForEach(outputs.devices) { d in
                row(title: d.name, subtitle: nil, icon: d.icon, selected: !inRoom && outputs.activeUID == d.uid,
                    identifier: "output.device.\(d.uid)") { pick(d.uid) }
            }
            if outputs.chosenUID != nil, outputs.activeUID == nil {
                // The chosen device has gone: says so, and uses it again when it's back.
                Text("\(outputs.chosenName ?? "Your chosen output") isn't connected. Using the system default for now.")
                    .font(.caption).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("output.missing")
            }
            Divider()
            header("AirPlay")
            HStack(spacing: 8) {
                MacRoutePicker(player: scope == .music ? music.routingPlayer : MacVideo.shared.activePlayer)
                    .frame(width: 24, height: 24)
                    .accessibilityIdentifier("output.airplay")
                Text(music.airPlaying ? "Playing over AirPlay" : "Choose a speaker or TV…")
                Spacer(minLength: 0)
            }
            if scope == .music {
                Text("Over AirPlay the equaliser and levelling may not be heard, and crossfade is off.")
                    .font(.caption).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
            }
            if scope == .music, !app.isOffline, !sonos.rooms.isEmpty {
                Divider()
                header("Sonos")
                ForEach(sonos.rooms, id: \.id) { r in
                    row(title: r.name, subtitle: sonos.waitingRoom?.id == r.id ? "Plays here when you start something" : r.model, icon: "hifispeaker",
                        selected: (sonos.room?.id == r.id && sonos.isActive) || sonos.waitingRoom?.id == r.id,
                        identifier: "output.sonos.\(r.id)") { sonos.select(r) }
                }
                Text("The equaliser, crossfade and levelling don't apply on Sonos.")
                    .font(.caption).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("output.sonosNote")
            }
        }
        .padding(12)
        .frame(width: 320)
        .task {
            if scope == .music, !app.isOffline { await sonos.refreshRooms() } else if app.isOffline { sonos.clearRooms() }
        }
    }

    private var volumeRow: some View {
        HStack(spacing: 8) {
            Image(systemName: "speaker.fill").foregroundStyle(.secondary).font(.caption)
            if inRoom {
                Slider(value: Binding(get: { Double(sonos.volume) }, set: { sonos.setVolume(Int($0)) }), in: 0...100)
                    .accessibilityLabel("Room volume")
                    .accessibilityIdentifier("output.volume")
            } else {
                Slider(value: Binding(get: { Double(music.appVolume) }, set: { playback.setVolume(Float($0)) }), in: 0...1)
                    .accessibilityLabel("Volume")
                    .accessibilityIdentifier("output.volume")
            }
            Image(systemName: "speaker.wave.3.fill").foregroundStyle(.secondary).font(.caption)
        }
    }

    private func header(_ s: String) -> some View {
        Text(s).font(.caption.weight(.semibold)).foregroundStyle(.secondary)
    }

    private func row(title: String, subtitle: String?, icon: String, selected: Bool, identifier: String, action: @escaping () -> Void) -> some View {
        MacHoverRow(action: action) {
            HStack(spacing: 8) {
                Image(systemName: icon).frame(width: 20)
                VStack(alignment: .leading, spacing: 0) {
                    Text(title).lineLimit(1)
                    if let subtitle { Text(subtitle).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
                }
                Spacer(minLength: 0)
                if selected { Image(systemName: "checkmark").foregroundStyle(Color.marqueeGold).accessibilityLabel("Selected") }
            }
        }
        .accessibilityValue(selected ? "Selected" : "")
        .accessibilityIdentifier(identifier)
    }

    /// A Mac output: back from a room if there is one (which resumes here), then that device.
    private func pick(_ uid: String?) {
        outputs.choose(uid)
        sonos.clearWaitingRoom()
        if inRoom { sonos.leave() }
    }
}

/// A plain row that lights up under the pointer.
struct MacHoverRow<Content: View>: View {
    let action: () -> Void
    @ViewBuilder let content: Content
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            content
                .padding(.horizontal, 6).padding(.vertical, 3)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(hovering ? Color.primary.opacity(0.1) : .clear, in: RoundedRectangle(cornerRadius: 6))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
    }
}
