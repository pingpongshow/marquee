import XCTest

/// The Apple TV sidebar (sidebarAdaptable): Back from the top of a page (or Left from its
/// leftmost item) opens it; choosing an entry opens that section.
extension XCUIApplication {
    private var focusedElement: XCUIElement { descendants(matching: .any).element(matching: NSPredicate(format: "hasFocus == true")) }

    /// The sidebar entry with this title while the sidebar is open (collapsed, its entries
    /// are still in the tree, disabled and zero-sized).
    func sidebarItem(_ name: String) -> XCUIElement? {
        buttons.matching(NSPredicate(format: "label == %@", name)).allElementsBoundByIndex
            .first { $0.isEnabled && $0.frame.width > 0 && $0.frame.minX < 100 }
    }

    var sidebarIsOpen: Bool { sidebarItem("Home") != nil }

    /// Presses Back until the sidebar shows (popping pushed pages on the way). Checked before
    /// each press: Back with the sidebar open leaves the app.
    func openSidebar(file: StaticString = #filePath, line: UInt = #line) {
        // Right after signing in the sidebar can already be open (Home has nothing to focus
        // yet): let it settle first.
        for _ in 0..<6 where !sidebarIsOpen { Thread.sleep(forTimeInterval: 0.5) }
        for _ in 0..<5 where !sidebarIsOpen {
            XCUIRemote.shared.press(.menu)
            for _ in 0..<4 where !sidebarIsOpen { Thread.sleep(forTimeInterval: 0.5) }
        }
        XCTAssertTrue(sidebarIsOpen, "the sidebar opened", file: file, line: line)
    }

    /// Opens the sidebar, moves focus to the entry and selects it.
    func openSection(_ name: String, file: StaticString = #filePath, line: UInt = #line) {
        openSidebar(file: file, line: line)
        var item = sidebarItem(name)
        for _ in 0..<10 where item == nil {
            Thread.sleep(forTimeInterval: 0.5)
            item = sidebarItem(name)
        }
        guard let item else { return XCTFail("no \(name) in the sidebar", file: file, line: line) }
        let remote = XCUIRemote.shared
        for _ in 0..<20 {
            let dy = item.frame.midY - focusedElement.frame.midY
            if abs(dy) < 10 { break }
            remote.press(dy > 0 ? .down : .up)
        }
        XCTAssertLessThan(abs(item.frame.midY - focusedElement.frame.midY), 10, "couldn't focus \(name); focus is on \(focusedElement.label)", file: file, line: line)
        remote.press(.select)
    }

    /// A page title on the page itself (not the collapsed sidebar's button at the top left,
    /// which names the page).
    func pageTitleShows(_ name: String) -> Bool {
        staticTexts.matching(NSPredicate(format: "label == %@", name)).allElementsBoundByIndex
            .contains { $0.frame.width > 0 && $0.frame.minX > 400 }
    }
}
