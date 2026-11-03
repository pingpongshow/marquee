import AppKit
import SwiftUI

// iOS names that shared views use, on the Mac: images are NSImage, and what only makes sense on a
// touch screen does nothing. They keep App/Shared free of #if around every modifier.

typealias UIImage = NSImage

extension NSImage {
    /// NSImage decodes as it draws.
    func preparingForDisplay() -> NSImage? { self }
    var scale: CGFloat { 1 }
}

extension Image {
    init(uiImage: NSImage) { self.init(nsImage: uiImage) }
}

enum MacTitleDisplayMode { case automatic, inline, large }
enum MacTextInputAutocapitalization { case never, words, sentences, characters }
enum MacKeyboardType { case `default`, URL, numberPad, decimalPad, emailAddress, asciiCapable, phonePad, numbersAndPunctuation }

extension View {
    func navigationBarTitleDisplayMode(_ mode: MacTitleDisplayMode) -> some View { self }
    func textInputAutocapitalization(_ mode: MacTextInputAutocapitalization?) -> some View { self }
    func keyboardType(_ type: MacKeyboardType) -> some View { self }
}

extension ToolbarItemPlacement {
    static var topBarLeading: ToolbarItemPlacement { .navigation }
    static var topBarTrailing: ToolbarItemPlacement { .automatic }
}

/// Covers on the Mac are sheets nearly the size of the window: the window itself is still what a
/// video belongs to (hiding it for the mini player takes the sheet along), and `dismiss` works.
private struct MacCover<Cover: View>: View {
    @ViewBuilder let cover: Cover
    let close: () -> Void

    var body: some View {
        let size = MacWindows.shared.coverSize
        cover
            .frame(width: size.width, height: size.height)
            .macEnvironment()
            .onAppear { MacVideo.shared.coverClose = close }
            .onDisappear { MacVideo.shared.coverClose = nil }
    }
}

extension View {
    func fullScreenCover<Item: Identifiable, Content: View>(item: Binding<Item?>, onDismiss: (() -> Void)? = nil,
                                                            @ViewBuilder content: @escaping (Item) -> Content) -> some View {
        sheet(item: item, onDismiss: onDismiss) { value in
            MacCover(cover: { content(value) }, close: { item.wrappedValue = nil })
        }
    }

    func fullScreenCover<Content: View>(isPresented: Binding<Bool>, onDismiss: (() -> Void)? = nil,
                                        @ViewBuilder content: @escaping () -> Content) -> some View {
        sheet(isPresented: isPresented, onDismiss: onDismiss) {
            MacCover(cover: { content() }, close: { isPresented.wrappedValue = false })
        }
    }
}

/// Enough of UIDevice for shared layout checks: a Mac is no phone.
enum UIUserInterfaceIdiom { case phone, pad, mac }
struct UIDevice {
    static let current = UIDevice()
    let userInterfaceIdiom = UIUserInterfaceIdiom.mac
}
