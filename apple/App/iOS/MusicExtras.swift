import MarqueeKit
import SwiftUI

/// The music equaliser (iPhone/iPad): on/off, presets and ten bands of ±12 dB, kept on this
/// device. Opened from Settings → Music.
struct EqualizerView: View {
    @Environment(MusicPlayer.self) private var music

    var body: some View {
        let eq = music.equalizer
        Form {
            Section {
                Toggle("Equalizer", isOn: Binding(get: { eq.enabled }, set: { eq.enabled = $0 }))
                if let note = music.equalizerNote {
                    Label(note, systemImage: "info.circle").font(.footnote).foregroundStyle(.secondary)
                        .accessibilityIdentifier("eqNote")
                }
            }
            Section("Preset") {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        ForEach(Equalizer.presets) { p in
                            Button(p.name) { eq.apply(p) }
                                .buttonStyle(.bordered)
                                .tint(eq.preset == p.name ? Color.marqueeGold : .secondary)
                                .accessibilityAddTraits(eq.preset == p.name ? .isSelected : [])
                        }
                    }
                    .padding(.vertical, 4)
                }
                if eq.preset == Equalizer.custom { Text("Custom").font(.footnote).foregroundStyle(Color.marqueeGold) }
            }
            Section {
                ForEach(Array(Equalizer.bands.enumerated()), id: \.offset) { i, hz in
                    HStack(spacing: 12) {
                        Text(Equalizer.label(hz)).font(.caption.monospacedDigit()).frame(width: 34, alignment: .trailing)
                        Slider(value: Binding(get: { eq.gains[i] }, set: { eq.setGain(i, $0) }), in: -Equalizer.maxGain...Equalizer.maxGain)
                            .accessibilityLabel("\(Equalizer.label(hz)) hertz")
                            .accessibilityValue(String(format: "%+.1f decibels", eq.gains[i]))
                        Text(String(format: "%+.1f", eq.gains[i])).font(.caption.monospacedDigit()).frame(width: 40, alignment: .trailing)
                    }
                }
            } header: {
                Text("Bands (dB)")
            } footer: {
                Text("Applies to music played on this device. Gapless albums and volume levelling keep working.")
            }
            .disabled(!eq.enabled)
        }
        .navigationTitle("Equalizer")
        .navigationBarTitleDisplayMode(.inline)
    }
}
