import MarqueeKit
import SwiftUI

/// Full screen, over a player: the profile's time limit or viewing hours stopped video (kids'
/// profiles). The server's reason is shown as it is, with one way out: closing the player.
struct TimeLimitScreen: View {
    let reason: String
    let close: () -> Void

    var body: some View {
        ZStack {
            Color.black.opacity(0.94).ignoresSafeArea()
            VStack(spacing: isTV ? 36 : 20) {
                Image(systemName: "hourglass")
                    .font(.system(size: isTV ? 110 : 64, weight: .semibold))
                    .foregroundStyle(Color.marqueeGold)
                    .accessibilityHidden(true)
                Text(reason)
                    .font(isTV ? .title.bold() : .title2.bold())
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("timeLimit.reason")
                Text("Ask a grown-up if you need more time.")
                    .font(isTV ? .title3 : .body)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                Button(action: close) {
                    Text("Close").frame(minWidth: isTV ? 300 : 160)
                }
                .buttonStyle(.borderedProminent)
                .padding(.top, 8)
                .accessibilityIdentifier("timeLimit.close")
            }
            .foregroundStyle(.white)
            .padding(isTV ? 80 : 32)
            .frame(maxWidth: isTV ? 1100 : 520)
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("timeLimit.screen")
        #if os(tvOS)
        .onExitCommand(perform: close)
        #endif
    }
}

/// "45 min left today" for a profile with a time limit (the profile menu), nothing otherwise.
struct TimeLeftLabel: View {
    @Environment(AppSession.self) private var app
    @State private var limits = TimeLimits.shared

    var body: some View {
        Group {
            if app.me?.isAdmin != true, let text = TimeLimits.leftLabel(limits.status) {
                Label(text, systemImage: "hourglass")
                    .font(isTV ? .headline : .subheadline)
                    .foregroundStyle(.secondary)
                    .accessibilityIdentifier("timeLimit.left")
            }
        }
        .task(id: app.me?.id) {
            guard app.me?.isAdmin != true else { return }
            await limits.refresh(app)
        }
    }
}
