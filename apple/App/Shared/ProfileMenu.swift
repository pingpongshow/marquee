import MarqueeKit
import SwiftUI
import UIKit

/// The household's profiles to switch to directly (the current one marked), with Account
/// Settings and Sign Out: the profile entry above Home in the sidebar (iPad, Apple TV), the
/// avatar button on Home (iPhone), and Settings → Switch Profile.
///
/// Switching asks for what the profile needs: nothing, its PIN (not for an admin switching to a
/// managed profile) or its password. Away from home a two-factor account can't be switched to;
/// the server says so and the message shows.
struct ProfilesPage: View {
    @Environment(AppSession.self) private var app
    /// Account Settings and Sign Out are shown when set (not from Settings, which has both).
    var onAccountSettings: (() -> Void)?
    var showsSignOut = false
    /// After a switch (a sheet closes).
    var onSwitched: (() -> Void)?
    @State private var profiles: [Schemas.Profile] = []
    @State private var loaded = false
    @State private var target: Schemas.Profile?
    @State private var password = ""
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        ScrollView {
            VStack(spacing: 28) {
                if let error { ErrorBanner(message: error) }
                if let target {
                    entry(target)
                } else {
                    picker
                    if onAccountSettings != nil || showsSignOut { accountButtons }
                }
            }
            .padding(isTV ? 60 : 20)
            .frame(maxWidth: .infinity)
        }
        .task(id: app.me?.id) {
            profiles = (try? await app.switchableProfiles()) ?? []
            loaded = true
        }
    }

    @ViewBuilder private var picker: some View {
        Text("Who's watching?").font(isTV ? .title2.bold() : .title3.bold())
        if loaded && profiles.isEmpty {
            Text("There are no other profiles to switch to.").foregroundStyle(.secondary)
        }
        LazyVGrid(columns: [GridItem(.adaptive(minimum: avatarSize + 40), spacing: isTV ? 40 : 20)], spacing: isTV ? 40 : 20) {
            ForEach(profiles, id: \.id) { p in
                let current = p.id == app.me?.id
                Button { choose(p) } label: {
                    VStack(spacing: 8) {
                        AvatarView(name: p.displayName, url: p.avatarUrl, size: avatarSize)
                            .overlay {
                                if current { Circle().strokeBorder(Color.marqueeGold, lineWidth: isTV ? 5 : 3) }
                            }
                            .overlay(alignment: .bottomTrailing) {
                                if !current && needsSecret(p) {
                                    Image(systemName: "lock.fill").font(.caption).padding(5).background(.thinMaterial, in: Circle())
                                }
                            }
                        Text(p.displayName).lineLimit(1)
                        Text(current ? "Current" : " ").font(.caption2).foregroundStyle(Color.marqueeGold)
                    }
                    .padding(isTV ? 12 : 0)
                }
                .buttonStyle(.plain)
                .disabled(busy || current)
                .accessibilityLabel(current ? "\(p.displayName), current profile" : p.displayName)
                .accessibilityIdentifier("profile.\(p.id)")
            }
        }
        .frame(maxWidth: isTV ? 1400 : 720)
    }

    private var accountButtons: some View {
        VStack(spacing: isTV ? 20 : 12) {
            if let onAccountSettings {
                Button { onAccountSettings() } label: {
                    Label("Account Settings", systemImage: "gearshape").frame(maxWidth: buttonWidth)
                }
                .buttonStyle(.bordered)
                .accessibilityIdentifier("profiles.accountSettings")
            }
            if showsSignOut {
                Button(role: .destructive) { Task { await app.signOut() } } label: {
                    Label("Sign Out", systemImage: "rectangle.portrait.and.arrow.right").frame(maxWidth: buttonWidth)
                }
                .buttonStyle(.bordered)
                .accessibilityIdentifier("profiles.signOut")
            }
        }
    }

    /// The PIN pad or password field for the profile picked.
    @ViewBuilder private func entry(_ p: Schemas.Profile) -> some View {
        AvatarView(name: p.displayName, url: p.avatarUrl, size: 80)
        if p.requires == .password {
            Text("Enter \(p.displayName)'s password").font(.headline)
            SecureField("Password", text: $password)
                .textContentType(.password)
                #if os(iOS)
                .textFieldStyle(.roundedBorder)
                #endif
                .frame(maxWidth: 420)
                .onSubmit { submitPassword(p) }
                .accessibilityIdentifier("profiles.password")
            Button("Switch") { submitPassword(p) }
                .buttonStyle(.borderedProminent)
                .disabled(busy || password.isEmpty)
        } else {
            Text("Enter \(p.displayName)'s PIN").font(.headline)
            PinPad(disabled: busy) { pin in switchTo(p, pin: pin) }
        }
        Button("Cancel") {
            target = nil
            password = ""
            error = nil
        }
    }

    /// Admins switch into managed profiles without the PIN (as the server allows).
    private func needsSecret(_ p: Schemas.Profile) -> Bool {
        p.requires != .none && !(app.me?.isAdmin == true && p.isManaged)
    }

    private func choose(_ p: Schemas.Profile) {
        error = nil
        if needsSecret(p) { target = p } else { switchTo(p, pin: nil) }
    }

    private func submitPassword(_ p: Schemas.Profile) {
        guard !password.isEmpty else { return }
        switchTo(p, password: password)
    }

    private func switchTo(_ p: Schemas.Profile, pin: String? = nil, password: String? = nil) {
        busy = true
        Task {
            defer { busy = false }
            do {
                try await app.switchProfile(to: p.id, pin: pin, password: password)
                target = nil
                self.password = ""
                onSwitched?()
            } catch {
                self.error = error.localizedDescription
            }
        }
    }

    #if os(tvOS)
    private let avatarSize: CGFloat = 160
    private let buttonWidth: CGFloat = 520
    #else
    private let avatarSize: CGFloat = 76
    private let buttonWidth: CGFloat = 280
    #endif
}

/// The signed-in profile's picture (or initial) as a round image for a sidebar entry: tab labels
/// take a plain image, not a view that loads one.
@MainActor @Observable
final class ProfileIcon {
    private(set) var image: UIImage?
    private var key: String?

    func load(app: AppSession, size: CGFloat) async {
        guard let me = app.me else {
            image = nil
            key = nil
            return
        }
        let k = "\(me.id)|\(me.displayName)|\(me.avatarUrl ?? "")|\(size)"
        guard k != key else { return }
        key = k
        image = Self.render(name: me.displayName, photo: nil, size: size)
        guard let url = app.absolute(me.avatarUrl) else { return }
        var photo = ImageMemory.image(url)
        if photo == nil, let (data, response) = try? await URLSession.shared.data(from: url),
           (response as? HTTPURLResponse)?.statusCode ?? 200 == 200 {
            photo = UIImage(data: data)
        }
        if let photo, key == k { image = Self.render(name: me.displayName, photo: photo, size: size) }
    }

    /// A circle with the photo filling it, or the name's first letter on grey (as AvatarView).
    static func render(name: String, photo: UIImage?, size: CGFloat) -> UIImage {
        let rect = CGRect(x: 0, y: 0, width: size, height: size)
        return UIGraphicsImageRenderer(size: rect.size).image { _ in
            UIBezierPath(ovalIn: rect).addClip()
            UIColor.gray.withAlphaComponent(0.45).setFill()
            UIRectFill(rect)
            if let photo, photo.size.width > 0, photo.size.height > 0 {
                let scale = max(size / photo.size.width, size / photo.size.height)
                let w = photo.size.width * scale, h = photo.size.height * scale
                photo.draw(in: CGRect(x: (size - w) / 2, y: (size - h) / 2, width: w, height: h))
            } else {
                let letter = String(name.prefix(1)).uppercased() as NSString
                let attrs: [NSAttributedString.Key: Any] = [
                    .font: UIFont.systemFont(ofSize: size * 0.46, weight: .bold),
                    .foregroundColor: UIColor.white,
                ]
                let s = letter.size(withAttributes: attrs)
                letter.draw(at: CGPoint(x: (size - s.width) / 2, y: (size - s.height) / 2), withAttributes: attrs)
            }
        }.withRenderingMode(.alwaysOriginal)
    }
}

#if os(iOS)
/// iPhone: the signed-in profile's avatar at the top of Home; it opens the profiles.
struct ProfileToolbarButton: View {
    @Environment(AppSession.self) private var app
    let open: () -> Void

    var body: some View {
        Button(action: open) {
            AvatarView(name: app.me?.displayName ?? "?", url: app.me?.avatarUrl, size: 30)
        }
        .accessibilityLabel("Profile: \(app.me?.displayName ?? "")")
        .accessibilityIdentifier("profileButton")
    }
}
#endif
