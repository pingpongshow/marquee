import AVFoundation
import CoreMedia
import Foundation
import MarqueeAPI

public typealias SubtitleStyle = Components.Schemas.SubtitleStyle

/// How text subtitles look (PLAY-20). It follows the person to every app; absent fields use
/// the defaults: medium, white, outline, bottom.
public extension Components.Schemas.SubtitleStyle {
    static let colors: [(hex: String, name: String)] = [("#FFFFFF", "White"), ("#FFFF00", "Yellow"), ("#00FFFF", "Cyan"), ("#00FF00", "Green")]

    var sizeValue: SizePayload { size ?? .medium }
    var colorValue: String { (color ?? "#FFFFFF").uppercased() }
    var backgroundValue: BackgroundPayload { background ?? .outline }
    var positionValue: PositionPayload { position ?? .bottom }

    /// Text size relative to the player's default, in percent.
    var relativeSize: Double {
        switch sizeValue {
        case .small: 75
        case .medium: 100
        case .large: 130
        case .huge: 165
        }
    }

    /// The colour as red, green and blue (0–1).
    var rgb: (red: Double, green: Double, blue: Double) {
        let hex = colorValue.dropFirst()
        guard hex.count == 6, let v = UInt32(hex, radix: 16) else { return (1, 1, 1) }
        return (Double((v >> 16) & 0xFF) / 255, Double((v >> 8) & 0xFF) / 255, Double(v & 0xFF) / 255)
    }

    /// The style as AVPlayerItem text style rules. They apply to text subtitles AVFoundation
    /// draws (WebVTT and tx3g); styled ASS subtitles are burned in by the server with their own look.
    /// Position: "raised" moves cues up with the line-position attribute, which AVFoundation
    /// honours for most cues; cues that place themselves keep their own position.
    var textStyleRules: [AVTextStyleRule] {
        let c = rgb
        var attrs: [String: Any] = [
            kCMTextMarkupAttribute_RelativeFontSize as String: relativeSize,
            kCMTextMarkupAttribute_ForegroundColorARGB as String: [1, c.red, c.green, c.blue],
        ]
        switch backgroundValue {
        case .none:
            attrs[kCMTextMarkupAttribute_CharacterEdgeStyle as String] = kCMTextMarkupCharacterEdgeStyle_DropShadow
            attrs[kCMTextMarkupAttribute_BackgroundColorARGB as String] = [0, 0, 0, 0]
        case .outline:
            attrs[kCMTextMarkupAttribute_CharacterEdgeStyle as String] = kCMTextMarkupCharacterEdgeStyle_Uniform
            attrs[kCMTextMarkupAttribute_BackgroundColorARGB as String] = [0, 0, 0, 0]
        case .translucent:
            attrs[kCMTextMarkupAttribute_CharacterEdgeStyle as String] = kCMTextMarkupCharacterEdgeStyle_None
            attrs[kCMTextMarkupAttribute_BackgroundColorARGB as String] = [0.55, 0, 0, 0]
        case .opaque:
            attrs[kCMTextMarkupAttribute_CharacterEdgeStyle as String] = kCMTextMarkupCharacterEdgeStyle_None
            attrs[kCMTextMarkupAttribute_BackgroundColorARGB as String] = [1, 0, 0, 0]
        }
        if positionValue == .raised {
            attrs[kCMTextMarkupAttribute_OrthogonalLinePositionPercentageRelativeToWritingDirection as String] = 72
        }
        return AVTextStyleRule(textMarkupAttributes: attrs).map { [$0] } ?? []
    }
}

public extension Components.Schemas.SubtitleStyle.SizePayload {
    var label: String { rawValue.capitalized }
}

public extension Components.Schemas.SubtitleStyle.BackgroundPayload {
    var label: String {
        switch self {
        case .none: "None (shadow)"
        case .outline: "Outline"
        case .translucent: "Translucent box"
        case .opaque: "Solid box"
        }
    }
}

public extension Components.Schemas.SubtitleStyle.PositionPayload {
    var label: String { self == .bottom ? "Bottom" : "Raised" }
}

@MainActor
public extension AppSession {
    /// The signed-in person's subtitle style.
    var subtitleStyle: SubtitleStyle { me?.preferences.subtitleStyle ?? .init() }

    func setSubtitleStyle(_ style: SubtitleStyle) async throws {
        guard let client else { throw MarqueeError("Not connected") }
        // Preferences are replaced as a whole, so the others are sent back unchanged.
        var prefs = me?.preferences ?? .init()
        prefs.subtitleStyle = style
        switch try await client.updateMe(body: .json(.init(preferences: prefs))) {
        case .ok: await refreshMe()
        case .badRequest(let r): throw MarqueeError((try? r.body.json.message) ?? "Couldn't save.")
        default: throw MarqueeError("Couldn't save.")
        }
    }
}
