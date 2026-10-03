import MarqueeKit
import SwiftUI

/// Your listening and watching (ADM-4, MUSIC-11's "year in music").
struct StatsView: View {
    @Environment(AppSession.self) private var app
    @State private var days = 30
    @State private var stats: Schemas.Stats?
    @State private var error: String?

    var body: some View {
        List {
            Picker("Period", selection: $days) {
                Text("7 days").tag(7)
                Text("30 days").tag(30)
                Text("Year").tag(365)
                Text("All time").tag(0)
            }
            .pickerStyle(.segmented)
            // Year in music (MUSIC-22).
            RecapCard().listRowInsets(EdgeInsets()).listRowBackground(Color.clear)
            if let error { Text(error).foregroundStyle(.red) }
            if let s = stats {
                if s.plays == 0 {
                    Text("Nothing played in this period.").foregroundStyle(.secondary)
                } else {
                    Section {
                        LabeledContent("Plays", value: s.plays.formatted())
                        LabeledContent("Watched", value: hours(Double(s.videoHours)))
                        LabeledContent("Listened", value: hours(Double(s.musicHours)))
                    }
                    top("Top artists", s.artists)
                    top("Top albums", s.albums)
                    top("Top tracks", s.tracks)
                    top("Top movies", s.movies)
                    top("Top shows", s.shows)
                }
            } else if error == nil {
                ProgressView()
            }
        }
        .navigationTitle("Your Stats")
        .task(id: days) {
            do { stats = try await app.stats(days: days); error = nil } catch { self.error = error.localizedDescription }
        }
    }

    @ViewBuilder private func top(_ title: String, _ list: [Schemas.StatsCount]) -> some View {
        if !list.isEmpty {
            Section(title) {
                ForEach(Array(list.enumerated()), id: \.offset) { i, c in
                    row(i, c)
                }
            }
        }
    }

    @ViewBuilder private func row(_ i: Int, _ c: Schemas.StatsCount) -> some View {
        let label = HStack {
            Text("\(i + 1)").font(.caption.monospacedDigit()).foregroundStyle(.secondary).frame(width: 22, alignment: .trailing)
            VStack(alignment: .leading) {
                Text(c.title).lineLimit(1)
                if let sub = c.subtitle { Text(sub).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
            }
            Spacer()
            Text("\(c.plays) \(c.plays == 1 ? "play" : "plays")").font(.caption).foregroundStyle(.secondary)
        }
        if let id = c.id {
            NavigationLink(value: Route.item(id)) { label }
        } else {
            label
        }
    }

    private func hours(_ h: Double) -> String {
        h >= 1 ? String(format: "%.1f h", h) : "\(Int((h * 60).rounded())) min"
    }
}
