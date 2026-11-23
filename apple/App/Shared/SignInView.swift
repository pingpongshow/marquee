import MarqueeKit
import SwiftUI

/// "Who's watching?" profile picker with PINs, password sign-in and Quick Connect.
struct SignInView: View {
    @Environment(AppSession.self) private var app
    @State private var profiles: [Schemas.Profile] = []
    @State private var chosen: Schemas.Profile?
    /// The profile picked that signs in with a password: its name goes in the username field,
    /// and it signs in as that profile (its username may be something else).
    @State private var passwordProfile: Schemas.Profile?
    @State private var mode: Mode = .profiles
    @State private var error: String?
    @State private var busy = false

    enum Mode { case profiles, password, quickConnect }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(spacing: 28) {
                    VStack(spacing: 6) {
                        Image(systemName: "film.stack").font(.system(size: 40)).foregroundStyle(Color.marqueeGold)
                        Text(app.server?.name ?? "Marquee").font(.title2.bold())
                    }
                    if let error { ErrorBanner(message: error).frame(maxWidth: 520) }
                    if let lastError = app.lastError { ErrorBanner(message: lastError).frame(maxWidth: 520) }
                    switch mode {
                    case .profiles: profilePicker
                    case .password: PasswordForm(profile: passwordProfile, onError: { error = $0 }).id(passwordProfile?.id)
                    case .quickConnect: QuickConnectView(onError: { error = $0 })
                    }
                    modeButtons
                }
                .padding(.vertical, 40)
                .padding(.horizontal, sidePadding)
                .frame(maxWidth: .infinity)
            }
            .task { await load() }
            .sheet(item: $chosen) { p in
                PinEntry(profile: p) { pin in await pinSignIn(p, pin: pin) }
                    .macSheetFrame(420, 640)
            }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Servers") { app.forgetServer() }
                }
            }
        }
    }

    private func load() async {
        profiles = (try? await app.profiles()) ?? []
        #if os(tvOS)
        if profiles.isEmpty { mode = .quickConnect }
        #else
        if profiles.isEmpty { mode = .password }
        #endif
    }

    @ViewBuilder private var profilePicker: some View {
        if profiles.isEmpty {
            Text("Profiles can't be picked from this network. Sign in with your password or Quick Connect.").foregroundStyle(.secondary).multilineTextAlignment(.center)
        } else {
            Text("Who's watching?").font(.title.bold())
            LazyVGrid(columns: [GridItem(.adaptive(minimum: avatarSize + 30), spacing: 24)], spacing: 24) {
                ForEach(profiles, id: \.id) { p in
                    Button {
                        if p.requires == .none { Task { await pinSignIn(p, pin: nil) } } else { ask(p) }
                    } label: {
                        VStack(spacing: 8) {
                            AvatarView(name: p.displayName, url: p.avatarUrl, size: avatarSize)
                                .overlay(alignment: .bottomTrailing) {
                                    if p.requires != .none { Image(systemName: "lock.fill").font(.caption).padding(5).background(.thinMaterial, in: Circle()) }
                                }
                            Text(p.displayName).lineLimit(1)
                        }
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel(p.displayName)
                    .disabled(busy)
                }
            }
            .frame(maxWidth: 720)
        }
    }

    private var modeButtons: some View {
        VStack(spacing: 12) {
            if mode != .profiles && !profiles.isEmpty { Button("Choose a profile") { mode = .profiles } }
            if mode != .password { Button("Sign in with username and password") { mode = .password } }
            if mode != .quickConnect { Button("Use Quick Connect") { mode = .quickConnect } }
        }
        .font(.callout)
    }

    #if os(tvOS)
    private let avatarSize: CGFloat = 160
    #else
    private let avatarSize: CGFloat = 76
    #endif

    /// The PIN pad or the password form for a profile that needs one.
    private func ask(_ p: Schemas.Profile) {
        if p.requires == .password {
            chosen = nil
            passwordProfile = p
            mode = .password
        } else {
            chosen = p
        }
    }

    /// Signs in as the profile. A refused PIN's reason is returned for the PIN pad to show
    /// (the page's own banner is behind its sheet); anything else shows on the page.
    @discardableResult
    private func pinSignIn(_ p: Schemas.Profile, pin: String?) async -> String? {
        busy = true
        defer { busy = false }
        do {
            try await app.pinSignIn(userID: p.id, pin: pin)
            chosen = nil
            return nil
        } catch {
            if pin != nil { return error.localizedDescription }
            // A profile that opened without a PIN when the list was loaded (at home) and was
            // refused: the list is old (the device has left home since, or the profile changed).
            // With a fresh one it asks for what the profile needs now, rather than refusing
            // every tap until the server locks the profile.
            if let fresh = try? await app.profiles() {
                profiles = fresh
                if let now = fresh.first(where: { $0.id == p.id }), now.requires != .none {
                    self.error = nil
                    ask(now)
                    return nil
                }
                if fresh.isEmpty { mode = isTV ? .quickConnect : .password }
            }
            self.error = error.localizedDescription
            return nil
        }
    }
}


/// A profile's PIN: its picture and name in a row above a compact keypad, in a sheet only as
/// tall as it needs. A refused PIN shows why on the pad and it's ready for the next try.
struct PinEntry: View {
    @Environment(\.dismiss) private var dismiss
    let profile: Schemas.Profile
    /// Sends the PIN; returns why it was refused, or nil.
    let submit: (String) async -> String?
    @State private var busy = false
    @State private var error: String?
    @State private var failures = 0

    var body: some View {
        VStack(spacing: isTV ? 30 : 18) {
            HStack(spacing: 12) {
                AvatarView(name: profile.displayName, url: profile.avatarUrl, size: isTV ? 72 : 40)
                Text(profile.displayName).font(isTV ? .title2.bold() : .headline).lineLimit(1)
            }
            .accessibilityElement(children: .combine)
            .accessibilityLabel("Enter \(profile.displayName)'s PIN")
            PinPad(disabled: busy, error: error, failures: failures) { pin in
                busy = true
                error = nil
                Task {
                    let refused = await submit(pin)
                    busy = false
                    if let refused {
                        error = refused
                        failures += 1
                    }
                }
            }
            #if os(macOS)
            // A Mac's sheet has no swipe to close it (the TV's Back closes it).
            Button("Cancel", role: .cancel) { dismiss() }
                .keyboardShortcut(.cancelAction)
            #endif
        }
        .padding(isTV ? 50 : 24)
        #if os(iOS)
        .presentationDetents([.height(440)])
        .presentationDragIndicator(.visible)
        #endif
    }
}

private struct PasswordForm: View {
    @Environment(AppSession.self) private var app
    /// The profile picked on Who's watching?, if any: its name is filled in.
    var profile: Schemas.Profile?
    let onError: (String) -> Void
    @State private var username = ""
    @State private var password = ""
    @State private var code = ""
    @State private var needsCode = false
    @State private var busy = false

    var body: some View {
        VStack(spacing: 14) {
            TextField("Username", text: $username)
                .textContentType(.username)
                #if !os(tvOS)
                .textInputAutocapitalization(.never)
                #endif
                .autocorrectionDisabled()
            SecureField("Password", text: $password).textContentType(.password)
            if needsCode {
                TextField("Authenticator code", text: $code)
                    .textContentType(.oneTimeCode)
                    #if !os(tvOS)
                    .keyboardType(.numberPad)
                    #endif
                    .accessibilityIdentifier("totpCode")
            }
            Button {
                busy = true
                Task {
                    do {
                        if let profile, username == profile.displayName {
                            // The profile that was picked, by its id: the name filled in is its
                            // display name, and the username it signs in with may be another.
                            try await app.pinSignIn(userID: profile.id, pin: nil, password: password, totpCode: needsCode ? code : nil)
                        } else {
                            try await app.signIn(username: username, password: password, totpCode: needsCode ? code : nil)
                        }
                    } catch is TwoFactorRequired {
                        needsCode = true
                        onError("Enter the code from your authenticator app, or a recovery code.")
                    } catch { onError(error.localizedDescription) }
                    busy = false
                }
            } label: {
                if busy { ProgressView() } else { Text("Sign In").frame(maxWidth: .infinity) }
            }
            .buttonStyle(.borderedProminent)
            .disabled(username.isEmpty || password.isEmpty || (needsCode && code.isEmpty) || busy)
        }
        #if !os(tvOS)
        .textFieldStyle(.roundedBorder)
        #endif
        .frame(maxWidth: 420)
        .onAppear { if let profile { username = profile.displayName } }
    }
}

/// Shows a code to approve from another device, and signs in when it's approved.
struct QuickConnectView: View {
    @Environment(AppSession.self) private var app
    let onError: (String) -> Void
    @State private var start: Schemas.QuickConnectStart?

    var body: some View {
        VStack(spacing: 16) {
            Text("Quick Connect").font(.title.bold())
            if let start {
                Text(start.code.map { String($0) }.joined(separator: " "))
                    .font(.system(size: codeSize, weight: .bold, design: .monospaced))
                    .padding(.horizontal, 30).padding(.vertical, 14)
                    .background(Color.secondary.opacity(0.15), in: RoundedRectangle(cornerRadius: 16))
                Text("On your phone or computer, open Marquee → Account → Link a device (or go to \(linkAddress)) and enter this code.")
                    .multilineTextAlignment(.center).foregroundStyle(.secondary).frame(maxWidth: 640)
            } else {
                ProgressView()
            }
        }
        .task { await run() }
    }

    #if os(tvOS)
    private let codeSize: CGFloat = 90
    #else
    private let codeSize: CGFloat = 44
    #endif

    private var linkAddress: String {
        guard let url = app.baseURL else { return "/link" }
        return (url.host() ?? "") + (url.port.map { ":\($0)" } ?? "") + "/link"
    }

    private func run() async {
        while !Task.isCancelled {
            do {
                let s = try await app.startQuickConnect()
                start = s
                while !Task.isCancelled, Date() < s.expiresAt {
                    try await Task.sleep(for: .milliseconds(s.pollIntervalMs))
                    if try await app.pollQuickConnect(secret: s.secret) { return }
                }
            } catch is CancellationError {
                return
            } catch {
                onError(error.localizedDescription)
                try? await Task.sleep(for: .seconds(3))
            }
        }
    }
}
