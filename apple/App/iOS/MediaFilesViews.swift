import MarqueeKit
import SwiftUI
import UIKit

// Media files for admins (ADM-11): file names in Fix Match, comparing duplicate files and
// deleting one to the trash, and the setting that allows it.

/// A path's file name with its folder in small text below. Long-press shows and copies the
/// full path.
struct FilePathLabel: View {
    let path: String
    var caption: String? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(path.fileBaseName).font(.subheadline.weight(.semibold)).lineLimit(2).truncationMode(.middle)
            Text(path.fileFolder).font(.caption2).foregroundStyle(.secondary).lineLimit(1).truncationMode(.head)
            if let caption { Text(caption).font(.caption2.weight(.semibold)).foregroundStyle(Color.marqueeGold) }
        }
        .help(path)
        .contextMenu {
            Text(path)
            Button("Copy Path", systemImage: "doc.on.doc") { UIPasteboard.general.string = path }
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("file-\(path)")
    }
}

extension String {
    /// The last path component ("Movie (2020).mkv").
    var fileBaseName: String { (self as NSString).lastPathComponent }
    /// The folder the file is in.
    var fileFolder: String { (self as NSString).deletingLastPathComponent }
}

// MARK: - Comparing duplicate files

/// Describes a media file for the duplicate comparison.
enum FileFacts {
    static func gigabytes(_ bytes: Int64) -> String {
        String(format: "%.2f GB", Double(bytes) / 1_000_000_000)
    }

    static func resolution(_ f: MediaFile) -> String {
        guard let w = f.width, let h = f.height, w > 0, h > 0 else { return "—" }
        let name = if w >= 3200 || h >= 2000 { "4K" } else if w >= 1800 || h >= 1000 { "1080p" } else if w >= 1200 || h >= 700 { "720p" } else { "SD" }
        return "\(name) (\(w)×\(h))"
    }

    static func hdr(_ f: MediaFile) -> String {
        switch f.hdrFormat {
        case .dolbyVision: f.dvProfile.map { "Dolby Vision (profile \($0))" } ?? "Dolby Vision"
        case .hdr10: "HDR10"
        case .hdr10plus: "HDR10+"
        case .hlg: "HLG"
        case nil: "SDR"
        }
    }

    static func bitrate(_ f: MediaFile) -> String {
        guard let k = f.bitrateKbps, k > 0 else { return "—" }
        return String(format: "%.1f Mbps", Double(k) / 1000)
    }

    static func firstAudio(_ f: MediaFile) -> MediaStream? { f.streams.first { $0.kind == .audio } }

    static func channels(_ n: Int) -> String {
        switch n {
        case 1: "1.0"
        case 2: "2.0"
        case 3: "2.1"
        case 6: "5.1"
        case 7: "6.1"
        case 8: "7.1"
        default: "\(n) ch"
        }
    }

    static func audio(_ f: MediaFile) -> String {
        let a = firstAudio(f)
        let codec = (a?.codec ?? f.audioCodec)?.uppercased()
        let ch = a?.channels.map(channels)
        let s = [codec, ch].compactMap { $0 }.joined(separator: " ")
        return s.isEmpty ? "—" : s
    }

    static func subtitleCount(_ f: MediaFile) -> Int { f.streams.filter { $0.kind == .subtitle }.count }

    static func duration(_ ms: Int64?) -> String {
        guard let ms, ms > 0 else { return "—" }
        let m = Int(ms / 60000)
        return m >= 60 ? "\(m / 60)h \(m % 60)m" : "\(m)m"
    }
}

/// One compared row: a value per file, and which files have the better value (none when all
/// are equal or the row isn't comparable).
private struct FactRow: Identifiable {
    let id: String
    let values: [String]
    var better: Set<Int> = []

    init(_ label: String, _ values: [String], scores: [Double]? = nil) {
        id = label
        self.values = values
        if let scores, let best = scores.max(), Set(scores).count > 1 {
            better = Set(scores.indices.filter { scores[$0] == best })
        }
    }
}

/// Every file of a duplicated title side by side, with the better value in each row
/// highlighted, and (when Settings → Library Settings allows it) Delete… on each.
struct DuplicateCompareView: View {
    @Environment(AppSession.self) private var app
    let issue: HealthIssue
    /// Called after a file is deleted, to refresh the issue list.
    var onDeleted: () async -> Void = {}
    @State private var files: [IssueFile] = []
    @State private var allowDeletion: Bool?
    @State private var confirming: IssueFile?
    @State private var deleting = false
    @State private var error: String?
    @State private var notice: String?

    private let column: CGFloat = 210

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                Text(issue.detail).font(.callout).foregroundStyle(.secondary)
                if let error { ErrorBanner(message: error) }
                if let notice { Label(notice, systemImage: "trash").font(.callout).foregroundStyle(.secondary) }
                if allowDeletion == false {
                    Text("Turn on 'Allow deleting media files' in Settings → Library Settings to delete from here.")
                        .font(.footnote).foregroundStyle(.secondary)
                        .padding(10).frame(maxWidth: .infinity, alignment: .leading)
                        .background(Color.secondary.opacity(0.12), in: RoundedRectangle(cornerRadius: 10))
                        .accessibilityIdentifier("deletionOffNote")
                }
                ScrollView(.horizontal) { grid }
            }
            .padding()
        }
        .navigationTitle("Compare Files")
        .navigationBarTitleDisplayMode(.inline)
        .task {
            if files.isEmpty { files = issue.files ?? [] }
            allowDeletion = (try? await app.mediaDeletionAllowed()) ?? false
        }
        .refreshable { allowDeletion = (try? await app.mediaDeletionAllowed()) ?? false }
        .alert(confirming.map { "Delete “\($0.file.path?.fileBaseName ?? $0.itemTitle)”?" } ?? "",
               isPresented: Binding(get: { confirming != nil }, set: { if !$0 { confirming = nil } }),
               presenting: confirming) { f in
            Button("Delete", role: .destructive) { delete(f) }
            Button("Cancel", role: .cancel) {}
        } message: { f in
            Text("“\(f.file.path ?? f.itemTitle)” moves to the library's .marquee-trash folder, where it's kept for 30 days before it's removed for good.")
        }
    }

    private var rows: [FactRow] {
        let fs = files.map(\.file)
        var out: [FactRow] = []
        if Set(files.map(\.itemTitle)).count > 1 { out.append(FactRow("Title", files.map(\.itemTitle))) }
        if files.contains(where: { $0.versionLabel?.isEmpty == false }) {
            out.append(FactRow("Edition", files.map { $0.versionLabel ?? "—" }))
        }
        out += [
            FactRow("Size", fs.map { FileFacts.gigabytes($0.size) }),
            FactRow("Resolution", fs.map(FileFacts.resolution), scores: fs.map { Double(($0.width ?? 0) * ($0.height ?? 0)) }),
            FactRow("Video", fs.map { $0.videoCodec?.uppercased() ?? "—" }),
            FactRow("HDR", fs.map(FileFacts.hdr), scores: fs.map { $0.hdrFormat == nil ? 0 : 1 }),
            FactRow("Bitrate", fs.map(FileFacts.bitrate), scores: fs.map { Double($0.bitrateKbps ?? 0) }),
            FactRow("Audio", fs.map(FileFacts.audio), scores: fs.map { Double(FileFacts.firstAudio($0)?.channels ?? 0) }),
            FactRow("Subtitles", fs.map { "\(FileFacts.subtitleCount($0))" }, scores: fs.map { Double(FileFacts.subtitleCount($0)) }),
            FactRow("Duration", fs.map { FileFacts.duration($0.durationMs) }),
            FactRow("Container", fs.map { $0.container?.uppercased() ?? "—" }),
            FactRow("Added", files.map { $0.addedAt.formatted(date: .abbreviated, time: .omitted) }),
        ]
        return out
    }

    private var grid: some View {
        Grid(alignment: .topLeading, horizontalSpacing: 16, verticalSpacing: 10) {
            GridRow {
                Color.clear.frame(width: 1, height: 1)
                ForEach(files, id: \.file.id) { f in
                    FilePathLabel(path: f.file.path ?? f.itemTitle).frame(width: column, alignment: .leading)
                }
            }
            Divider()
            ForEach(rows) { row in
                GridRow {
                    Text(row.id).font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                    ForEach(Array(row.values.enumerated()), id: \.offset) { i, v in
                        let better = row.better.contains(i)
                        Text(v)
                            .font(.callout.weight(better ? .semibold : .regular))
                            .foregroundStyle(better ? Color.green : .primary)
                            .lineLimit(2)
                            .frame(width: column, alignment: .leading)
                            .accessibilityLabel("\(row.id): \(v)\(better ? ", better" : "")")
                    }
                }
            }
            if allowDeletion == true {
                Divider()
                GridRow {
                    Color.clear.frame(width: 1, height: 1)
                    ForEach(files, id: \.file.id) { f in
                        Button("Delete…", systemImage: "trash", role: .destructive) { confirming = f }
                            .buttonStyle(.bordered)
                            .disabled(deleting || files.count <= 1)
                            .accessibilityIdentifier("delete-\(f.file.path ?? "\(f.file.id)")")
                            .accessibilityHint(files.count <= 1 ? "The last copy can't be deleted from here." : "")
                    }
                }
            }
        }
    }

    private func delete(_ f: IssueFile) {
        guard files.count > 1 else { return }
        deleting = true
        Task {
            defer { deleting = false }
            do {
                try await app.deleteMediaFile(f.file.id)
                files.removeAll { $0.file.id == f.file.id }
                notice = "Moved “\(f.file.path?.fileBaseName ?? f.itemTitle)” to the trash."
                error = nil
                await onDeleted()
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}

// MARK: - Library settings

/// Server library settings (admin) that the apps manage: deleting media files.
struct LibrarySettingsView: View {
    @Environment(AppSession.self) private var app
    @State private var allow = false
    @State private var loaded = false
    @State private var error: String?

    var body: some View {
        Form {
            if let error { ErrorBanner(message: error) }
            Section {
                Toggle("Allow deleting media files", isOn: Binding(get: { allow }, set: { save($0) }))
                    .disabled(!loaded)
                    .accessibilityIdentifier("allowMediaDeletion")
            } footer: {
                Text("Lets admins delete media files from Library Health. Deleted files are moved to a .marquee-trash folder in their library folder and removed for good after 30 days.")
            }
        }
        .navigationTitle("Library Settings")
        .task {
            do { allow = try await app.mediaDeletionAllowed(); loaded = true } catch { self.error = error.localizedDescription }
        }
    }

    private func save(_ on: Bool) {
        let before = allow
        allow = on
        Task {
            do { try await app.setMediaDeletionAllowed(on); error = nil } catch {
                allow = before
                self.error = error.localizedDescription
            }
        }
    }
}
