import MarqueeKit
import SwiftUI

/// "Who's watching?" profile picker with PINs, password sign-in and Quick Connect.
struct SignInView: View {
    @Environment(AppSession.self) private var app
    @State private var profiles: [Schemas.Profile] = []
    @State private var chosen: Schemas.Profile?
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
                    case .password: PasswordForm(prefill: chosen?.displayName, onError: { error = $0 })
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
                        if p.requires == .none { Task { await pinSignIn(p, pin: nil) } } else if p.requires == .password { chosen = nil; mode = .password } else { chosen = p }
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

    private func pinSignIn(_ p: Schemas.Profile, pin: String?) async {
        busy = true
        defer { busy = false }
        do {
            try await app.pinSignIn(userID: p.id, pin: pin)
            chosen = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}


private struct PinEntry: View {
    let profile: Schemas.Profile
    let submit: (String) async -> Void
    @State private var busy = false
    var body: some View {
        VStack(spacing: 24) {
            AvatarView(name: profile.displayName, url: profile.avatarUrl, size: 80)
            Text("Enter \(profile.displayName)'s PIN").font(.title3.bold())
            PinPad(disabled: busy) { pin in
                busy = true
                Task { await submit(pin); busy = false }
            }
        }
        .padding(32)
        .presentationDetents([.medium, .large])
    }
}

private struct PasswordForm: View {
    @Environment(AppSession.self) private var app
    var prefill: String?
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
                #if os(iOS)
                .textInputAutocapitalization(.never)
                #endif
                .autocorrectionDisabled()
            SecureField("Password", text: $password).textContentType(.password)
            if needsCode {
                TextField("Authenticator code", text: $code)
                    .textContentType(.oneTimeCode)
                    #if os(iOS)
                    .keyboardType(.numberPad)
                    #endif
                    .accessibilityIdentifier("totpCode")
            }
            Button {
                busy = true
                Task {
                    do {
                        try await app.signIn(username: username, password: password, totpCode: needsCode ? code : nil)
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
        #if os(iOS)
        .textFieldStyle(.roundedBorder)
        #endif
        .frame(maxWidth: 420)
        .onAppear { if let prefill { username = prefill } }
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
