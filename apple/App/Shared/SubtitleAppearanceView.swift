import MarqueeKit
import SwiftUI

/// Subtitle appearance (PLAY-20): size, colour, background and position, with a preview.
/// Saved to the person's preferences, so it follows them to every app.
struct SubtitleAppearanceView: View {
    @Environment(AppSession.self) private var app
    @State private var style = SubtitleStyle()
    @State private var loaded = false
    @State private var error: String?
    @State private var saving: Task<Void, Never>?

    var body: some View {
        Form {
            Section {
                SubtitlePreview(style: style)
                    .listRowInsets(EdgeInsets())
            }
            if let error { Text(error).foregroundStyle(.red) }
            Section {
                Picker("Size", selection: binding(\.sizeValue) { $0.size = $1 }) {
                    ForEach(SubtitleStyle.SizePayload.allCases, id: \.self) { Text($0.label).tag($0) }
                }
                Picker("Colour", selection: binding(\.colorValue) { $0.color = $1 }) {
                    ForEach(colorChoices, id: \.hex) { c in Text(c.name).tag(c.hex) }
                }
                Picker("Background", selection: binding(\.backgroundValue) { $0.background = $1 }) {
                    ForEach(SubtitleStyle.BackgroundPayload.allCases, id: \.self) { Text($0.label).tag($0) }
                }
                Picker("Position", selection: binding(\.positionValue) { $0.position = $1 }) {
                    ForEach(SubtitleStyle.PositionPayload.allCases, id: \.self) { Text($0.label).tag($0) }
                }
            } footer: {
                Text("Applies to text subtitles on every Marquee app. Styled subtitles (ASS) keep their own look. Raised lifts subtitles above the player's controls where the player allows it.")
            }
            Section {
                Button("Reset to Default") { update(SubtitleStyle()) }
            }
        }
        .navigationTitle("Subtitle Appearance")
        .disabled(!loaded)
        .task {
            await app.refreshMe()
            style = app.subtitleStyle
            loaded = true
        }
    }

    /// The four colours, plus the saved one when it's a custom colour from the web.
    private var colorChoices: [(hex: String, name: String)] {
        let base = SubtitleStyle.colors
        return base.contains { $0.hex == style.colorValue } ? base : base + [(style.colorValue, "Custom (\(style.colorValue))")]
    }

    private func binding<V>(_ get: KeyPath<SubtitleStyle, V>, _ set: @escaping (inout SubtitleStyle, V) -> Void) -> Binding<V> {
        Binding(get: { style[keyPath: get] }, set: { v in
            var s = style
            set(&s, v)
            update(s)
        })
    }

    private func update(_ s: SubtitleStyle) {
        style = s
        // Saved straight away; quick changes save once.
        saving?.cancel()
        saving = Task {
            try? await Task.sleep(for: .milliseconds(300))
            guard !Task.isCancelled else { return }
            do {
                try await app.setSubtitleStyle(s)
                error = nil
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}

/// A frame of "video" with a subtitle line in the chosen style.
struct SubtitlePreview: View {
    let style: SubtitleStyle

    var body: some View {
        ZStack(alignment: .bottom) {
            LinearGradient(colors: [Color(red: 0.15, green: 0.3, blue: 0.45), Color(red: 0.55, green: 0.45, blue: 0.3)],
                           startPoint: .top, endPoint: .bottom)
            Image(systemName: "mountain.2.fill").font(.system(size: 90)).foregroundStyle(.black.opacity(0.25)).offset(y: -20)
            line
                .padding(.bottom, style.positionValue == .raised ? height * 0.3 : height * 0.08)
        }
        .frame(height: height)
        .clipped()
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("subtitlePreview")
        .accessibilityValue("\(style.sizeValue.rawValue), \(style.colorValue), \(style.backgroundValue.rawValue), \(style.positionValue.rawValue)")
    }

    private var line: some View {
        let c = style.rgb
        let color = Color(red: c.red, green: c.green, blue: c.blue)
        let text = Text("The quick brown fox jumps over the lazy dog.")
            .font(.system(size: baseSize * style.relativeSize / 100, weight: .medium))
            .foregroundStyle(color)
            .multilineTextAlignment(.center)
        return Group {
            switch style.backgroundValue {
            case .none:
                text.shadow(color: .black.opacity(0.9), radius: 2, x: 1, y: 2)
            case .outline:
                text.shadow(color: .black, radius: 0.6, x: 1, y: 1).shadow(color: .black, radius: 0.6, x: -1, y: -1)
                    .shadow(color: .black, radius: 0.6, x: 1, y: -1).shadow(color: .black, radius: 0.6, x: -1, y: 1)
            case .translucent:
                text.padding(.horizontal, 6).padding(.vertical, 2).background(Color.black.opacity(0.55))
            case .opaque:
                text.padding(.horizontal, 6).padding(.vertical, 2).background(Color.black)
            }
        }
        .padding(.horizontal, 12)
    }

    #if os(tvOS)
    private let height: CGFloat = 360
    private let baseSize: CGFloat = 36
    #else
    private let height: CGFloat = 200
    private let baseSize: CGFloat = 17
    #endif
}
