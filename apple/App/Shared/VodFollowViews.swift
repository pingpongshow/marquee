import MarqueeKit
import SwiftUI

// Following series on demand: a show's Follow button and sheet (size limit, the episodes already
// there, a save folder), the follow's state with Unfollow, and the Following list from VOD Shows.

/// Size limits offered for a follow, in GB (0 = none: only the recording space limits it).
private let followLimits: [Double] = [0, 5, 10, 20, 50, 100, 200]

private func limitLabel(_ gb: Double) -> String {
    gb == 0 ? "No limit" : "\(gb.formatted(.number.precision(.fractionLength(0...1)))) GB"
}

/// The Follow button on a show's page, or its follow's state (episodes, size, status) with
/// Change and Unfollow. Shown while the server lets people follow: admins read the setting;
/// for everyone else the show says (its `following` field), and a 409 hides it.
struct VodFollowControl: View {
    @Environment(AppSession.self) private var app
    let show: VodSeriesDetail
    @State private var follow: VodFollow?
    @State private var loaded = false
    /// Admins: following is on, and the limit a new follow starts with.
    @State private var settings: (enabled: Bool, defaultMaxGB: Double)?
    /// The server answered that following is off.
    @State private var off = false
    @State private var editing = false
    @State private var busy = false

    private var available: Bool {
        if follow != nil || show.base.following == true { return true }
        if off { return false }
        if let settings { return settings.enabled }
        return show.base.following != nil
    }

    var body: some View {
        Group {
            if let follow {
                state(follow)
            } else if available {
                Button { editing = true } label: { Label("Follow", systemImage: "plus.circle") }
                    .buttonStyle(.bordered)
                    .disabled(busy)
                    .accessibilityIdentifier("vod.show.follow")
            }
        }
        .task(id: show.id) { await load() }
        .sheet(isPresented: $editing) {
            VodFollowSheet(show: show, existing: follow, defaultMaxGB: settings?.defaultMaxGB) { result in
                switch result {
                case let .followed(f): follow = f
                case .off: off = true
                }
            }
        }
    }

    private func state(_ f: VodFollow) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 10) {
                Label("Following", systemImage: "checkmark.circle.fill").font(.headline).foregroundStyle(Color.marqueeGold)
                Menu {
                    Button("Change", systemImage: "slider.horizontal.3") { editing = true }
                    Button("Unfollow", systemImage: "minus.circle", role: .destructive) { unfollow() }
                } label: {
                    Label("Options", systemImage: "ellipsis.circle").labelStyle(.iconOnly)
                }
                .disabled(busy)
                .accessibilityIdentifier("vod.show.followOptions")
            }
            Text(f.summary).font(.footnote).foregroundStyle(.secondary)
            Text(f.statusLabel).font(.footnote).foregroundStyle(f.isPaused ? Color.orange : Color.secondary)
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("vod.show.following")
    }

    private func load() async {
        async let s = app.vodFollowSettings()
        // The follow itself, when the show says it's followed (or might be: the list is small).
        if show.base.following != false, let list = try? await app.vodFollows() {
            follow = list.first { $0.seriesId == show.id }
        }
        settings = await s
        loaded = true
    }

    private func unfollow() {
        busy = true
        Task {
            do {
                try await app.unfollowVodSeries(show.id)
                follow = nil
            } catch {
                ActionError.shared.message = error.localizedDescription
            }
            busy = false
        }
    }
}

/// Following a show, or changing its follow: the size limit, the episodes already there (new
/// follows), and a save folder when the server has more than one (admins).
struct VodFollowSheet: View {
    enum Result { case followed(VodFollow), off }

    @Environment(AppSession.self) private var app
    @Environment(\.dismiss) private var dismiss
    let show: VodSeriesDetail
    let existing: VodFollow?
    let defaultMaxGB: Double?
    let done: (Result) -> Void
    @State private var maxGB: Double = 0
    @State private var includeExisting = false
    @State private var destination = ""
    @State private var saving = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("Size limit", selection: $maxGB) {
                        ForEach(limits, id: \.self) { gb in Text(limitLabel(gb)).tag(gb) }
                    }
                    .accessibilityIdentifier("vod.follow.limit")
                    if existing == nil {
                        Toggle("Also save the episodes already there", isOn: $includeExisting)
                            .accessibilityIdentifier("vod.follow.existing")
                    }
                    if existing == nil, let folders = show.info.saveDestinations, !folders.isEmpty {
                        Picker("Save to", selection: $destination) {
                            Text("Shows folder").tag("")
                            ForEach(folders, id: \.id) { d in Text(d.name).tag(d.name) }
                        }
                    }
                } footer: {
                    Text("New episodes are saved to the library as they appear. They count towards your recording space; once this show's saved episodes reach the size limit, new ones wait.")
                }
                if let error {
                    Section { Text(error).foregroundStyle(.red) }
                }
            }
            .navigationTitle(existing == nil ? "Follow \(show.title)" : show.title)
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button(existing == nil ? "Follow" : "Save") { save() }
                        .disabled(saving)
                        .accessibilityIdentifier("vod.follow.save")
                }
            }
        }
        .onAppear {
            maxGB = existing?.maxGb ?? defaultMaxGB ?? 0
        }
    }

    /// The choices, with the follow's own or the server's default limit among them.
    private var limits: [Double] {
        let current = existing?.maxGb ?? defaultMaxGB ?? 0
        return followLimits.contains(current) ? followLimits : (followLimits + [current]).sorted()
    }

    private func save() {
        saving = true
        error = nil
        Task {
            do {
                let f = try await app.followVodSeries(show.id, maxGB: maxGB, includeExisting: existing == nil ? includeExisting : nil,
                                                      destination: existing == nil && !destination.isEmpty ? destination : nil)
                done(.followed(f))
                dismiss()
            } catch let off as VodFollowingOff {
                done(.off)
                ActionError.shared.message = off.message
                dismiss()
            } catch {
                self.error = error.localizedDescription
            }
            saving = false
        }
    }
}

/// The shows on demand the person follows: each with its episodes, size and status; opening
/// one shows its page, and Unfollow is in its menu.
struct VodFollowingView: View {
    @Environment(AppSession.self) private var app
    @State private var follows: [VodFollow] = []
    @State private var loaded = false
    @State private var error: String?

    var body: some View {
        List {
            if let error { ErrorBanner(message: error) }
            if loaded && follows.isEmpty && error == nil {
                ContentUnavailableView("Not following any shows", systemImage: "play.tv",
                                       description: Text("Follow a show from its page and its new episodes are saved to the library."))
            }
            ForEach(follows, id: \.seriesId) { f in
                NavigationLink(value: Route.vodSeries(f.seriesId)) { row(f) }
                    .contextMenu {
                        Button("Unfollow", systemImage: "minus.circle", role: .destructive) { unfollow(f) }
                    }
                    #if os(iOS)
                    .swipeActions {
                        Button("Unfollow", role: .destructive) { unfollow(f) }
                    }
                    #endif
            }
        }
        .navigationTitle("Following")
        .task { await load() }
        .refreshable { await load() }
        .accessibilityIdentifier("vod.following")
    }

    private func row(_ f: VodFollow) -> some View {
        HStack(spacing: isTV ? 24 : 12) {
            VodArt(title: f.title, path: f.posterUrl, width: isTV ? 100 : 56)
            VStack(alignment: .leading, spacing: 3) {
                Text(f.title).font(.body.weight(.medium)).lineLimit(1)
                Text(f.summary).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                Text(f.statusLabel).font(.caption).foregroundStyle(f.isPaused ? Color.orange : Color.secondary).lineLimit(2)
            }
        }
        .padding(.vertical, 2)
    }

    private func load() async {
        do {
            follows = try await app.vodFollows().sorted { $0.title.localizedCaseInsensitiveCompare($1.title) == .orderedAscending }
            error = nil
        } catch is CancellationError {
        } catch {
            self.error = error.localizedDescription
        }
        loaded = true
    }

    private func unfollow(_ f: VodFollow) {
        Task {
            do {
                try await app.unfollowVodSeries(f.seriesId)
                follows.removeAll { $0.seriesId == f.seriesId }
            } catch {
                ActionError.shared.message = error.localizedDescription
            }
        }
    }
}

/// VOD Shows' way to the Following list, shown once the person follows something.
struct VodFollowingLink: View {
    @Environment(AppSession.self) private var app
    @State private var count = 0

    var body: some View {
        Group {
            if count > 0 {
                NavigationLink(value: Route.vodFollowing) {
                    Label("Following (\(count))", systemImage: "checkmark.circle")
                }
                #if os(iOS)
                .buttonStyle(.bordered)
                #endif
                .accessibilityIdentifier("vod.followingLink")
            }
        }
        // Again on coming back (a show may have been followed or unfollowed meanwhile).
        .onAppear { Task { count = (try? await app.vodFollows().count) ?? count } }
    }
}
