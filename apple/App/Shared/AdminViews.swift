import MarqueeKit
import SwiftUI

/// Cinema trailers (PLAY-18), admin: how many trailers play before a movie started from the
/// beginning, and an optional pre-roll video after them.
struct CinemaSettingsView: View {
    @Environment(AppSession.self) private var app
    @State private var trailers = 0
    @State private var prerollID: Int64 = 0
    @State private var prerollTitle: String?
    @State private var query = ""
    @State private var results: [Item] = []
    @State private var idText = ""
    @State private var loaded = false
    @State private var message: String?
    @State private var error: String?

    var body: some View {
        Form {
            if let error { ErrorBanner(message: error) }
            Section {
                Picker("Trailers before movies", selection: $trailers) {
                    Text("Off").tag(0)
                    ForEach(1...5, id: \.self) { Text("\($0)").tag($0) }
                }
            } footer: {
                Text("Trailers of other movies in your libraries play before a movie started from the beginning. People can turn them off in their own settings.")
            }
            Section {
                if prerollID > 0 {
                    LabeledContent("Pre-roll", value: prerollTitle ?? "Item \(prerollID)")
                    Button("Clear Pre-roll", role: .destructive) { prerollID = 0; prerollTitle = nil }
                }
                TextField("Search for a video", text: $query)
                    .autocorrectionDisabled()
                ForEach(results, id: \.id) { r in
                    Button {
                        prerollID = r.id
                        prerollTitle = r.title
                        query = ""
                    } label: {
                        VStack(alignment: .leading) {
                            Text(r.title)
                            Text(r.subtitle).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }
                HStack {
                    TextField("Or enter an item id", text: $idText)
                        #if os(iOS)
                        .keyboardType(.numberPad)
                        #endif
                    Button("Use") { useID() }.disabled(Int64(idText) == nil)
                }
            } header: {
                Text("Pre-roll video")
            } footer: {
                Text("A video such as a cinema intro, played after the trailers and before the movie.")
            }
            Section {
                Button("Save") { save() }.disabled(!loaded)
                if let message { Text(message).foregroundStyle(.secondary) }
            }
        }
        .navigationTitle("Cinema Trailers")
        .task { await load() }
        .task(id: query) {
            let q = query.trimmingCharacters(in: .whitespaces)
            guard q.count > 1 else { results = []; return }
            try? await Task.sleep(for: .milliseconds(300))
            guard !Task.isCancelled else { return }
            let found = (try? await app.search(q, limit: 10))?.groups.flatMap(\.items) ?? []
            results = Array(found.filter(\.isPlayableVideo).prefix(8))
        }
    }

    private func load() async {
        do {
            let c = try await app.cinemaSettings()
            trailers = c.trailers ?? 0
            prerollID = c.prerollItemId ?? 0
            if prerollID > 0 { prerollTitle = try? await app.item(prerollID).title }
            loaded = true
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func useID() {
        guard let id = Int64(idText) else { return }
        Task {
            do {
                prerollTitle = try await app.item(id).title
                prerollID = id
                idText = ""
                error = nil
            } catch {
                self.error = "There's no item \(id)."
            }
        }
    }

    private func save() {
        message = nil
        Task {
            do {
                try await app.saveCinemaSettings(trailers: trailers, prerollItemID: prerollID)
                message = "Saved."
                error = nil
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}

/// Users (admin): the household and friends (USER-13), and invites for friends.
struct UsersView: View {
    @Environment(AppSession.self) private var app
    @State private var users: [User] = []
    @State private var invites: [Invite] = []
    @State private var inviting = false
    @State private var error: String?

    var body: some View {
        Form {
            if let error { ErrorBanner(message: error) }
            Section("Household") {
                ForEach(users.filter { $0.restrictions.friend != true }, id: \.id) { userRow($0) }
            }
            Section {
                let friends = users.filter { $0.restrictions.friend == true }
                if friends.isEmpty { Text("No friends yet.").foregroundStyle(.secondary) }
                ForEach(friends, id: \.id) { userRow($0) }
                Button { inviting = true } label: { Label("Invite a Friend", systemImage: "person.badge.plus") }
            } header: {
                Text("Friends")
            } footer: {
                Text("Friends join with a link, sign in with their own username and password, and don't appear on this server's profile picker.")
            }
            if !invites.isEmpty {
                Section("Invites") {
                    ForEach(invites, id: \.id) { inviteRow($0) }
                }
            }
        }
        .navigationTitle("Users")
        .task { await load() }
        .refreshable { await load() }
        .sheet(isPresented: $inviting, onDismiss: { Task { await load() } }) { InviteSheet() }
    }

    private func userRow(_ u: User) -> some View {
        HStack(spacing: 12) {
            AvatarView(name: u.displayName, url: u.avatarUrl, size: 36)
            VStack(alignment: .leading) {
                Text(u.displayName)
                Text(u.isAdmin ? "Admin" : u.isManaged ? "Managed profile" : "@\(u.username)").font(.caption).foregroundStyle(.secondary)
            }
        }
    }

    private func inviteRow(_ i: Invite) -> some View {
        let note = (i.note?.isEmpty == false ? i.note : nil) ?? "Invite"
        return HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(note)
                Group {
                    switch i.status {
                    case .pending: Text(i.expiresAt.map { "Pending · expires \($0.formatted(date: .abbreviated, time: .omitted))" } ?? "Pending")
                    case .used(let who): Text("Used by \(who)")
                    case .expired: Text("Expired")
                    }
                }
                .font(.caption).foregroundStyle(.secondary)
            }
            Spacer()
            Button(role: .destructive) { delete(i) } label: { Image(systemName: "trash") }
                #if os(iOS)
                .buttonStyle(.borderless)
                #endif
                .accessibilityLabel("Delete invite \(note)")
        }
    }

    private func load() async {
        do {
            users = try await app.users()
            invites = try await app.invites().sorted { $0.createdAt > $1.createdAt }
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func delete(_ i: Invite) {
        Task {
            do {
                try await app.deleteInvite(i.id)
                invites.removeAll { $0.id == i.id }
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}

/// Invite a friend (USER-13): what they may see, then a link to send.
struct InviteSheet: View {
    @Environment(AppSession.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var note = ""
    @State private var days = 7
    @State private var libraries: [Library] = []
    @State private var allLibraries = true
    @State private var chosen: Set<Int64> = []
    @State private var maxRating: UserRestrictions.MaxContentRatingPayload?
    @State private var allowRemote = true
    @State private var canRequest = false
    @State private var link: URL?
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Form {
                if let error { ErrorBanner(message: error) }
                if let link {
                    created(link)
                } else {
                    form
                }
            }
            .navigationTitle("Invite a Friend")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(link == nil ? "Cancel" : "Done") { dismiss() } }
            }
            .task { libraries = (try? await app.libraries()) ?? [] }
        }
    }

    @ViewBuilder private var form: some View {
        Section {
            TextField("Note (who it's for)", text: $note)
            #if os(iOS)
            Stepper("Link expires in \(days) day\(days == 1 ? "" : "s")", value: $days, in: 1...90)
            #else
            Picker("Link expires in", selection: $days) {
                ForEach([1, 3, 7, 14, 30, 90], id: \.self) { Text("\($0) day\($0 == 1 ? "" : "s")").tag($0) }
            }
            #endif
        }
        Section("Libraries") {
            Toggle("All libraries", isOn: $allLibraries)
            if !allLibraries {
                ForEach(libraries, id: \.id) { lib in
                    Toggle(lib.name, isOn: Binding(get: { chosen.contains(lib.id) },
                                                   set: { if $0 { chosen.insert(lib.id) } else { chosen.remove(lib.id) } }))
                }
            }
        }
        Section {
            Picker("Maximum rating", selection: $maxRating) {
                Text("No limit").tag(UserRestrictions.MaxContentRatingPayload?.none)
                ForEach(UserRestrictions.MaxContentRatingPayload.allCases.filter { $0 != ._empty_ }, id: \.self) {
                    Text($0.rawValue).tag(UserRestrictions.MaxContentRatingPayload?.some($0))
                }
            }
            Toggle("Allow streaming away from home", isOn: $allowRemote)
            Toggle("Can request titles", isOn: $canRequest)
        }
        Section {
            Button { create() } label: {
                if busy { ProgressView() } else { Text("Create Invite") }
            }
            .disabled(busy || (!allLibraries && chosen.isEmpty))
        }
    }

    @ViewBuilder private func created(_ link: URL) -> some View {
        Section {
            Text(link.absoluteString).font(.callout.monospaced())
                #if os(iOS)
                .textSelection(.enabled)
                #endif
                .accessibilityIdentifier("inviteLink")
            #if os(iOS)
            ShareLink(item: link, subject: Text("Join me on \(app.server?.name ?? "Marquee")"),
                      message: Text("Here's an invite to my Marquee server.")) {
                Label("Share Link", systemImage: "square.and.arrow.up")
            }
            #endif
        } header: {
            Text("Send this link")
        } footer: {
            Text("The link must use an address your friend can reach (for example your Tailscale or public address). It works once.")
        }
    }

    private func create() {
        busy = true
        error = nil
        var r = UserRestrictions()
        r.libraryIds = allLibraries ? nil : Array(chosen).sorted()
        r.maxContentRating = maxRating
        r.allowRemote = allowRemote
        r.canRequest = canRequest
        Task {
            defer { busy = false }
            do {
                link = try await app.createInvite(note: note.trimmingCharacters(in: .whitespaces), expiresDays: days, restrictions: r).link
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}
