import Foundation

/// An artist's releases in sections by release type (MusicBrainz, META-3):
/// Albums (and releases of unknown type), Singles & EPs, Live Albums, Compilations… Empty
/// sections are left out.
public struct ReleaseSection: Sendable {
    public let title: String
    public let items: [Item]
}

public func releaseSections(_ items: [Item]) -> [ReleaseSection] {
    let order: [(String, Set<String>)] = [
        ("Albums", ["album", ""]), ("Singles & EPs", ["ep", "single"]), ("Live Albums", ["live"]),
        ("Compilations", ["compilation"]), ("Soundtracks", ["soundtrack"]), ("Remixes", ["remix"]),
        ("Demos", ["demo"]), ("Other", ["other"]),
    ]
    return order.compactMap { title, types in
        let list = items.filter { types.contains($0.releaseType?.rawValue ?? "") }
        return list.isEmpty ? nil : ReleaseSection(title: title, items: list)
    }
}
