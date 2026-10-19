#if os(iOS)
import MarqueeKit
import SwiftUI

/// Settings → Downloads → Smart downloads: two switches (shows, music), each with its own
/// storage budget, plus Wi-Fi only and the episodes' quality.
struct SmartDownloadsSettingsView: View {
    @Environment(SmartDownloads.self) private var smart
    @Environment(AppSession.self) private var app

    var body: some View {
        @Bindable var smart = smart
        Form {
            Section {
                Toggle("Next episodes", isOn: $smart.settings.showsEnabled)
                    .accessibilityIdentifier("smart.shows")
                if smart.settings.showsEnabled {
                    Picker("Episodes per show", selection: $smart.settings.episodesPerShow) {
                        ForEach(SmartDownloadSettings.episodeChoices, id: \.self) { n in Text("\(n)").tag(n) }
                    }
                    budgetPicker($smart.settings.showsBudget)
                    LabeledContent("Using", value: usage(.shows))
                }
            } header: {
                Text("Shows")
            } footer: {
                Text("Keeps the next unwatched episodes of the shows on your Continue Watching downloaded, and removes them once watched. Episodes you download yourself are never removed. When the space runs out, the shows you watched longest ago go first.")
            }
            Section {
                Toggle("Favourites and most played", isOn: $smart.settings.musicEnabled)
                    .accessibilityIdentifier("smart.music")
                if smart.settings.musicEnabled {
                    budgetPicker($smart.settings.musicBudget)
                    LabeledContent("Using", value: usage(.music))
                }
            } header: {
                Text("Music")
            } footer: {
                Text("Keeps the songs, albums and artists you've hearted downloaded, then what you play most and lately, as space allows.")
            }
            Section {
                Toggle("Wi-Fi only", isOn: $smart.settings.wifiOnly)
                Picker("Episode quality", selection: $smart.settings.quality) {
                    ForEach(Downloads.Quality.allCases, id: \.self) { q in Text(q.label).tag(q) }
                }
            } footer: {
                Text(footer)
            }
            if smart.hasAny {
                Section {
                    Button("Remove Smart Downloads", role: .destructive) { smart.removeAll() }
                        .accessibilityIdentifier("smart.removeAll")
                } footer: {
                    Text("Removes what smart downloads fetched. While a switch is on, it downloads them again.")
                }
            }
        }
        .navigationTitle("Smart Downloads")
        .onChange(of: smart.settings) { Task { await smart.run(app) } }
    }

    private func budgetPicker(_ selection: Binding<SmartBudget>) -> some View {
        Picker("Storage", selection: selection) {
            ForEach(SmartBudget.choices, id: \.self) { b in Text(b.label).tag(b) }
        }
    }

    private func usage(_ kind: SmartDownloads.Kind) -> String {
        let n = smart.entries(kind).count
        return "\(ByteCountFormatter.string(fromByteCount: smart.bytes(kind), countStyle: .file)) · \(n) \(kind == .shows ? (n == 1 ? "episode" : "episodes") : (n == 1 ? "song" : "songs"))"
    }

    private var footer: String {
        var s = "Smart downloads run while the app is open: when it starts or comes back, and after a video ends. Turning a switch off removes what it downloaded."
        if smart.running { s += "\nChecking now…" } else if let note = smart.note { s += "\n\(note)" }
        return s
    }
}

/// The Downloads page's summary of smart downloads: what each kind holds, how much space it
/// uses, its settings, and Remove Smart Downloads.
struct SmartDownloadsSection: View {
    @Environment(SmartDownloads.self) private var smart

    var body: some View {
        let on = smart.settings.showsEnabled || smart.settings.musicEnabled
        if on || smart.hasAny {
            Section {
                ForEach(SmartDownloads.Kind.allCases, id: \.self) { kind in
                    let list = smart.entries(kind)
                    if !list.isEmpty || enabled(kind) {
                        LabeledContent {
                            Text(ByteCountFormatter.string(fromByteCount: smart.bytes(kind), countStyle: .file))
                        } label: {
                            Text(kind == .shows ? "Next episodes" : "Music")
                            Text(detail(kind, list)).font(.caption)
                        }
                        .accessibilityIdentifier("smart.summary.\(kind.rawValue)")
                    }
                }
                NavigationLink("Smart Downloads Settings", value: Route.settings(.smartDownloads))
                if smart.hasAny {
                    Button("Remove Smart Downloads", role: .destructive) { smart.removeAll() }
                }
            } header: {
                Text("Smart downloads")
            } footer: {
                if let note = smart.note { Text(note) }
            }
        }
    }

    private func enabled(_ kind: SmartDownloads.Kind) -> Bool {
        kind == .shows ? smart.settings.showsEnabled : smart.settings.musicEnabled
    }

    private func detail(_ kind: SmartDownloads.Kind, _ list: [Downloads.Entry]) -> String {
        let n = list.count, waiting = list.filter { $0.state != .done }.count
        var s: String
        if kind == .shows {
            let shows = Set(list.compactMap(\.item.grandparentId)).count
            s = "\(n) \(n == 1 ? "episode" : "episodes") of \(shows) \(shows == 1 ? "show" : "shows")"
        } else {
            s = "\(n) \(n == 1 ? "song" : "songs")"
        }
        if waiting > 0 { s += " · \(waiting) downloading" }
        if !enabled(kind) { s += " · off" }
        return s
    }
}
#endif
