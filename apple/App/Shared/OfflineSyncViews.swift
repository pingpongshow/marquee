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
