import TVServices

/// The Top Shelf (shown when Marquee is in the top row of the Apple TV home screen):
/// Continue Watching and what's new, from the snapshot the app saves.
final class ContentProvider: TVTopShelfContentProvider {
    override func loadTopShelfContent() async -> (any TVTopShelfContent)? {
        guard let snapshot = TopShelfSnapshot.load(), !snapshot.sections.isEmpty else { return nil }
        let sections = snapshot.sections.map { s in
            let collection = TVTopShelfItemCollection(items: s.items.map(item))
            collection.title = s.title
            return collection
        }
        return TVTopShelfSectionedContent(sections: sections)
    }

    private func item(_ i: TopShelfSnapshot.Item) -> TVTopShelfSectionedItem {
        let t = TVTopShelfSectionedItem(identifier: String(i.id))
        t.title = i.title
        t.imageShape = i.wide ? .hdtv : .poster
        if let url = i.imageURL { t.setImageURL(url, for: [.screenScale1x, .screenScale2x]) }
        if let open = URL(string: "marquee://item/\(i.id)") { t.displayAction = TVTopShelfAction(url: open) }
        if i.playable, let play = URL(string: "marquee://play/\(i.id)") { t.playAction = TVTopShelfAction(url: play) }
        return t
    }
}
