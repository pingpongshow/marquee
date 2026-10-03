import MarqueeKit
import SwiftUI

/// Library health (ADM-11, admin): checks for duplicates, unmatched titles, missing files and
/// more, each with the items it found.
struct LibraryHealthView: View {
    @Environment(AppSession.self) private var app
    @State private var checks: [HealthCheck]?
    @State private var error: String?

    var body: some View {
        List {
            if let error { ErrorBanner(message: error) }
            if let checks {
                ForEach(checks, id: \.id) { c in
                    if c.available == false {
                        row(c).opacity(0.45)
                    } else {
                        NavigationLink { HealthIssuesView(check: c) } label: { row(c) }
                    }
                }
            } else if error == nil {
                ProgressView()
            }
        }
        .navigationTitle("Library Health")
        .task { await load() }
        .refreshable { await load() }
    }

    private func row(_ c: HealthCheck) -> some View {
        HStack(alignment: .top, spacing: 12) {
            Circle().fill(c.available == false ? Color.secondary : severityColor(c.severity)).frame(width: 10, height: 10).padding(.top, 6)
            VStack(alignment: .leading, spacing: 3) {
                Text(c.title).font(.headline)
                Text(c.description).font(.caption).foregroundStyle(.secondary)
                if c.available == false { Text("Unavailable").font(.caption.weight(.semibold)).foregroundStyle(.secondary) }
            }
            Spacer()
            if c.available != false {
                Text(c.count.formatted()).font(.callout.monospacedDigit().weight(.semibold))
                    .foregroundStyle(c.count == 0 ? Color.secondary : severityColor(c.severity))
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("check-\(c.id.rawValue)")
    }

    private func load() async {
        do { checks = try await app.libraryHealth(); error = nil } catch { self.error = error.localizedDescription }
    }
}

func severityColor(_ s: HealthCheck.SeverityPayload) -> Color {
    switch s {
    case .error: .red
    case .warning: .orange
    case .info: .blue
    }
}

/// What one check found, a page at a time, with Open, Refresh Metadata, Fix Match, Bazarr and
/// Ignore (with undo). Duplicates open a side-by-side comparison of their files.
struct HealthIssuesView: View {
    @Environment(AppSession.self) private var app
    let check: HealthCheck
    @State private var issues: [HealthIssue] = []
    @State private var total = 0
    @State private var loading = false
    @State private var loaded = false
    @State private var error: String?
    @State private var notice: String?
    /// The last ignored issue, for Undo.
    @State private var ignored: (issue: HealthIssue, index: Int)?
    @State private var open: Int64?
    @State private var matching: Item?
    @State private var bazarr: Item?
    @State private var comparing: HealthIssue?

    var body: some View {
        List {
            if let error { ErrorBanner(message: error) }
            Section {
                Text(check.description).font(.footnote).foregroundStyle(.secondary)
            }
            if loaded && issues.isEmpty {
                ContentUnavailableView("Nothing to fix", systemImage: "checkmark.seal", description: Text("This check found nothing."))
            }
            ForEach(Array(issues.enumerated()), id: \.element.item.id) { i, issue in
                issueRow(issue)
                    .onAppear { if i >= issues.count - 10 { Task { await loadMore() } } }
                    .swipeActions { Button("Ignore") { ignore(issue) }.tint(.gray) }
            }
            if loading { ProgressView() }
        }
        .navigationTitle(check.title)
        .safeAreaInset(edge: .bottom) { banner }
        .navigationDestination(item: $open) { ItemDetailView(id: $0) }
        .sheet(item: $matching) { FixMatchSheet(item: $0) }
        .sheet(item: $bazarr) { BazarrSheet(item: $0) }
        .navigationDestination(item: $comparing) { DuplicateCompareView(issue: $0, onDeleted: { await reload() }) }
        .task { await loadMore() }
        .refreshable { await reload() }
    }

    private func issueRow(_ issue: HealthIssue) -> some View {
        let it = issue.item
        return HStack(alignment: .top, spacing: 12) {
            ArtworkView(item: it, shape: PosterShape.for(it) == .wide ? .wide : PosterShape.for(it), width: 54)
                .frame(width: 54)
            VStack(alignment: .leading, spacing: 3) {
                Text(it._type == .episode ? "\(it.grandparentTitle ?? "") · \(it.title)" : it.title).font(.headline).lineLimit(2)
                Text(issue.detail).font(.callout).foregroundStyle(.secondary)
                if let path = issue.path { Text(path).font(.caption2.monospaced()).foregroundStyle(.tertiary).lineLimit(2) }
                if let others = issue.related, !others.isEmpty {
                    Text("Other copies: " + others.map { [$0.title, $0.year.map(String.init)].compactMap { $0 }.joined(separator: " ") }.joined(separator: ", "))
                        .font(.caption).foregroundStyle(.secondary)
                }
                if check.id == .duplicates, let files = issue.files, files.count > 1 {
                    Button("Compare \(files.count) Files", systemImage: "rectangle.split.3x1") { comparing = issue }
                        .buttonStyle(.borderless).font(.callout.weight(.semibold))
                        .accessibilityIdentifier("compare-\(it.id)")
                        .padding(.top, 2)
                }
            }
            Spacer(minLength: 0)
            Menu {
                Button("Open", systemImage: "arrow.up.right.square") { open = it.id }
                Button("Refresh Metadata", systemImage: "arrow.clockwise") { refresh(it) }
                if [.movie, .show].contains(it._type) {
                    Button("Fix Match…", systemImage: "wand.and.stars") { matching = it }
                }
                if check.id == .missingSubtitles {
                    Button("Download with Bazarr…", systemImage: "captions.bubble") { bazarr = it }
                }
                Button("Ignore", systemImage: "eye.slash") { ignore(issue) }
            } label: {
                Image(systemName: "ellipsis.circle").font(.title3).frame(width: 36, height: 36)
            }
            .accessibilityLabel("Actions for \(it.title)")
        }
        .padding(.vertical, 2)
    }

    @ViewBuilder private var banner: some View {
        if let ignored {
            HStack {
                Text("Ignored \(ignored.issue.item.title)").lineLimit(1)
                Spacer()
                Button("Undo") { undo() }.bold()
            }
            .padding()
            .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 12))
            .padding()
        } else if let notice {
            Text(notice).padding().frame(maxWidth: .infinity).background(.regularMaterial, in: RoundedRectangle(cornerRadius: 12)).padding()
        }
    }

    private func reload() async {
        issues = []
        total = 0
        loaded = false
        await loadMore()
    }

    private func loadMore() async {
        guard !loading, !loaded || issues.count < total else { return }
        loading = true
        defer { loading = false }
        do {
            let page = try await app.healthIssues(check.id.rawValue, offset: issues.count)
            let have = Set(issues.map(\.item.id))
            issues += page.items.filter { !have.contains($0.item.id) }
            total = page.total
            loaded = true
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func ignore(_ issue: HealthIssue) {
        guard let i = issues.firstIndex(where: { $0.item.id == issue.item.id }) else { return }
        issues.remove(at: i)
        total -= 1
        ignored = (issue, i)
        notice = nil
        Task {
            do {
                try await app.setHealthIgnored(check.id.rawValue, itemID: issue.item.id, true)
            } catch {
                self.error = error.localizedDescription
                issues.insert(issue, at: min(i, issues.count))
                ignored = nil
            }
        }
    }

    private func undo() {
        guard let ig = ignored else { return }
        let (issue, i) = (ig.issue, ig.index)
        ignored = nil
        Task {
            do {
                try await app.setHealthIgnored(check.id.rawValue, itemID: issue.item.id, false)
                issues.insert(issue, at: min(i, issues.count))
                total += 1
            } catch {
                self.error = error.localizedDescription
            }
        }
    }

    private func refresh(_ it: Item) {
        Task {
            do {
                try await app.refreshMetadata(it.id)
                notice = "Refreshed \(it.title)."
                error = nil
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}

/// Fix Match (admin): search the metadata provider and pick the right entry.
struct FixMatchSheet: View {
    @Environment(AppSession.self) private var app
    @Environment(\.dismiss) private var dismiss
    let item: Item
    @State private var title = ""
    @State private var year = ""
    @State private var results: [MatchCandidate]?
    @State private var busy = false
    @State private var error: String?
    /// The item's files (admins get paths), with the version label when there are several.
    @State private var files: [(path: String, version: String?)] = []

    var body: some View {
        NavigationStack {
            List {
                if !files.isEmpty {
                    Section(files.count == 1 ? "File" : "Files") {
                        ForEach(files, id: \.path) { f in FilePathLabel(path: f.path, caption: f.version) }
                    }
                }
                Section {
                    TextField("Title", text: $title).autocorrectionDisabled()
                    TextField("Year", text: $year).keyboardType(.numberPad)
                    Button("Search") { Task { await search() } }.disabled(busy || title.trimmingCharacters(in: .whitespaces).isEmpty)
                }
                if let error { Text(error).foregroundStyle(.red) }
                if let results {
                    Section("Matches") {
                        if results.isEmpty { Text("No matches.").foregroundStyle(.secondary) }
                        ForEach(results, id: \.id) { c in
                            Button { apply(c) } label: {
                                HStack(alignment: .top, spacing: 12) {
                                    AsyncImage(url: c.posterUrl.flatMap(URL.init(string:))) { $0.image?.resizable().scaledToFill() }
                                        .frame(width: 44, height: 66).background(Color.secondary.opacity(0.2)).clipShape(RoundedRectangle(cornerRadius: 4))
                                    VStack(alignment: .leading, spacing: 3) {
                                        Text([c.title, c.year.map { "(\($0))" }].compactMap { $0 }.joined(separator: " ")).foregroundStyle(.primary)
                                        if let o = c.overview { Text(o).font(.caption).foregroundStyle(.secondary).lineLimit(3) }
                                        if c.current == true { Text("Current match").font(.caption.bold()).foregroundStyle(Color.marqueeGold) }
                                    }
                                }
                            }
                            .disabled(busy)
                        }
                    }
                } else if busy {
                    ProgressView()
                }
            }
            .navigationTitle("Fix Match")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } } }
            .task {
                title = item.title
                year = item.year.map(String.init) ?? ""
                async let detail = try? app.item(item.id)
                await search()
                if let versions = await detail?.info.versions {
                    let many = versions.count > 1
                    files = versions.flatMap { v in v.files.compactMap { f in f.path.map { ($0, many ? v.label : nil) } } }
                }
            }
        }
    }

    private func search() async {
        busy = true
        defer { busy = false }
        do {
            results = try await app.matchCandidates(item.id, title: title, year: Int(year))
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func apply(_ c: MatchCandidate) {
        busy = true
        Task {
            defer { busy = false }
            do {
                try await app.applyMatch(item.id, c)
                dismiss()
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}

/// Integrations (admin): Seerr for requests and Bazarr for subtitles (META-12). API keys are
/// write-only: the field shows whether one is set; typing a new one replaces it.
struct IntegrationsView: View {
    @Environment(AppSession.self) private var app
    @State private var seerrURL = ""
    @State private var seerrKey = ""
    @State private var seerrKeySet = false
    @State private var bazarrURL = ""
    @State private var bazarrKey = ""
    @State private var bazarrKeySet = false
    @State private var loaded = false
    @State private var message: String?
    @State private var error: String?

    var body: some View {
        Form {
            if let error { ErrorBanner(message: error) }
            Section {
                TextField("Address, e.g. http://10.1.1.10:5055", text: $seerrURL)
                    .keyboardType(.URL).textInputAutocapitalization(.never).autocorrectionDisabled()
                SecureField(seerrKeySet ? "API key (set)" : "API key", text: $seerrKey)
            } header: {
                Text("Seerr (requests)")
            }
            Section {
                TextField("Address, e.g. http://10.1.1.10:6767", text: $bazarrURL)
                    .keyboardType(.URL).textInputAutocapitalization(.never).autocorrectionDisabled()
                    .accessibilityIdentifier("bazarrURL")
                SecureField(bazarrKeySet ? "API key (set)" : "API key", text: $bazarrKey)
                    .accessibilityIdentifier("bazarrKey")
            } header: {
                Text("Bazarr (subtitles)")
            } footer: {
                Text("Bazarr's API key is in Bazarr under Settings → General. Leave a key empty to keep the one that's set. An empty address turns the integration off.")
            }
            Section {
                Button("Save") { save() }.disabled(!loaded)
                if let message { Text(message).foregroundStyle(.secondary) }
            }
        }
        .navigationTitle("Integrations")
        .task { await load() }
    }

    private func load() async {
        do {
            let s = try await app.integrationSettings()
            seerrURL = s.seerrUrl ?? ""
            seerrKeySet = s.seerrApiKeySet ?? false
            bazarrURL = s.bazarrUrl ?? ""
            bazarrKeySet = s.bazarrApiKeySet ?? false
            loaded = true
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func save() {
        message = nil
        Task {
            do {
                try await app.saveIntegrations(seerrURL: seerrURL, seerrKey: seerrKey.isEmpty ? nil : seerrKey,
                                               bazarrURL: bazarrURL, bazarrKey: bazarrKey.isEmpty ? nil : bazarrKey)
                seerrKey = ""
                bazarrKey = ""
                await load()
                message = "Saved."
                error = nil
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}
