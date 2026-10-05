import MarqueeKit
import SwiftUI

/// "Your rating" and the community's: everyone's average on an item page, which opens the
/// ratings and comments.
struct ItemRatingsHeader: View {
    let item: Item
    /// Asks the page to fetch the item again (a new average).
    let refresh: () async -> Void
    @State private var showing = false

    var body: some View {
        // TV: side by side, so down from the stars reaches the actions row directly.
        let layout = isTV ? AnyLayout(HStackLayout(alignment: .bottom, spacing: 40)) : AnyLayout(VStackLayout(alignment: .leading, spacing: 10))
        layout {
            VStack(alignment: .leading, spacing: 4) {
                Text("Your rating").font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                RatingStars(itemID: item.id, rating: item.userRating, labelPrefix: "Rate ")
                    .id(item.id)
                    .font(isTV ? .title3 : .body)
                    .accessibilityIdentifier("itemRating")
            }
            Button { showing = true } label: {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Community").font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                    HStack(spacing: 8) {
                        CommunitySummary(rating: item.communityRating)
                        Image(systemName: "chevron.right").font(.caption.bold()).foregroundStyle(.tertiary)
                    }
                }
                .contentShape(Rectangle())
            }
            #if os(tvOS)
            .buttonStyle(.bordered)
            #else
            .buttonStyle(.plain)
            #endif
            .accessibilityIdentifier("communityRow")
        }
        .sheet(isPresented: $showing, onDismiss: { Task { await refresh() } }) {
            ReviewsSheet(itemID: item.id, title: item.title, yourRating: item.userRating)
        }
    }
}

/// Read-only average stars and "4.2 · 7 ratings", or "Not rated yet".
struct CommunitySummary: View {
    let rating: CommunityRating?

    var body: some View {
        Group {
            if let r = rating, r.count > 0 {
                HStack(spacing: 8) {
                    RatingGlyphs(rating: r.average).font(.system(size: isTV ? 22 : 13, weight: .semibold))
                    Text("\((r.average / 2).formatted(.number.precision(.fractionLength(1)))) · \(r.count) \(r.count == 1 ? "rating" : "ratings")")
                        .font(.subheadline.monospacedDigit()).foregroundStyle(.secondary)
                }
            } else {
                Text("Not rated yet").font(.subheadline).foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(rating.map { $0.count > 0 ? "Community rating \(($0.average / 2).formatted(.number.precision(.fractionLength(1)))) stars, \($0.count) \($0.count == 1 ? "rating" : "ratings")" : "Not rated yet" } ?? "Not rated yet")
        .accessibilityIdentifier("communitySummary")
    }
}

/// Everyone's ratings and comments on an item; yours can be written, changed or removed here
/// (on the Apple TV only the rating), and an administrator can remove anyone's.
struct ReviewsSheet: View {
    @Environment(AppSession.self) private var app
    @Environment(\.dismiss) private var dismiss
    let itemID: Int64
    let title: String
    let yourRating: Double?
    @State private var reviews: ItemReviews?
    @State private var draft = ""
    @State private var error: String?
    @State private var busy = false
    @State private var confirmDelete: Review?

    private var mine: Review? { reviews?.reviews.first { $0.mine } }
    /// Your comment: one waiting to sync (USER-18), or the server's.
    private var myComment: String { OfflineSync.shared.pendingComment(itemID) ?? mine?.comment ?? "" }
    private var others: [Review] { reviews?.reviews.filter { !$0.mine } ?? [] }
    private var isAdmin: Bool { app.me?.isAdmin == true }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    CommunitySummary(rating: reviews.flatMap { r in r.average.map { CommunityRating(average: $0, count: r.count) } })
                }
                if let error {
                    Section { Label(error, systemImage: "exclamationmark.triangle.fill").foregroundStyle(.red) }
                }
                Section("You") {
                    RatingStars(itemID: itemID, rating: mine?.rating ?? yourRating, labelPrefix: "Rate ")
                        .id("\(itemID)-\(mine?.rating ?? -1)")
                        .accessibilityIdentifier("reviewRating")
                    #if os(iOS)
                    TextField("Add a comment", text: $draft, axis: .vertical)
                        .lineLimit(2...8)
                        .accessibilityIdentifier("commentField")
                    HStack {
                        Button(!myComment.isEmpty ? "Save" : "Post") { save(draft) }
                            .buttonStyle(.borderedProminent)
                            .disabled(busy || draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || draft == myComment || draft.count > 2000)
                            .accessibilityIdentifier("postComment")
                        if !myComment.isEmpty {
                            Button("Delete", role: .destructive) { save("") }
                                .buttonStyle(.bordered)
                                .disabled(busy)
                                .accessibilityIdentifier("deleteComment")
                        }
                        Spacer()
                        if draft.count > 1800 { Text("\(draft.count)/2000").font(.caption).foregroundStyle(draft.count > 2000 ? .red : .secondary) }
                    }
                    #endif
                    if OfflineSync.shared.pendingComment(itemID) != nil {
                        Label("Saved offline, will sync", systemImage: "icloud.and.arrow.up")
                            .font(.caption).foregroundStyle(.secondary)
                            .accessibilityIdentifier("commentPending")
                    }
                }
                Section("Ratings & Comments") {
                    if reviews == nil, error == nil {
                        ProgressView()
                    } else if others.isEmpty {
                        Text("No one else has rated this yet.").foregroundStyle(.secondary)
                            .accessibilityIdentifier("noOtherReviews")
                    }
                    ForEach(reviews?.reviews ?? [], id: \.userId) { r in row(r) }
                }
            }
            .navigationTitle("Ratings & Comments")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
            .task { await load(resetDraft: true) }
            // A star changed here: the average and your row follow once it's saved.
            .onChange(of: RatingStore.shared.saves[itemID]) { Task { await load(resetDraft: false) } }
            // Changes made offline reached the server (USER-18).
            .onChange(of: OfflineSync.shared.generation) { Task { await load(resetDraft: true) } }
            .confirmationDialog("Delete \(confirmDelete?.userName ?? "")'s comment?", isPresented: Binding(get: { confirmDelete != nil }, set: { if !$0 { confirmDelete = nil } }),
                                presenting: confirmDelete) { r in
                Button("Delete", role: .destructive) { adminDelete(r) }
            }
        }
        .accessibilityIdentifier("reviewsSheet")
    }

    private func row(_ r: Review) -> some View {
        HStack(alignment: .top, spacing: 12) {
            AvatarView(name: r.userName, url: r.avatarUrl, size: isTV ? 60 : 36)
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 6) {
                    Text(r.userName).font(.subheadline.weight(.semibold)).lineLimit(1)
                    if r.mine {
                        Text("You").font(.caption2.bold()).padding(.horizontal, 5).padding(.vertical, 1)
                            .background(Color.marqueeGold.opacity(0.25), in: Capsule()).foregroundStyle(Color.marqueeGold)
                    }
                    Spacer(minLength: 4)
                    Text(r.updatedAt, format: .relative(presentation: .named)).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                }
                if let rating = r.rating, rating > 0 { RatingGlyphs(rating: rating).font(.system(size: isTV ? 18 : 11, weight: .semibold)) }
                if let c = r.comment, !c.isEmpty { Text(c).font(.callout) }
            }
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("review.\(r.userId)")
        #if os(iOS)
        .swipeActions {
            if isAdmin, !r.mine {
                Button("Delete", role: .destructive) { confirmDelete = r }
            }
        }
        .contextMenu {
            if isAdmin, !r.mine {
                Button("Delete Comment", systemImage: "trash", role: .destructive) { confirmDelete = r }
            }
        }
        #endif
    }

    private func load(resetDraft: Bool) async {
        do {
            let r = try await app.reviews(itemID)
            reviews = r
            if resetDraft { draft = OfflineSync.shared.pendingComment(itemID) ?? r.reviews.first { $0.mine }?.comment ?? "" }
            error = nil
        } catch is CancellationError {
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func save(_ text: String) {
        busy = true
        Task {
            defer { busy = false }
            do {
                // Kept on the device when the server can't be reached; it syncs later.
                if try await app.setComment(itemID, text.trimmingCharacters(in: .whitespacesAndNewlines)) {
                    await load(resetDraft: true)
                } else {
                    draft = text.trimmingCharacters(in: .whitespacesAndNewlines)
                }
            } catch { self.error = error.localizedDescription }
        }
    }

    private func adminDelete(_ r: Review) {
        Task {
            do {
                if try await app.deleteReview(itemID, userID: r.userId) {
                    await load(resetDraft: false)
                } else {
                    reviews?.reviews.removeAll { $0.userId == r.userId }
                }
            } catch { self.error = error.localizedDescription }
        }
    }
}
