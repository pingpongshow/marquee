import MarqueeKit
import SwiftUI

/// Edit Home (USER-12): reorder the rows (drag on iPhone/iPad, buttons on the TV), hide or
/// show them, unpin pinned collections and playlists, or go back to the default. Each change
/// is saved straight away.
struct HomeEditView: View {
    @Environment(AppSession.self) private var app
    @State private var rows: [HomeLayoutRow] = []
    @State private var loaded = false
    @State private var error: String?

    var body: some View {
        content
            .navigationTitle("Edit Home")
            .task { await load() }
    }

    #if os(iOS)
    private var content: some View {
        List {
            if let error { ErrorBanner(message: error) }
            Section {
                ForEach(rows, id: \.id) { row in rowView(row) }
                    .onMove { from, to in
                        rows.move(fromOffsets: from, toOffset: to)
                        save()
                    }
            } footer: {
                Text("Drag rows to change their order. Pinned collections and playlists appear on Home like any other row.")
            }
            Section {
                Button("Reset to Default", role: .destructive) { reset() }.disabled(!loaded)
            }
        }
        .environment(\.editMode, .constant(.active))
    }
    #else
    private var content: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                if let error { ErrorBanner(message: error) }
                ForEach(Array(rows.enumerated()), id: \.element.id) { i, row in
                    HStack(spacing: 24) {
                        rowView(row)
                        Button { move(i, by: -1) } label: { Image(systemName: "chevron.up") }
                            .disabled(i == 0)
                            .accessibilityLabel("Move \(row.title ?? row.id) up")
                        Button { move(i, by: 1) } label: { Image(systemName: "chevron.down") }
                            .disabled(i == rows.count - 1)
                            .accessibilityLabel("Move \(row.title ?? row.id) down")
                    }
                    .focusSection()
                }
                Button("Reset to Default") { reset() }
                    .disabled(!loaded)
                    .padding(.top, 20)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .focusSection()
            }
            .padding(.horizontal, sidePadding)
            .padding(.vertical, 40)
        }
    }

    private func move(_ i: Int, by d: Int) {
        guard rows.indices.contains(i + d) else { return }
        rows.swapAt(i, i + d)
        save()
    }
    #endif

    private func rowView(_ row: HomeLayoutRow) -> some View {
        let title = row.title ?? row.id
        let hidden = row.hidden ?? false
        return HStack(spacing: 16) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title).foregroundStyle(hidden ? .secondary : .primary)
                if row.pinned == true { Label("Pinned", systemImage: "pin.fill").font(.caption).foregroundStyle(Color.marqueeGold) }
            }
            Spacer()
            Button { toggle(row.id) } label: {
                Label(hidden ? "Show" : "Hide", systemImage: hidden ? "eye.slash" : "eye")
                    .labelStyle(.iconOnly)
                    .foregroundStyle(hidden ? Color.secondary : Color.marqueeGold)
            }
            .accessibilityLabel(hidden ? "Show \(title)" : "Hide \(title)")
            if row.pinned == true {
                Button(role: .destructive) { unpin(row.id) } label: { Label("Unpin", systemImage: "pin.slash").labelStyle(.iconOnly) }
                    .accessibilityLabel("Unpin \(title)")
            }
        }
        #if os(iOS)
        .buttonStyle(.borderless)
        #endif
    }

    private func load() async {
        do {
            rows = try await app.homeLayout()
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
        loaded = true
    }

    private func toggle(_ id: String) {
        guard let i = rows.firstIndex(where: { $0.id == id }) else { return }
        rows[i].hidden = !(rows[i].hidden ?? false)
        save()
    }

    private func unpin(_ id: String) {
        rows.removeAll { $0.id == id }
        save()
    }

    private func reset() {
        rows = []
        save()
    }

    private func save() {
        let wanted = rows
        Task {
            do {
                let saved = try await app.saveHomeLayout(wanted)
                if rows.map(\.id) == wanted.map(\.id) { rows = saved }
                error = nil
            } catch {
                self.error = error.localizedDescription
                await load()
            }
        }
    }
}

/// "Pin to Home" / "Unpin from Home" for a collection or playlist (USER-12).
/// Bumped when Home's rows change elsewhere (a pin), so Home loads again on its next appearance.
@MainActor @Observable
final class HomeChanges {
    static let shared = HomeChanges()
    var version = 0
}

struct PinToHomeButton: View {
    @Environment(AppSession.self) private var app
    let rowID: String
    @State private var pinned: Bool?

    var body: some View {
        Button {
            guard let on = pinned else { return }
            pinned = !on
            ActionError.run {
                do {
                    try await app.setPinned(rowID, !on)
                    HomeChanges.shared.version += 1
                } catch { pinned = on; throw error }
            }
        } label: {
            Label(pinned == true ? "Unpin from Home" : "Pin to Home", systemImage: pinned == true ? "pin.slash" : "pin")
        }
        .buttonStyle(.bordered)
        .disabled(pinned == nil)
        .task(id: rowID) { pinned = await app.isPinned(rowID) }
    }
}
