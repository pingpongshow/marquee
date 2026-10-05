import MarqueeKit
import SwiftUI

struct SettingsView: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    @State private var local = QualityPreference.local
    @State private var remote = QualityPreference.remote
    @State private var switching = false
    @State private var linkCode = ""
    @State private var linkResult: String?
    @State private var trailersError: String?

    var body: some View {
        Form {
            Section("Signed in") {
                HStack(spacing: 14) {
                    AvatarView(name: app.me?.displayName ?? "?", url: app.me?.avatarUrl, size: 48)
                    VStack(alignment: .leading) {
                        Text(app.me?.displayName ?? "").font(.headline)
                        Text("@\(app.me?.username ?? "")").font(.caption).foregroundStyle(.secondary)
                    }
                }
                NavigationLink("Your Stats", value: Route.settings(.stats))
                if app.me?.isAdmin == true {
                    NavigationLink("Requests", value: Route.settings(.requests))
                    NavigationLink("Users and Friends", value: Route.settings(.users))
                    NavigationLink("Cinema Trailers", value: Route.settings(.cinema))
                    #if os(iOS)
                    NavigationLink("Library Settings", value: Route.settings(.librarySettings))
                    NavigationLink("Library Health", value: Route.settings(.libraryHealth))
                    NavigationLink("Integrations", value: Route.settings(.integrations))
                    #endif
                }
                NavigationLink("Edit Home", value: Route.settings(.editHome))
                NavigationLink("Subtitle Appearance", value: Route.settings(.subtitleAppearance))
                #if os(iOS)
                NavigationLink("Remote Control", value: Route.settings(.remotePlayers))
                #endif
                Button("Switch Profile") { switching = true }
                Button("Sign Out", role: .destructive) { Task { await app.signOut() } }
            }
            PendingSyncSection()
            Section {
                LabeledContent("Server", value: app.server?.name ?? "")
                LabeledContent("Connected via", value: app.isRemote ? "Tailscale (away)" : "Home network")
                if let u = app.server?.lanURL { LabeledContent("Home address", value: u.absoluteString) }
                if let u = app.server?.remoteURL { LabeledContent("Away address", value: u.absoluteString) }
                LabeledContent("Server version", value: app.info?.version ?? "")
                Button(app.isReconnecting ? "Reconnecting…" : "Reconnect") { Task { await app.reconnect() } }
                    .disabled(app.isReconnecting)
            } header: {
                Text("Server")
            }
            Section {
                Picker("At home", selection: $local) {
                    ForEach(QualityPreference.options, id: \.kbps) { o in Text(o.kbps == 0 ? "Original" : o.label).tag(o.kbps) }
                }
                .onChange(of: local) { QualityPreference.local = local }
                Picker("Away from home", selection: $remote) {
                    ForEach(QualityPreference.options, id: \.kbps) { o in Text(o.kbps == 0 ? "Automatic" : o.label).tag(o.kbps) }
                }
                .onChange(of: remote) { QualityPreference.remote = remote }
            } header: {
                Text("Video quality")
            } footer: {
                Text("Original plays files untouched when this device supports them. Automatic picks the best quality your connection allows.")
            }
            Section {
                Toggle("Play trailers before movies", isOn: Binding(get: { app.cinemaTrailersPreference }, set: { on in
                    Task {
                        do { try await app.setCinemaTrailers(on); trailersError = nil } catch { trailersError = error.localizedDescription }
                    }
                }))
                if let trailersError { Text(trailersError).font(.caption).foregroundStyle(.red) }
            } footer: {
                Text("When the server has cinema trailers on, they play before a movie you start from the beginning.")
            }
            Section {
                @Bindable var music = music
                Toggle("Show audio quality", isOn: $music.showAudioQuality)
                    .accessibilityIdentifier("showAudioQuality")
            } header: {
                Text("Music")
            } footer: {
                Text("Audio quality shows each track's format, such as FLAC · 24-bit/96 kHz, in Now Playing and track lists.")
            }
            #if os(iOS)
            Section {
                HStack {
                    TextField("Code shown on the TV", text: $linkCode)
                        .textInputAutocapitalization(.characters).autocorrectionDisabled()
                        .font(.body.monospaced())
                    Button("Link") {
                        Task {
                            do { linkResult = "\(try await app.approveQuickConnect(code: linkCode)) is signed in."; linkCode = "" } catch { linkResult = error.localizedDescription }
                        }
                    }
                    .disabled(linkCode.replacingOccurrences(of: " ", with: "").count != 6)
                }
                if let linkResult { Text(linkResult).font(.caption).foregroundStyle(.secondary) }
            } header: {
                Text("Link a TV")
            } footer: {
                Text("On your Apple TV, choose Quick Connect and enter its code here.")
            }
            #endif
            Section {
                Button("Use a Different Server", role: .destructive) { app.forgetServer() }
            } footer: {
                Text("Marquee \(Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "")")
            }
        }
        .navigationTitle("Settings")
        .sheet(isPresented: $switching) { ProfileSwitcher() }
    }
}

/// Switch to another profile on this device.
struct ProfileSwitcher: View {
    @Environment(AppSession.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var profiles: [Schemas.Profile] = []
    @State private var target: Schemas.Profile?
    @State private var error: String?

    var body: some View {
        NavigationStack {
            VStack(spacing: 24) {
                if let error { ErrorBanner(message: error) }
                if let target {
                    AvatarView(name: target.displayName, url: target.avatarUrl, size: 80)
                    Text("Enter \(target.displayName)'s PIN").font(.headline)
                    PinPad { pin in switchTo(target, pin: pin) }
                } else {
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: 100), spacing: 20)], spacing: 20) {
                        ForEach(profiles, id: \.id) { p in
                            Button {
                                if p.requires == .pin && app.me?.isAdmin != true { target = p } else { switchTo(p, pin: nil) }
                            } label: {
                                VStack {
                                    AvatarView(name: p.displayName, url: p.avatarUrl, size: 72)
                                    Text(p.displayName).lineLimit(1)
                                    if p.id == app.me?.id { Text("Current").font(.caption2).foregroundStyle(Color.marqueeGold) }
                                }
                            }
                            .buttonStyle(.plain)
                        }
                    }
                }
                Spacer()
            }
            .padding()
            .navigationTitle("Switch Profile")
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } } }
            .task { profiles = (try? await app.switchableProfiles()) ?? [] }
        }
    }

    private func switchTo(_ p: Schemas.Profile, pin: String?) {
        Task {
            do {
                try await app.switchProfile(to: p.id, pin: pin, password: nil)
                dismiss()
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}
