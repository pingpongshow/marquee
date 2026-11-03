import AppKit
import MarqueeKit
import SwiftUI
import UserNotifications

/// Tells the person when a newer Mac app is out: at launch, then once a day, it asks GitHub for
/// the latest release. A notification says so once per version; Settings and the sidebar keep
/// saying so until it's installed. It only ever opens the release page; nothing installs itself.
@MainActor @Observable
final class MacUpdates: NSObject, UNUserNotificationCenterDelegate {
    static let shared = MacUpdates()

    private static let enabledKey = "marquee.updates.check"
    private static let notifiedKey = "marquee.updates.notified"
    private static let interval: Duration = .seconds(24 * 60 * 60)

    private(set) var available: AppUpdate?
    private(set) var checking = false
    /// What the last check the person asked for found, for Settings.
    private(set) var status: String?
    var enabled: Bool = UserDefaults.standard.object(forKey: MacUpdates.enabledKey) as? Bool ?? true {
        didSet {
            UserDefaults.standard.set(enabled, forKey: Self.enabledKey)
            if enabled { start() } else { loop?.cancel(); loop = nil; available = nil }
        }
    }
    @ObservationIgnored private var loop: Task<Void, Never>?

    static var currentVersion: String { Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "" }

    /// At launch.
    func start() {
        UNUserNotificationCenter.current().delegate = self
        guard enabled, loop == nil else { return }
        loop = Task { [weak self] in
            // Not in the way of the app starting.
            try? await Task.sleep(for: .seconds(5))
            while !Task.isCancelled {
                _ = await self?.check(current: Self.currentVersion)
                try? await Task.sleep(for: Self.interval)
            }
        }
    }

    /// Asks GitHub. A failure (offline, GitHub's rate limit) says nothing and tries again next time,
    /// unless the person asked.
    @discardableResult
    func check(current: String, askedByPerson: Bool = false) async -> AppUpdate? {
        guard !checking else { return available }
        checking = true
        defer { checking = false }
        var req = URLRequest(url: AppUpdate.releasesURL, timeoutInterval: 20)
        req.setValue("application/vnd.github+json", forHTTPHeaderField: "Accept")
        req.setValue("Marquee-Mac/\(Self.currentVersion)", forHTTPHeaderField: "User-Agent")
        guard let (data, response) = try? await URLSession.shared.data(for: req),
              (response as? HTTPURLResponse)?.statusCode == 200 else {
            if askedByPerson { status = "Couldn't check for updates." }
            return available
        }
        let found = AppUpdate.from(releases: data, current: current)
        available = found
        if askedByPerson { status = found == nil ? "Marquee \(current) is the latest version." : nil }
        if let found { notifyOnce(found) }
        return found
    }

    /// Check for Updates… in the menu: says what it found wherever the app is, even signed out.
    func checkFromMenu() async {
        let found = await check(current: Self.currentVersion, askedByPerson: true)
        let alert = NSAlert()
        if let found {
            alert.messageText = "Marquee \(found.version) is available"
            alert.informativeText = "You have Marquee \(Self.currentVersion)."
            alert.addButton(withTitle: "Download…")
            alert.addButton(withTitle: "Later")
            if alert.runModal() == .alertFirstButtonReturn { openRelease() }
        } else {
            alert.messageText = status ?? "Couldn't check for updates."
            alert.runModal()
        }
    }

    func openRelease() {
        if let url = available?.url { NSWorkspace.shared.open(url) }
    }

    private func notifyOnce(_ update: AppUpdate) {
        let defaults = UserDefaults.standard
        guard defaults.string(forKey: Self.notifiedKey) != update.version else { return }
        Task {
            let center = UNUserNotificationCenter.current()
            guard (try? await center.requestAuthorization(options: [.alert, .sound])) == true else { return }
            let content = UNMutableNotificationContent()
            content.title = "Marquee \(update.version) is available"
            content.body = "Click to download it."
            content.userInfo = ["url": update.url.absoluteString]
            let request = UNNotificationRequest(identifier: "update-\(update.version)", content: content, trigger: nil)
            if (try? await center.add(request)) != nil { defaults.set(update.version, forKey: Self.notifiedKey) }
        }
    }

    // MARK: UNUserNotificationCenterDelegate

    /// Also while the app is in front.
    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        [.banner, .sound]
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        if response.actionIdentifier == UNNotificationDefaultActionIdentifier,
           let s = response.notification.request.content.userInfo["url"] as? String, let url = URL(string: s) {
            await MainActor.run { _ = NSWorkspace.shared.open(url) }
        }
    }
}

/// Settings' updates section.
struct MacUpdateSection: View {
    @State private var updates = MacUpdates.shared

    var body: some View {
        Section {
            Toggle("Tell me about new versions", isOn: $updates.enabled)
                .accessibilityIdentifier("settings.updateCheck")
            if let update = updates.available {
                Button("Download Marquee \(update.version)…") { updates.openRelease() }
                    .accessibilityIdentifier("settings.updateDownload")
            }
            HStack {
                Button("Check Now") {
                    Task { await updates.check(current: MacUpdates.currentVersion, askedByPerson: true) }
                }
                .disabled(updates.checking)
                .accessibilityIdentifier("settings.updateNow")
                if updates.checking { ProgressView().controlSize(.small) }
                if let status = updates.status { Text(status).font(.caption).foregroundStyle(.secondary) }
            }
        } header: {
            Text("Updates")
        } footer: {
            Text("You have Marquee \(MacUpdates.currentVersion). It asks GitHub once a day whether there's a newer one, and only ever opens its page; nothing installs by itself.")
        }
    }
}
