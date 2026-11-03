import MarqueeKit
import SwiftUI

extension AppSession {
    /// An admin has turned In Town on, this isn't a kid's profile, and the server's there.
    /// Nothing about it shows otherwise.
    var inTownOn: Bool { info?.inTown == true && !inTownBarred && !offlineUI }

    /// A kid's profile has no In Town (set in the web admin; the server refuses them too), and
    /// nothing shows until it's known who this is.
    var inTownBarred: Bool { me.map { $0.restrictions.isKid == true } ?? true }
}

/// In Town (MUSIC-27): artists in your libraries on tour, then shows near you.
struct InTownView: View {
    @Environment(AppSession.self) private var app
    /// The artist the page was opened for, with all their shows above the rest.
    @State private var focus: Int64?
    @State private var page: InTownPage?
    @State private var error: String?
    /// Looking again after a change of location, with the last answer still showing.
    @State private var reloading = false
    @State private var editing = false

    init(artist: Int64?) {
        _focus = State(initialValue: artist)
    }

    var body: some View {
        Group {
            if app.inTownBarred {
                Color.clear
            } else if let page {
                if page.enabled { content(page) } else { turnedOff }
            } else if let error {
                ErrorBanner(message: error).padding().frame(maxHeight: .infinity, alignment: .top)
            } else {
                ProgressView("Looking for shows").frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .navigationTitle("In Town")
        .task(id: app.inTownBarred) {
            // Once per visit, not on every appear (coming back from an artist).
            if page == nil, !app.inTownBarred { await load() }
        }
    }

    private var turnedOff: some View {
        Text("In Town isn't turned on. An administrator can turn it on in Settings → Music.")
            .foregroundStyle(.secondary)
            .padding()
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }

    private func content(_ d: InTownPage) -> some View {
        ScrollViewReader { proxy in
            ScrollView {
                VStack(alignment: .leading, spacing: 32) {
                    if let error { ErrorBanner(message: error) }
                    if let focus {
                        InTownFocus(itemID: focus, attribution: d.attribution.name) { self.focus = nil }
                            .id(focus)
                    }
                    artists(d) { id in
                        focus = id
                        withAnimation { proxy.scrollTo("inTownTop", anchor: .top) }
                    }
                    near(d)
                    InTownAttributionLine(attribution: d.attribution)
                }
                .padding(sidePadding)
                .frame(maxWidth: .infinity, alignment: .leading)
                .id("inTownTop")
            }
        }
    }

    // MARK: Your artists

    private func artists(_ d: InTownPage, more: @escaping (Int64) -> Void) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Your artists on tour").font(.title3.bold())
            if d.artists.isEmpty {
                Text("No shows found yet for artists in your libraries. The server looks them up a few at a time, so check back later.")
                    .foregroundStyle(.secondary)
            } else {
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 400), spacing: 16, alignment: .top)], alignment: .leading, spacing: 16) {
                    ForEach(d.artists, id: \.artist.id) { row in
                        InTownArtistCard(row: row, attribution: d.attribution.name, more: more)
                    }
                }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("inTown.artists")
    }

    // MARK: Near you

    private func near(_ d: InTownPage) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .firstTextBaseline, spacing: 12) {
                Text(InTownFormat.nearTitle(d.location)).font(.title3.bold())
                if let l = d.location, let distance = l.distance, let unit = l.unit {
                    Text("Within \(distance) \(unit.rawValue)").font(.subheadline).foregroundStyle(.secondary)
                }
                if reloading { ProgressView().controlSize(.small) }
                Spacer(minLength: 0)
                if d.location != nil {
                    Button { editing.toggle() } label: { Label("Change", systemImage: "mappin.and.ellipse") }
                        .buttonStyle(.plain)
                        .foregroundStyle(.secondary)
                        .accessibilityIdentifier("inTown.changeLocation")
                }
            }
            if d.needsLocation || editing {
                VStack(alignment: .leading, spacing: 14) {
                    if d.needsLocation {
                        Text("Where are you? Set a location to see shows near you.").font(.headline)
                    }
                    InTownLocationForm {
                        editing = false
                        Task { await load() }
                    }
                }
                .padding(16)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Color.secondary.opacity(0.12), in: RoundedRectangle(cornerRadius: 12))
            }
            if let nearError = d.nearError {
                Label(nearError, systemImage: "info.circle").foregroundStyle(.secondary)
            }
            if !d.needsLocation, d.locationFound == false, d.near.isEmpty {
                Text("Ticketmaster doesn't recognise that location. Try your nearest city instead.").foregroundStyle(.secondary)
            }
            if !d.near.isEmpty {
                VStack(spacing: 0) {
                    ForEach(Array(d.near.enumerated()), id: \.element.id) { i, show in
                        if i > 0 { Divider() }
                        InTownShowRow(show: show, attribution: d.attribution.name)
                    }
                }
                .background(Color.secondary.opacity(0.12), in: RoundedRectangle(cornerRadius: 12))
            }
            if !d.needsLocation, d.locationFound != false, d.near.isEmpty, d.nearError == nil {
                Text("No shows found near you right now.").foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("inTown.near")
    }

    private func load() async {
        if page != nil { reloading = true }
        defer { reloading = false }
        do {
            page = try await app.inTown()
            error = nil
        } catch is CancellationError {
        } catch {
            self.error = error.localizedDescription
        }
    }
}

/// The artist the page was opened for, with all their shows.
private struct InTownFocus: View {
    @Environment(AppSession.self) private var app
    let itemID: Int64
    let attribution: String
    let allArtists: () -> Void
    @State private var name: String?
    @State private var shows: [InTownShow]?
    @State private var error: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .firstTextBaseline) {
                Text(title).font(.title3.bold())
                Spacer(minLength: 12)
                Button("All artists", action: allArtists)
                    .buttonStyle(.plain)
                    .foregroundStyle(.secondary)
                    .accessibilityIdentifier("inTown.allArtists")
            }
            if let shows {
                if shows.isEmpty {
                    Text("No upcoming shows found.").foregroundStyle(.secondary)
                } else {
                    VStack(spacing: 0) {
                        ForEach(Array(shows.enumerated()), id: \.element.id) { i, show in
                            if i > 0 { Divider() }
                            InTownShowRow(show: show, attribution: attribution, showArtist: false)
                        }
                    }
                    .background(Color.secondary.opacity(0.12), in: RoundedRectangle(cornerRadius: 12))
                }
            } else if let error {
                ErrorBanner(message: error)
            } else {
                ProgressView().frame(maxWidth: .infinity)
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("inTown.focus")
        .task(id: itemID) { await load() }
    }

    private var title: String {
        if let name, let shows { return InTownFormat.headline(artist: name, count: shows.count) }
        return "Upcoming shows"
    }

    private func load() async {
        async let artist = try? app.item(itemID)
        do {
            shows = try await app.itemShows(itemID).shows
        } catch is CancellationError {
        } catch {
            self.error = error.localizedDescription
        }
        name = await artist?.title
    }
}

/// One artist in the upper area: their next few shows, with the rest a click away.
private struct InTownArtistCard: View {
    let row: InTownArtist
    let attribution: String
    let more: (Int64) -> Void

    var body: some View {
        VStack(spacing: 0) {
            NavigationLink(value: Route.item(row.artist.id)) {
                HStack(spacing: 12) {
                    ArtworkView(item: row.artist, shape: .square, width: 56).frame(width: 56)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(row.artist.title).font(.headline).lineLimit(1)
                        Text(counts).font(.subheadline).foregroundStyle(.secondary)
                        if let reason = row.reason {
                            Text(reason).font(.caption).foregroundStyle(Color.marqueeGold)
                        }
                    }
                    Spacer(minLength: 0)
                }
                .padding(12)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("inTown.artist.\(row.artist.id)")
            ForEach(row.shows, id: \.id) { show in
                Divider()
                InTownShowRow(show: show, attribution: attribution, showArtist: false)
            }
            let others = row.showCount - row.shows.count
            if others > 0 {
                Divider()
                Button { more(row.artist.id) } label: {
                    HStack(spacing: 4) {
                        Text("\(others) more")
                        Image(systemName: "chevron.right").font(.caption.bold())
                        Spacer(minLength: 0)
                    }
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("inTown.more.\(row.artist.id)")
            }
        }
        .background(Color.secondary.opacity(0.12), in: RoundedRectangle(cornerRadius: 12))
    }

    private var counts: String {
        let shows = InTownFormat.showCount(row.showCount)
        return InTownFormat.nearLine(row.nearCount).map { "\(shows) · \($0)" } ?? shows
    }
}

/// One show: when and where at the venue's own time, why it's here, and a link to its tickets.
private struct InTownShowRow: View {
    @Environment(\.openURL) private var openURL
    let show: InTownShow
    let attribution: String
    var showArtist = true

    var body: some View {
        HStack(spacing: 12) {
            if showArtist { artwork }
            VStack(alignment: .leading, spacing: 2) {
                if showArtist { artistLine }
                HStack(spacing: 8) {
                    Text(InTownFormat.when(show))
                    if show.status != .scheduled {
                        Text(show.status == .postponed ? "Postponed" : "Rescheduled")
                            .font(.caption2.weight(.semibold))
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Color.secondary.opacity(0.2), in: Capsule())
                    }
                }
                Text(placeLine).font(.subheadline).foregroundStyle(.secondary)
                if let reason = show.reason {
                    Text(reason).font(.caption).foregroundStyle(Color.marqueeGold)
                        .accessibilityIdentifier("inTown.reason")
                }
            }
            Spacer(minLength: 8)
            if let url = InTownFormat.webURL(show.ticketsUrl) {
                Button { openURL(url) } label: {
                    Label("Tickets on \(attribution)", systemImage: "arrow.up.right.square").lineLimit(1)
                }
                .help(url.absoluteString)
                .accessibilityIdentifier("inTown.tickets")
            }
        }
        .padding(12)
        .accessibilityElement(children: .contain)
    }

    private var placeLine: String {
        let place = InTownFormat.place(show)
        return InTownFormat.distance(show.distance, unit: show.distanceUnit?.rawValue).map { "\(place) · \($0)" } ?? place
    }

    @ViewBuilder private var artwork: some View {
        if let artist = show.artist {
            NavigationLink(value: Route.item(artist.id)) {
                ArtworkView(item: artist, shape: .square, width: 48).frame(width: 48)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(artist.title)
        } else {
            Image(systemName: "music.note")
                .foregroundStyle(.secondary)
                .frame(width: 48, height: 48)
                .background(Color.secondary.opacity(0.15), in: RoundedRectangle(cornerRadius: 6))
                .accessibilityHidden(true)
        }
    }

    private var artistLine: some View {
        HStack(spacing: 4) {
            if let id = show.artistItemId {
                NavigationLink(show.artistName, value: Route.item(id))
                    .buttonStyle(.plain)
                    .font(.headline)
                    .accessibilityIdentifier("inTown.showArtist.\(id)")
            } else {
                Text(show.artistName).font(.headline)
            }
            if show.support, let headliner = show.lineup.first {
                Text("supporting \(headliner)").foregroundStyle(.secondary)
            }
        }
        .lineLimit(1)
    }
}

/// Who the shows come from, linked, wherever they're listed.
private struct InTownAttributionLine: View {
    @Environment(\.openURL) private var openURL
    let attribution: InTownAttribution

    var body: some View {
        HStack(spacing: 0) {
            if let url = InTownFormat.webURL(attribution.url) {
                Button { openURL(url) } label: { Text(attribution.text).underline() }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("inTown.attribution")
            } else {
                Text(attribution.text)
            }
            Text(". Tickets are sold on \(attribution.name)'s site.")
        }
        .font(.caption)
        .foregroundStyle(.secondary)
    }
}

/// Banner on an artist's page: how many shows and the next one. The page only asks for it once the
/// shows are known, so it never holds the page up.
struct ArtistShowsBanner: View {
    let artist: ItemDetail
    let shows: ItemShows

    var body: some View {
        if shows.enabled, let next = shows.shows.first {
            let headline = Text(InTownFormat.headline(artist: artist.title, count: shows.shows.count)).fontWeight(.medium)
            let rest = Text(" · \(InTownFormat.nextLine(next, nearCount: shows.nearCount))").foregroundStyle(.secondary)
            NavigationLink(value: Route.inTown(artist: artist.id)) {
                HStack(spacing: 12) {
                    Image(systemName: "ticket").font(.title3).foregroundStyle(Color.marqueeGold)
                    Text("\(headline)\(rest)")
                    Spacer(minLength: 8)
                    Image(systemName: "chevron.right").font(.footnote.bold()).foregroundStyle(.tertiary)
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .background(Color.secondary.opacity(0.12), in: RoundedRectangle(cornerRadius: 12))
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .padding(.horizontal, sidePadding)
            .accessibilityIdentifier("item.showsBanner")
        }
    }
}

/// Where someone is, for shows near them: a postcode or a city, never GPS.
struct InTownLocationForm: View {
    @Environment(AppSession.self) private var app
    var onSaved: (() -> Void)?
    @State private var saved: InTownLocation?
    // Unset fields follow what's saved until they're edited.
    @State private var postalCode: String?
    @State private var city: String?
    @State private var country: String?
    @State private var unit: InTownLocation.UnitPayload?
    @State private var distance: String?
    @State private var busy = false
    @State private var error: String?

    private var currentPostalCode: String { postalCode ?? saved?.postalCode ?? "" }
    private var currentCity: String { city ?? saved?.city ?? "" }
    private var currentCountry: String { country ?? saved?.countryCode ?? InTownFormat.guessCountry() }
    private var currentUnit: InTownLocation.UnitPayload { unit ?? saved?.unit ?? InTownFormat.unit(forCountry: currentCountry) }
    private var currentDistance: String { distance ?? String(saved?.distance ?? InTownFormat.defaultDistance(currentUnit)) }
    private var hasPlace: Bool { !currentPostalCode.trimmed.isEmpty || !currentCity.trimmed.isEmpty }

    /// One or the other: typing a postcode clears the city, and the other way round.
    private func setPostalCode(_ value: String) {
        guard value != currentPostalCode else { return }
        postalCode = String(value.prefix(12))
        if !value.isEmpty { city = "" }
    }

    private func setCity(_ value: String) {
        guard value != currentCity else { return }
        city = String(value.prefix(80))
        if !value.isEmpty { postalCode = "" }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Grid(alignment: .leadingFirstTextBaseline, horizontalSpacing: 12, verticalSpacing: 10) {
                GridRow {
                    Text("Postcode or ZIP")
                    TextField("Postcode or ZIP", text: Binding(get: { currentPostalCode }, set: setPostalCode))
                        .labelsHidden()
                        .accessibilityIdentifier("inTown.postalCode")
                }
                GridRow {
                    Text("Or city")
                    TextField("Or city", text: Binding(get: { currentCity }, set: setCity))
                        .labelsHidden()
                        .accessibilityIdentifier("inTown.city")
                }
                GridRow {
                    Text("Country")
                    Picker("Country", selection: Binding(get: { currentCountry }, set: { country = $0 })) {
                        ForEach(InTownFormat.countries, id: \.self) { Text(name(of: $0)).tag($0) }
                        if !InTownFormat.countries.contains(currentCountry) { Text(currentCountry).tag(currentCountry) }
                    }
                    .labelsHidden()
                    .accessibilityIdentifier("inTown.country")
                }
                GridRow {
                    Text("How far you'd go")
                    HStack {
                        // Text, not a number: a number only reaches the form once the field loses focus.
                        TextField("How far you'd go", text: Binding(get: { currentDistance }, set: { distance = String($0.filter(\.isNumber).prefix(4)) }))
                            .labelsHidden()
                            .frame(width: 70)
                            .accessibilityIdentifier("inTown.distance")
                        Picker("Unit", selection: Binding(get: { currentUnit }, set: { unit = $0 })) {
                            Text("miles").tag(InTownLocation.UnitPayload.miles)
                            Text("km").tag(InTownLocation.UnitPayload.km)
                        }
                        .labelsHidden()
                        .fixedSize()
                        .accessibilityIdentifier("inTown.unit")
                    }
                }
            }
            .textFieldStyle(.roundedBorder) // In Settings' form too, where an empty field would look like none.
            .frame(maxWidth: 480, alignment: .leading)
            Text("To find shows, this server sends your postcode or city to Ticketmaster, without your name. It never uses GPS, and only you can see what you enter here. In the UK, Ireland, Canada and the Netherlands only the first part of a postcode is kept. If a postcode finds nothing, try your city.")
                .font(.callout)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            if let error {
                Text(error).font(.caption).foregroundStyle(.red)
                    .accessibilityIdentifier("inTown.locationError")
            }
            HStack {
                Button("Save") { Task { await save() } }
                    .buttonStyle(.borderedProminent)
                    .disabled(busy || !hasPlace)
                    .accessibilityIdentifier("inTown.saveLocation")
                if saved != nil {
                    Button("Remove") { Task { await remove() } }
                        .disabled(busy)
                        .accessibilityIdentifier("inTown.removeLocation")
                }
                if busy { ProgressView().controlSize(.small) }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("inTown.location")
        .task { saved = try? await app.myLocation() }
        #if DEBUG
        .onReceive(NotificationCenter.default.publisher(for: MacTestDriver.saveLocation)) { _ in Task { await save() } }
        #endif
    }

    private func name(of code: String) -> String {
        Locale.current.localizedString(forRegionCode: code) ?? code
    }

    private func save() async {
        busy = true
        defer { busy = false }
        // Only ever one of them: the server refuses both, which an older saved location can have.
        let postal = currentPostalCode.trimmed
        let location = InTownLocation(postalCode: postal.isEmpty ? nil : postal,
                                      city: postal.isEmpty ? currentCity.trimmed : nil,
                                      countryCode: currentCountry, distance: Int(currentDistance), unit: currentUnit)
        do {
            done(saved: try await app.setMyLocation(location))
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func remove() async {
        busy = true
        defer { busy = false }
        do {
            try await app.removeMyLocation()
            done(saved: nil)
        } catch {
            self.error = error.localizedDescription
        }
    }

    /// Fields go back to following what's saved: the server may have shortened a postcode.
    private func done(saved: InTownLocation?) {
        self.saved = saved
        postalCode = nil
        city = nil
        country = nil
        unit = nil
        distance = nil
        error = nil
        onSaved?()
    }
}

private extension String {
    var trimmed: String { trimmingCharacters(in: .whitespacesAndNewlines) }
}
