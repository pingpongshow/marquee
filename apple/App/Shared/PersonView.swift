import MarqueeKit
import SwiftUI

struct PersonView: View {
    @Environment(AppSession.self) private var app
    let id: Int64
    @State private var person: Person?

    var body: some View {
        ScrollView {
            if let p = person {
                VStack(alignment: .leading, spacing: 20) {
                    HStack(spacing: 18) {
                        ZStack {
                            Circle().fill(Color.gray.opacity(0.3))
                            Image(systemName: "person.fill").font(.largeTitle).foregroundStyle(.secondary)
                            if p.hasPhoto { AsyncImage(url: app.personPhotoURL(p.id, width: 140)) { $0.image?.resizable().scaledToFill() } }
                        }
                        .frame(width: 120, height: 120).clipShape(Circle())
                        VStack(alignment: .leading) {
                            Text(p.name).font(.largeTitle.bold())
                            Text("\(p.credits.count) titles in your libraries").foregroundStyle(.secondary)
                        }
                    }
                    .padding(.horizontal, sidePadding)
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: PosterCard.defaultWidth), spacing: 14, alignment: .top)], spacing: 18) {
                        ForEach(p.credits, id: \.item.id) { c in PosterCard(item: c.item) }
                    }
                    .padding(.horizontal, sidePadding)
                }
                .padding(.vertical)
            } else {
                ProgressView().padding(.top, 80)
            }
        }
        .navigationTitle(person?.name ?? "")
        .task { person = try? await app.person(id) }
    }
}
