#if DEBUG
import AVFoundation
import CoreAudio
import Darwin
import MarqueeKit
import MediaPlayer
import Security
import SwiftUI

/// Debug builds only, with `-marquee-test-driver <folder>`: reads a command from <folder>/cmd and
/// writes the answer to <folder>/out, so the app can be driven and looked at without taking the
/// mouse or keyboard on a Mac someone's working on.
@MainActor
enum MacTestDriver {
    static func startIfAsked() {
        let a = ProcessInfo.processInfo.arguments
        guard let i = a.firstIndex(of: "-marquee-test-driver"), a.indices.contains(i + 1) else { return }
        let folder = URL(fileURLWithPath: a[i + 1])
        Task {
            while true {
                try? await Task.sleep(for: .milliseconds(150))
                let cmdURL = folder.appending(path: "cmd")
                guard let line = try? String(contentsOf: cmdURL, encoding: .utf8) else { continue }
                try? FileManager.default.removeItem(at: cmdURL)
                let out = await run(line.trimmingCharacters(in: .whitespacesAndNewlines))
                try? out.write(to: folder.appending(path: "out.tmp"), atomically: true, encoding: .utf8)
                try? FileManager.default.moveItem(at: folder.appending(path: "out.tmp"), to: folder.appending(path: "out"))
            }
        }
    }

    private static var p: MacPlayback { MacPlayback.shared }
    private static var fakeDevice = AudioObjectID(0)

    private static func run(_ line: String) async -> String {
        let w = line.split(separator: " ", omittingEmptySubsequences: true).map(String.init)
        guard let cmd = w.first else { return "empty" }
        do {
            switch cmd {
            case "signin":
                try await p.app.addServer(lan: w[1], remote: "")
                try await p.app.signIn(username: w[2], password: w[3])
                return "signed in"
            case "signout": await p.app.signOut(); return "ok"
            case "select": MacNavigation.shared.select(w[1]); return "ok"
            case "item": MacNavigation.shared.push(.item(Int64(w[1])!)); return "ok"
            case "route":
                switch w[1] {
                case "muse": MacNavigation.shared.push(.musicMuse(library: Int64(w[2])!))
                case "moods": MacNavigation.shared.push(.musicBrowse(library: Int64(w[2])!, .moodsAndStyles))
                case "topSongs": MacNavigation.shared.push(.topSongs(artist: Int64(w[2])!, name: "Artist"))
                case "equalizer": MacNavigation.shared.push(.settings(.equalizer))
                case "smart": MacNavigation.shared.push(.settings(.smartDownloads))
                case "dlquality": MacNavigation.shared.push(.settings(.downloadQuality(song: w[2] == "song")))
                case "stats": MacNavigation.shared.push(.settings(.stats))
                case "person": MacNavigation.shared.push(.person(Int64(w[2])!))
                case "subtitle": MacNavigation.shared.push(.settings(.subtitleAppearance))
                default: return "unknown route"
                }
                return "ok"
            case "back": MacNavigation.shared.goBack(); return "ok"
            case "video":
                p.video.play(Int64(w[1])!, startMs: w.count > 2 ? Int64(w[2]) : nil)
                return "ok"
            case "chrome": MacVideo.shared.chrome?.show(); return "ok"
            case "videoClose": MacVideo.shared.closeActive(); return "ok"
            case "videoState":
                guard let v = MacVideo.shared.activePlayer else { return "no video" }
                return "item=\(v.currentItem.map { String(describing: ObjectIdentifier($0)) } ?? "-") t=\(v.currentTime().seconds) rate=\(v.rate) status=\(v.timeControlStatus.rawValue) output=\(v.audioOutputDeviceUniqueID ?? "default") player=\(ObjectIdentifier(v))"
            case "music":
                let ids = w[1].split(separator: ",").compactMap { Int64($0) }
                var items: [Item] = []
                for id in ids {
                    let d = try await p.app.item(id)
                    if d.type == .track { items.append(d.base) } else { items += try await p.app.leaves(id) }
                }
                p.music.play(items, source: "Driver")
                return "\(items.count) tracks"
            case "download":
                let ids = w[1].split(separator: ",").compactMap { Int64($0) }
                for id in ids { await p.downloads.download(children: try await p.app.leaves(id)) }
                return "\(p.downloads.entries.count) entries"
            case "downloads":
                let at = (p.music.routingPlayer.currentItem?.asset as? AVURLAsset)?.url.absoluteString ?? "-"
                return p.downloads.entries.values.map { "\($0.item.title): \($0.state) \($0.size) \(p.downloads.localURL($0.id)?.path ?? "-")" }.joined(separator: "\n")
                    + "\ntotal \(p.downloads.totalBytes) done \(p.downloads.doneCount)\nplaying from \(at)"
            case "scroll":
                // The tallest scrolling view in the main window, to a point down its content.
                func find(_ v: NSView) -> [NSScrollView] { (v as? NSScrollView).map { [$0] } ?? v.subviews.flatMap(find) }
                let main = NSApp.windows.first { $0.isVisible && !($0 is NSPanel) && $0.level == .normal && $0.frame.width > 200 }
                guard let sv = main?.contentView.map(find)?.max(by: { ($0.documentView?.frame.height ?? 0) < ($1.documentView?.frame.height ?? 0) }) else { return "no scroll view" }
                sv.contentView.scroll(to: NSPoint(x: 0, y: Double(w[1])!))
                sv.reflectScrolledClipView(sv.contentView)
                return "of \(sv.documentView?.frame.height ?? 0)"
            case "radio": p.music.startRadio(itemID: Int64(w[1])!, app: p.app); return "ok"
            case "toggle": p.togglePlayPause(); return "ok"
            case "next": p.music.next(); return "ok"
            case "prev": p.music.previous(); return "ok"
            case "seek": p.music.seek(Double(w[1])!); return "ok"
            case "stop": p.music.stop(); return "ok"
            case "nowplaying": MacNavigation.shared.showNowPlaying = w.count < 2 || w[1] == "on"; return "ok"
            case "mini": MacWindows.shared.switchToMini(); return "ok"
            case "full": MacWindows.shared.switchToFull(); return "ok"
            case "closeMain":
                let main = NSApp.windows.first { $0.identifier?.rawValue.contains("main") == true || $0.title == "Marquee" }
                main?.performClose(nil)
                return "closed \(main != nil)"
            case "output":
                p.outputs.choose(w[1] == "default" ? nil : w.dropFirst().joined(separator: " "))
                return "ok"
            case "sonos":
                if w[1] == "leave" { p.sonos.leave(); return "ok" }
                await p.sonos.refreshRooms()
                // Never a real speaker from here, unless a run names the one room it may use.
                let args = ProcessInfo.processInfo.arguments
                let allowed = args.firstIndex(of: "-marquee-test-allow-room").flatMap { args.indices.contains($0 + 1) ? args[$0 + 1] : nil }
                guard args.contains("-marquee-test-speakers") || allowed == w[1] else { return "rooms: \(p.sonos.rooms.map { "\($0.name) (\($0.model))" })" }
                guard let r = p.sonos.rooms.first(where: { $0.id == w[1] }) else { return "no room \(w[1]); rooms: \(p.sonos.rooms.map(\.id))" }
                p.sonos.select(r)
                return "ok"
            case "eq":
                p.music.equalizer.enabled = w[1] == "on"
                if w.count > 2, let preset = Equalizer.presets.first(where: { $0.name == w[2...].joined(separator: " ") }) { p.music.equalizer.apply(preset) }
                return "ok"
            case "crossfade": p.music.crossfade = Int(w[1])!; return "ok"
            case "room": // Debug: the room's name and status
                return "\(p.sonos.room.map { "\($0.name) \($0.state) \($0.positionMs ?? -1) q\($0.queueIndex ?? -1)/\($0.queueLength ?? -1)" } ?? "-")"
            case "movemini":
                NSApp.windows.first { $0.title == "Marquee Mini Player" }?.setFrameOrigin(NSPoint(x: Double(w[1])!, y: Double(w[2])!))
                return "ok"
            case "fakeadd":
                // A device only this process can see, to try devices coming and going.
                let desc: [String: Any] = [kAudioAggregateDeviceNameKey: "Test Output", kAudioAggregateDeviceUIDKey: "test.marquee.output",
                                           kAudioAggregateDeviceIsPrivateKey: 1,
                                           kAudioAggregateDeviceSubDeviceListKey: [[kAudioSubDeviceUIDKey: "BlackHole2ch_UID"]]]
                let status = AudioHardwareCreateAggregateDevice(desc as CFDictionary, &fakeDevice)
                return "created \(status)"
            case "fakeremove":
                let status = AudioHardwareDestroyAggregateDevice(fakeDevice)
                return "destroyed \(status)"
            case "forgetall":
                // Leaves no test sign-in behind.
                for server in ServerStore.servers { ServerStore.forget(server.id) }
                let q: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: "app.marquee"]
                return "keychain \(SecItemDelete(q as CFDictionary))"
            case "appearance":
                return NSApp.effectiveAppearance.name.rawValue
            case "state":
                return state()
            case "shot":
                // An app can always capture its own windows, whatever Screen Recording says.
                return shot(to: w[1], window: w.count > 2 ? Int(w[2]) : nil)
            case "press":
                // Runs a menu item by title, as the menu would.
                return press(w.dropFirst().joined(separator: " "))
            default:
                return "unknown \(cmd)"
            }
        } catch {
            return "error: \(error)"
        }
    }

    private static func shot(to path: String, window: Int?) -> String {
        let target = NSApp.windows.first { w in
            if let window { return w.windowNumber == window }
            return w.isVisible && !(w is NSPanel) && w.level == .normal && w.frame.width > 200
        } ?? NSApp.windows.first { $0.isVisible && $0.frame.width > 200 }
        guard let target else { return "no window" }
        typealias Capture = @convention(c) (CGRect, UInt32, UInt32, UInt32) -> Unmanaged<CGImage>?
        guard let sym = dlsym(UnsafeMutableRawPointer(bitPattern: -2), "CGWindowListCreateImage") else { return "no capture function" }
        let capture = unsafeBitCast(sym, to: Capture.self)
        // Including the window only; best resolution; no framing shadow. A floating panel only
        // captures at the normal level.
        let level = target.level
        target.level = .normal
        defer { target.level = level }
        let rep: NSBitmapImageRep
        if let image = capture(.null, 1 << 3, UInt32(target.windowNumber), (1 << 0) | (1 << 3))?.takeRetainedValue(), image.width > 8 {
            rep = NSBitmapImageRep(cgImage: image)
        } else if let view = target.contentView, let cached = view.bitmapImageRepForCachingDisplay(in: view.bounds) {
            // Some panels can't be captured from outside: draw the view instead.
            view.cacheDisplay(in: view.bounds, to: cached)
            rep = cached
        } else {
            return "capture failed"
        }
        guard let data = rep.representation(using: .png, properties: [:]) else { return "no png" }
        do { try data.write(to: URL(fileURLWithPath: path)) } catch { return "\(error)" }
        return "\(path) \(rep.pixelsWide)x\(rep.pixelsHigh) of #\(target.windowNumber)"
    }

    private static func press(_ title: String) -> String {
        func find(_ menu: NSMenu?) -> NSMenuItem? {
            for item in menu?.items ?? [] {
                if item.title == title { return item }
                if let f = find(item.submenu) { return f }
            }
            return nil
        }
        guard let item = find(NSApp.mainMenu), let menu = item.menu else { return "no menu item \(title)" }
        menu.performActionForItem(at: menu.index(of: item))
        return "pressed \(title)"
    }

    private static func menus() -> String {
        func dump(_ menu: NSMenu?, depth: Int) -> [String] {
            (menu?.items ?? []).filter { !$0.isSeparatorItem }.flatMap { item -> [String] in
                let key = item.keyEquivalent.isEmpty ? "" : " [\(item.keyEquivalentModifierMask.rawValue >> 16):\(item.keyEquivalent.unicodeScalars.map { String($0.value, radix: 16) }.joined())]"
                return [String(repeating: "  ", count: depth) + item.title + key + (item.isEnabled ? "" : " (off)")] + dump(item.submenu, depth: depth + 1)
            }
        }
        return dump(NSApp.mainMenu, depth: 0).joined(separator: "\n")
    }

    private static func systemDefaultUID() -> String {
        var a = AudioObjectPropertyAddress(mSelector: kAudioHardwarePropertyDefaultOutputDevice, mScope: kAudioObjectPropertyScopeGlobal, mElement: kAudioObjectPropertyElementMain)
        var id: AudioDeviceID = 0
        var size = UInt32(MemoryLayout<AudioDeviceID>.size)
        guard AudioObjectGetPropertyData(AudioObjectID(kAudioObjectSystemObject), &a, 0, nil, &size, &id) == noErr else { return "?" }
        var ua = AudioObjectPropertyAddress(mSelector: kAudioDevicePropertyDeviceUID, mScope: kAudioObjectPropertyScopeGlobal, mElement: kAudioObjectPropertyElementMain)
        var value: Unmanaged<CFString>?
        var s = UInt32(MemoryLayout<Unmanaged<CFString>?>.size)
        _ = withUnsafeMutablePointer(to: &value) { AudioObjectGetPropertyData(id, &ua, 0, nil, &s, $0) }
        return value?.takeRetainedValue() as String? ?? "?"
    }

    private static func state() -> String {
        var o: [String] = []
        o.append("state=\(p.app.state) offline=\(p.app.isOffline) me=\(p.app.me?.displayName ?? "-") admin=\(p.app.me?.isAdmin == true)")
        let m = p.music
        o.append("music: \(m.current?.item.title ?? "-") playing=\(m.playing) t=\(Int(m.time))/\(Int(m.duration)) idx=\(m.queue.index)/\(m.queue.entries.count) source=\(m.source?.title ?? "-") volume=\(m.appVolume) error=\(m.error ?? "-")")
        o.append("output: player=\(m.audioOutputDeviceUID ?? "nil(default)") chosen=\(p.outputs.chosenUID ?? "nil") system=\(systemDefaultUID()) defaultName=\(p.outputs.defaultName ?? "-")")
        o.append("devices: " + p.outputs.devices.map { "\($0.name)[\($0.uid)]" }.joined(separator: " | "))
        o.append("airplaying=\(m.airPlaying) eq=\(m.equalizer.enabled) tapRates=\(m.equalizer.tapSampleRates) eqNote=\(m.equalizerNote ?? "-")")
        let np = MPNowPlayingInfoCenter.default()
        o.append("nowPlayingInfo: \(np.nowPlayingInfo?[MPMediaItemPropertyTitle] ?? "-") rate=\(np.nowPlayingInfo?[MPNowPlayingInfoPropertyPlaybackRate] ?? "-") hasArt=\(np.nowPlayingInfo?[MPMediaItemPropertyArtwork] != nil) state=\(np.playbackState.rawValue) borrowed=\(m.nowPlayingBorrowed)")
        o.append("sonos: state=\(p.sonos.state) rooms=\(p.sonos.rooms.map(\.name)) room=\(p.sonos.room?.name ?? "-") volume=\(p.sonos.volume) notice=\(p.sonos.notice ?? "-")")
        o.append("mini=\(MacWindows.shared.isMini) mainVisible=\(MacWindows.shared.mainVisible) menuBarIcon=\(MacWindows.shared.showsMenuBarIcon) onTop=\(MacWindows.shared.miniOnTop)")
        o.append("video=\(p.video.request?.itemID.description ?? "-") activePlayer=\(MacVideo.shared.activePlayer != nil)")
        o.append("nav=\(MacNavigation.shared.selection ?? "-")")
        for w in NSApp.windows {
            o.append("window #\(w.windowNumber) '\(w.title)' visible=\(w.isVisible) level=\(w.level.rawValue) frame=\(Int(w.frame.origin.x)),\(Int(w.frame.origin.y)) \(Int(w.frame.width))x\(Int(w.frame.height)) key=\(w.isKeyWindow) class=\(type(of: w))")
        }
        o.append("menus:\n" + menus())
        return o.joined(separator: "\n")
    }
}
#endif
