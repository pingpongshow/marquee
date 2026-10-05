import MarqueeKit
import SwiftUI

/// Settings: changes made offline that haven't reached the server yet (USER-18). Only shown
/// while some wait.
struct PendingSyncSection: View {
    private var sync: OfflineSync { OfflineSync.shared }

    var body: some View {
        let n = sync.pendingCount
        if n > 0 {
            Section {
                LabeledContent("Changes waiting to sync", value: "\(n)")
                    .accessibilityIdentifier("pendingChanges")
                if let last = sync.lastSync {
                    LabeledContent("Last synced") { Text(last, format: .relative(presentation: .named)) }
                }
                Button(sync.isSyncing ? "Syncing…" : "Sync Now") { Task { await sync.flush() } }
                    .disabled(sync.isSyncing)
                    .accessibilityIdentifier("syncNow")
            } header: {
                Text("Offline changes")
            } footer: {
                Text("Ratings, watched, watchlist, comments and offline plays made while the server couldn't be reached. They're sent when it can be.")
            }
        }
    }
}

/// A brief "Saved offline, will sync" note when a change is kept on the device.
private struct OfflineSavedToast: ViewModifier {
    @State private var shown = false
    @State private var hide: Task<Void, Never>?

    func body(content: Content) -> some View {
        content
            .overlay(alignment: .top) {
                if shown {
                    Label("Saved offline, will sync", systemImage: "icloud.and.arrow.up")
                        .font(isTV ? .callout.weight(.semibold) : .footnote.weight(.semibold))
                        .padding(.horizontal, 14).padding(.vertical, 8)
                        .background(.regularMaterial, in: Capsule())
                        .padding(.top, 8)
                        .transition(.move(edge: .top).combined(with: .opacity))
                        .accessibilityIdentifier("savedOfflineToast")
                        .allowsHitTesting(false)
                }
            }
            .onChange(of: OfflineSync.shared.lastQueued) {
                withAnimation { shown = true }
                hide?.cancel()
                hide = Task {
                    try? await Task.sleep(for: .seconds(2.5))
                    guard !Task.isCancelled else { return }
                    withAnimation { shown = false }
                }
            }
    }
}

extension View {
    func offlineSavedToast() -> some View { modifier(OfflineSavedToast()) }
}
