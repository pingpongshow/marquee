import ImageIO
import MarqueeKit
import SwiftUI
import UniformTypeIdentifiers
#if os(iOS)
import PhotosUI
#endif

extension View {
    /// Change Picture for a playlist or collection of the person's own (MUSIC-30): an image
    /// uploaded from the device, or one at a web address; Remove goes back to the mosaic. The
    /// TV has no files to choose from: a link only.
    func playlistPicture(isPresented: Binding<Bool>, playlist: Playlist?, changed: @escaping (Playlist) -> Void) -> some View {
        modifier(PlaylistPictureFlow(isPresented: isPresented, playlist: playlist, changed: changed))
    }
}

private struct PlaylistPictureFlow: ViewModifier {
    @Environment(AppSession.self) private var app
    @Binding var isPresented: Bool
    let playlist: Playlist?
    let changed: (Playlist) -> Void
    #if os(tvOS)
    @State private var link = ""
    #endif

    func body(content: Content) -> some View {
        #if os(tvOS)
        content
            .alert("Change Picture", isPresented: $isPresented) {
                TextField("Image link", text: $link)
                    .accessibilityIdentifier("picture.link")
                Button("Use Link") {
                    let url = link.trimmingCharacters(in: .whitespacesAndNewlines)
                    guard let playlist, !url.isEmpty else { return }
                    ActionError.run { done(try await app.setPlaylistImageLink(playlist.id, url: url)) }
                }
                if let playlist, playlist.imageVersion != nil {
                    Button("Remove Picture", role: .destructive) {
                        ActionError.run { done(try await app.removePlaylistImage(playlist.id)) }
                    }
                }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("The address of an image on the web. The server keeps its own copy.")
            }
            .onChange(of: isPresented) { if isPresented { link = "" } }
        #else
        content
            .sheet(isPresented: $isPresented) {
                if let playlist { PlaylistPictureSheet(playlist: playlist, changed: done).macSheetFrame(460, 480) }
            }
        #endif
    }

    /// The pages and lists showing it load again.
    private func done(_ p: Playlist) {
        changed(p)
        PlaylistChanges.shared.changed()
    }
}

#if !os(tvOS)
private struct PlaylistPictureSheet: View {
    @Environment(AppSession.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State var playlist: Playlist
    let changed: (Playlist) -> Void
    @State private var link = ""
    @State private var working = false
    @State private var error: String?
    @State private var importing = false
    #if os(iOS)
    @State private var photo: PhotosPickerItem?
    #endif

    private var trimmedLink: String { link.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        NavigationStack {
            Form {
                if let error { ErrorBanner(message: error) }
                Section {
                    HStack {
                        Spacer()
                        PlaylistMosaic(playlist: playlist, size: 140)
                            .overlay { if working { ProgressView().controlSize(.large) } }
                        Spacer()
                    }
                    .listRowBackground(Color.clear)
                }
                Section {
                    #if os(iOS)
                    PhotosPicker(selection: $photo, matching: .images) { Label("Choose Photo…", systemImage: "photo.on.rectangle") }
                        .accessibilityIdentifier("picture.photo")
                    #endif
                    Button { importing = true } label: { Label("Choose File…", systemImage: "folder") }
                        .accessibilityIdentifier("picture.file")
                } header: {
                    Text("Upload image")
                }
                Section {
                    TextField("https://…", text: $link)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(.URL)
                        .accessibilityIdentifier("picture.link")
                    Button("Use Link") {
                        let url = trimmedLink
                        run { try await app.setPlaylistImageLink(playlist.id, url: url) }
                    }
                    .disabled(trimmedLink.isEmpty)
                    .accessibilityIdentifier("picture.useLink")
                } header: {
                    Text("Image link")
                } footer: {
                    Text("The server downloads the image and keeps its own copy.")
                }
                if playlist.imageVersion != nil {
                    Section {
                        Button("Remove Picture", role: .destructive) { run { try await app.removePlaylistImage(playlist.id) } }
                            .accessibilityIdentifier("picture.remove")
                    }
                }
            }
            .disabled(working)
            .navigationTitle("Change Picture")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
            .fileImporter(isPresented: $importing, allowedContentTypes: [.image]) { result in
                switch result {
                case let .success(url): run { try await app.uploadPlaylistImage(playlist.id, data: try Self.read(url)) }
                case let .failure(e): error = e.localizedDescription
                }
            }
            #if os(iOS)
            .onChange(of: photo) {
                guard let picked = photo else { return }
                photo = nil
                run {
                    guard let data = try await picked.loadTransferable(type: Data.self) else { throw MarqueeError("Couldn't read that photo.") }
                    return try await app.uploadPlaylistImage(playlist.id, data: try Self.uploadable(data))
                }
            }
            #endif
        }
    }

    private func run(_ work: @escaping @MainActor () async throws -> Playlist) {
        working = true
        error = nil
        Task {
            defer { working = false }
            do {
                let p = try await work()
                playlist = p
                link = ""
                changed(p)
            } catch is CancellationError {
            } catch {
                self.error = error.localizedDescription
            }
        }
    }

    /// A chosen file's bytes (the file may be outside the app's own folders).
    private static func read(_ url: URL) throws -> Data {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        return try uploadable(Data(contentsOf: url))
    }

    /// The server takes JPEG, PNG and WebP up to 20 MB: those go as they are, anything else (a
    /// photo in HEIC, or a larger file) as a JPEG no more than 3000 pixels across.
    private static func uploadable(_ data: Data) throws -> Data {
        guard let source = CGImageSourceCreateWithData(data as CFData, nil) else { throw MarqueeError("That file isn't an image.") }
        let type = (CGImageSourceGetType(source) as String?).flatMap { UTType($0) }
        if let type, [.jpeg, .png, .webP].contains(type), data.count <= 20 << 20 { return data }
        let options: [CFString: Any] = [kCGImageSourceCreateThumbnailFromImageAlways: true, kCGImageSourceCreateThumbnailWithTransform: true,
                                        kCGImageSourceThumbnailMaxPixelSize: 3000]
        let out = NSMutableData()
        guard let image = CGImageSourceCreateThumbnailAtIndex(source, 0, options as CFDictionary),
              let dest = CGImageDestinationCreateWithData(out, UTType.jpeg.identifier as CFString, 1, nil) else {
            throw MarqueeError("That image can't be read.")
        }
        CGImageDestinationAddImage(dest, image, [kCGImageDestinationLossyCompressionQuality: 0.9] as CFDictionary)
        guard CGImageDestinationFinalize(dest) else { throw MarqueeError("That image can't be read.") }
        return out as Data
    }
}
#endif
