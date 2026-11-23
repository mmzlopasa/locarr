import SwiftUI

/// Design tokens, the same palette as the web client (web/src/styles.css, `--lc-*`): near
/// black surfaces in three steps, hairline borders, mint accent, calm status colours.
/// Components read only these, so the look can change without touching screens.
enum Theme {
    static let background = Color(hex: 0x0B0E12)
    static let surface = Color(hex: 0x11151A)
    static let surface2 = Color(hex: 0x171C22)
    static let surface3 = Color(hex: 0x1E242C)
    static let border = Color(hex: 0x252C35)
    static let borderStrong = Color(hex: 0x343D48)
    static let text = Color(hex: 0xE4E8EE)
    static let textMuted = Color(hex: 0x8C96A4)
    static let textFaint = Color(hex: 0x5F6875)
    static let accent = Color(hex: 0x4FD1A5)
    static let accentInk = Color(hex: 0x052419)
    static let danger = Color(hex: 0xF0727A)
    static let dangerBg = Color(hex: 0x2A1417)
    static let warning = Color(hex: 0xE6B450)
    static let warningBg = Color(hex: 0x2A2214)
    static let info = Color(hex: 0x6CB6FF)
    static let infoBg = Color(hex: 0x13202E)
    static let success = Color(hex: 0x4FD1A5)
    static let successBg = Color(hex: 0x10251E)

    static let radius: CGFloat = 10
    static let posterRadius: CGFloat = 8
    static let spacing: CGFloat = 16

    /// Foreground and background of a tone ("success", "info", "warning", "danger").
    static func tone(_ tone: String?) -> (fg: Color, bg: Color) {
        switch tone {
        case "success": return (success, successBg)
        case "warning": return (warning, warningBg)
        case "danger", "error": return (danger, dangerBg)
        case "info": return (info, infoBg)
        default: return (textMuted, surface3)
        }
    }

    static func tone(for status: RequestStatus) -> String {
        switch status {
        case .available: return "success"
        case .partial, .processing, .approved: return "info"
        case .pending: return "warning"
        case .failed, .declined: return "danger"
        default: return "neutral"
        }
    }

    static func tone(for status: DiscoverStatus) -> String {
        switch status {
        case .available, .inLibrary: return "success"
        case .partial, .processing: return "info"
        case .pending: return "warning"
        default: return "neutral"
        }
    }

    /// A stable, muted hue for artwork placeholders.
    static func hue(for title: String) -> Double {
        var h = 0
        for u in title.unicodeScalars { h = (h &* 31 &+ Int(u.value)) % 360 }
        return Double(abs(h)) / 360
    }
}

extension Color {
    init(hex: UInt32) {
        self.init(red: Double((hex >> 16) & 0xFF) / 255, green: Double((hex >> 8) & 0xFF) / 255, blue: Double(hex & 0xFF) / 255)
    }
}

/// Primary action: mint with dark ink.
struct PrimaryButtonStyle: ButtonStyle {
    var tint: Color = Theme.accent
    var ink: Color = Theme.accentInk
    @Environment(\.isEnabled) private var enabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body.weight(.semibold))
            .foregroundStyle(ink)
            .padding(.horizontal, 18)
            .frame(minHeight: 48)
            .frame(maxWidth: .infinity)
            .background(tint.opacity(enabled ? (configuration.isPressed ? 0.8 : 1) : 0.35),
                        in: RoundedRectangle(cornerRadius: Theme.radius, style: .continuous))
            .scaleEffect(configuration.isPressed ? 0.98 : 1)
            .animation(.spring(response: 0.25, dampingFraction: 0.8), value: configuration.isPressed)
    }
}

/// Secondary action: a quiet surface with a hairline.
struct SecondaryButtonStyle: ButtonStyle {
    var tint: Color = Theme.text
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body.weight(.semibold))
            .foregroundStyle(tint)
            .padding(.horizontal, 16)
            .frame(minHeight: 44)
            .background(Theme.surface3.opacity(configuration.isPressed ? 0.7 : 1), in: RoundedRectangle(cornerRadius: Theme.radius, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).stroke(Theme.border))
            .scaleEffect(configuration.isPressed ? 0.98 : 1)
            .animation(.spring(response: 0.25, dampingFraction: 0.8), value: configuration.isPressed)
    }
}

/// Cards sink a touch when pressed.
struct PressStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed ? 0.97 : 1)
            .opacity(configuration.isPressed ? 0.9 : 1)
            .animation(.spring(response: 0.3, dampingFraction: 0.75), value: configuration.isPressed)
    }
}

extension View {
    /// A raised panel: surface, hairline border, rounded corners.
    func card(padding: CGFloat = 14) -> some View {
        self.padding(padding)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.radius, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).stroke(Theme.border))
    }

    /// Dark list and navigation chrome for a screen.
    func screenStyle() -> some View {
        self.scrollContentBackground(.hidden)
            .background(Theme.background.ignoresSafeArea())
            .toolbarBackground(Theme.background, for: .navigationBar)
    }
}
