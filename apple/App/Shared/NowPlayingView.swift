import MarqueeKit
import SwiftUI

/// The bar above the tab bar while music plays (iPhone/iPad).
struct MiniPlayerBar: View {
    @Environment(MusicPlayer.self) private var music
    @Environment(AppSession.self) private var app
    @Environment(\.openNowPlaying) private var openNowPlaying

    var body: some View {
        if let t = music.current?.item {
            HStack(spacing: 12) {
                Button { openNowPlaying() } label: {
                    HStack(spacing: 12) {
                        ArtworkView(item: t, shape: .square, width: 44).frame(width: 44)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(t.title).font(.subheadline.weight(.semibold)).lineLimit(1)
                            Text(t.artistCredit ?? t.grandparentTitle ?? "").font(.caption).foregroundStyle(.secondary).lineLimit(1)
                        }
                        Spacer(minLength: 0)
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                Button { music.toggle() } label: { Image(systemName: music.playing ? "pause.fill" : "play.fill").font(.title3).frame(width: 36, height: 36) }
                    .buttonStyle(.plain)
                    .accessibilityLabel(music.playing ? "Pause" : "Play")
                Button { music.next() } label: { Image(systemName: "forward.fill").font(.title3).frame(width: 36, height: 36) }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Next track")
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 14))
            .overlay(alignment: .bottom) {
                GeometryReader { g in
                    Rectangle().fill(Color.marqueeGold).frame(width: g.size.width * (music.duration > 0 ? music.time / music.duration : 0), height: 2)
                }
                .frame(height: 2)
                .padding(.horizontal, 10)
            }
            .padding(.horizontal, 10)
            .padding(.bottom, 4)
        }
    }
}

/// Full-screen Now Playing with the queue.
struct NowPlayingView: View {
    @Environment(MusicPlayer.self) private var music
    @Environment(AppSession.self) private var app
    @State private var showQueue = false
    @State private var scrub: Double?

    var body: some View {
        if let t = music.current?.item {
            ZStack {
                backdrop(t)
                #if os(tvOS)
                HStack(spacing: 80) {
                    player(t).frame(maxWidth: 700)
                    queueList.frame(width: 700)
                }
                .padding(80)
                #else
                VStack(spacing: 22) {
                    Capsule().fill(.secondary).frame(width: 40, height: 5).padding(.top, 8)
                    if showQueue { queueList } else { player(t) }
                    Button { withAnimation { showQueue.toggle() } } label: {
                        Label(showQueue ? "Now Playing" : "Up Next", systemImage: showQueue ? "music.note" : "list.bullet")
                    }
                    .padding(.bottom)
                }
                .padding(.horizontal, 24)
                #endif
            }
        } else {
            ContentUnavailableView("Nothing playing", systemImage: "music.note")
        }
    }

    private func backdrop(_ t: Item) -> some View {
        ZStack {
            Color.black
            AsyncImage(url: app.imageURL(t.images?.poster, width: 64)) { $0.image?.resizable().scaledToFill() }
                .blur(radius: 60).opacity(0.55)
        }
        .ignoresSafeArea()
    }

    private func player(_ t: Item) -> some View {
        VStack(spacing: 22) {
            Spacer(minLength: 0)
            ArtworkView(item: t, shape: .square, width: 600)
                .frame(maxWidth: artSize)
                .shadow(color: .black.opacity(0.5), radius: 30, y: 10)
            VStack(spacing: 4) {
                Text(t.title).font(.title2.bold()).lineLimit(1)
                Text([t.artistCredit ?? t.grandparentTitle, t.parentTitle].compactMap { $0 }.joined(separator: " · ")).foregroundStyle(.secondary).lineLimit(1)
            }
            VStack(spacing: 4) {
                #if os(tvOS)
                ProgressView(value: music.duration > 0 ? music.time / music.duration : 0)
                #else
                Slider(value: Binding(get: { scrub ?? music.time }, set: { scrub = $0 }), in: 0...max(music.duration, 1)) { editing in
                    if !editing, let s = scrub { music.seek(s); scrub = nil }
                }
                #endif
                HStack {
                    Text(formatTime(seconds: scrub ?? music.time))
                    Spacer()
                    Text("-" + formatTime(seconds: max(0, music.duration - (scrub ?? music.time))))
                }
                .font(.caption.monospacedDigit()).foregroundStyle(.secondary)
            }
            HStack(spacing: 36) {
                Button { music.toggleShuffle() } label: { Image(systemName: "shuffle").foregroundStyle(music.queue.shuffled ? Color.marqueeGold : .secondary) }
                Button { music.previous() } label: { Image(systemName: "backward.fill").font(.title) }
                Button { music.toggle() } label: { Image(systemName: music.playing ? "pause.circle.fill" : "play.circle.fill").font(.system(size: 64)) }
                Button { music.next() } label: { Image(systemName: "forward.fill").font(.title) }
                Button { music.cycleRepeat() } label: {
                    Image(systemName: music.queue.repeatMode == .one ? "repeat.1" : "repeat")
                        .foregroundStyle(music.queue.repeatMode == .off ? .secondary : Color.marqueeGold)
                }
            }
            .buttonStyle(.plain)
            Spacer(minLength: 0)
        }
    }

    #if os(tvOS)
    private let artSize: CGFloat = 560
    #else
    private let artSize: CGFloat = 340
    #endif

    private var queueList: some View {
        List {
            if let cur = music.current {
                Section("Now playing") { row(cur, current: true) }
            }
            Section("Up next") {
                ForEach(Array(music.queue.upcoming)) { e in row(e, current: false) }
                    #if os(iOS)
                    .onMove { from, to in
                        guard let f = from.first else { return }
                        let entries = Array(music.queue.upcoming)
                        let target = music.queue.index + 1 + (to > f ? to - 1 : to)
                        music.move(entries[f].id, to: target)
                    }
                    .onDelete { offsets in
                        let entries = Array(music.queue.upcoming)
                        for i in offsets { music.remove(entries[i].id) }
                    }
                    #endif
            }
        }
        #if os(iOS)
        .environment(\.editMode, .constant(.active))
        .scrollContentBackground(.hidden)
        #endif
    }

    private func row(_ e: PlayQueue.Entry, current: Bool) -> some View {
        Button { if !current { music.jump(e.id) } } label: {
            HStack(spacing: 12) {
                ArtworkView(item: e.item, shape: .square, width: 44).frame(width: 44)
                VStack(alignment: .leading) {
                    Text(e.item.title).lineLimit(1).foregroundStyle(current ? Color.marqueeGold : .primary)
                    Text(e.item.artistCredit ?? e.item.grandparentTitle ?? "").font(.caption).foregroundStyle(.secondary).lineLimit(1)
                }
            }
        }
        .buttonStyle(.plain)
    }
}
