import MarqueeKit
import SwiftUI

/// First run: find the server on the LAN or enter its address.
struct ConnectView: View {
    @Environment(AppSession.self) private var app
    @State private var discovery = ServerDiscovery()
    @State private var lan = ""
    @State private var remote = ""
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        #if os(tvOS)
        tvBody
        #else
        phoneBody
        #endif
    }

    #if os(tvOS)
    @State private var manual = false

    private var tvBody: some View {
        NavigationStack {
            VStack(spacing: 40) {
                Image(systemName: "film.stack").font(.system(size: 80)).foregroundStyle(Color.marqueeGold)
                Text("Welcome to Marquee").font(.title.bold())
                if discovery.servers.isEmpty {
                    HStack(spacing: 16) { ProgressView(); Text("Looking for servers on your network…").foregroundStyle(.secondary) }
                } else {
                    Text("Choose your server").foregroundStyle(.secondary)
                }
                HStack(spacing: 40) {
                    ForEach(discovery.servers) { s in
                        Button {
                            connect { try await app.addDiscovered(s) }
                        } label: {
                            VStack(spacing: 10) {
                                Image(systemName: "server.rack").font(.system(size: 50))
                                Text(s.name).font(.headline)
                                Text(s.url.host() ?? "").font(.caption).foregroundStyle(.secondary)
                            }
                            .frame(width: 360, height: 220)
                        }
                        .buttonStyle(.card)
                        .accessibilityLabel("\(s.name), \(s.url.host() ?? "")")
                    }
                }
                if busy { ProgressView() }
                if let error { ErrorBanner(message: error).frame(maxWidth: 900) }
                Button("Enter an address") { manual = true }
            }
            .padding(80)
            .sheet(isPresented: $manual) {
                Form {
                    TextField("Home address, e.g. 10.1.1.10:32500", text: $lan)
                    TextField("Tailscale address (optional)", text: $remote)
                    Button("Connect") {
                        manual = false
                        connect { try await app.addServer(lan: lan, remote: remote) }
                    }
                    .disabled(lan.isEmpty && remote.isEmpty)
                }
            }
        }
        .onAppear { discovery.start() }
        .onDisappear { discovery.stop() }
    }
    #endif

    private var phoneBody: some View {
        NavigationStack {
            Form {
                Section {
                    VStack(spacing: 8) {
                        Image(systemName: "film.stack").font(.system(size: 44)).foregroundStyle(Color.marqueeGold)
                        Text("Welcome to Marquee").font(.title2.bold())
                        Text("Connect to your Marquee server.").foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity).padding(.vertical, 8)
                }
                Section("On this network") {
                    if discovery.servers.isEmpty {
                        HStack(spacing: 10) {
                            ProgressView()
                            Text("Looking for servers…").foregroundStyle(.secondary)
                        }
                    }
                    ForEach(discovery.servers) { s in
                        Button {
                            connect { try await app.addDiscovered(s) }
                        } label: {
                            LabeledContent {
                                Image(systemName: "chevron.right").foregroundStyle(.secondary)
                            } label: {
                                Text(s.name)
                                Text(s.url.host() ?? "").font(.caption).foregroundStyle(.secondary)
                            }
                        }
                    }
                }
                Section {
                    TextField("Home address, e.g. 10.1.1.10:32500", text: $lan)
                        .textContentType(.URL)
                        #if os(iOS)
                        .keyboardType(.URL).textInputAutocapitalization(.never)
                        #endif
                        .autocorrectionDisabled()
                    TextField("Tailscale address (optional)", text: $remote)
                        .textContentType(.URL)
                        #if os(iOS)
                        .keyboardType(.URL).textInputAutocapitalization(.never)
                        #endif
                        .autocorrectionDisabled()
                    Button {
                        connect { try await app.addServer(lan: lan, remote: remote) }
                    } label: {
                        if busy { ProgressView() } else { Text("Connect") }
                    }
                    .disabled(busy || (lan.isEmpty && remote.isEmpty))
                } header: {
                    Text("Enter an address")
                } footer: {
                    Text("At home the app connects directly. Away from home it uses the Tailscale address.")
                }
                if let error { Section { ErrorBanner(message: error) } }
            }
            .navigationTitle("Marquee")
        }
        .onAppear { discovery.start() }
        .onDisappear { discovery.stop() }
    }

    private func connect(_ op: @escaping () async throws -> Void) {
        busy = true
        error = nil
        Task {
            do { try await op() } catch { self.error = error.localizedDescription }
            busy = false
        }
    }
}
