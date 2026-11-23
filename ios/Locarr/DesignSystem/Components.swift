import SwiftUI

/// Artwork from the server (token-authenticated, cached), with a typographic placeholder.
struct RemoteImage: View {
    let url: URL?
    var title: String = ""
    var maxPixel: CGFloat = 600
    @Environment(Session.self) private var session
    @State private var image: UIImage?

    var body: some View {
        Color.clear
            .overlay {
                ZStack {
                    Placeholder(title: title).opacity(image == nil ? 1 : 0)
                    if let image {
                        Image(uiImage: image).resizable().scaledToFill().transition(.opacity)
                    }
                }
            }
            .clipped()
            .animation(.easeOut(duration: 0.25), value: image != nil)
            .task(id: url) {
                guard let url else { image = nil; return }
                image = await ImageLoader.shared.image(url: url, token: session.client.token, maxPixel: maxPixel)
            }
            .accessibilityHidden(true)
    }
}

/// When there is no artwork: the title on a muted gradient.
struct Placeholder: View {
    let title: String
    var body: some View {
        let h = Theme.hue(for: title)
        ZStack(alignment: .bottomLeading) {
            LinearGradient(colors: [Color(hue: h, saturation: 0.25, brightness: 0.22), Theme.surface2],
                           startPoint: .topLeading, endPoint: .bottomTrailing)
            if !title.isEmpty {
                Text(verbatim: title)
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(Theme.text.opacity(0.8))
                    .lineLimit(3)
                    .padding(8)
            }
        }
    }
}

/// A poster with a fixed aspect ratio (2:3, square for music).
struct Poster: View {
    let url: URL?
    let title: String
    var square = false
    var width: CGFloat? = nil

    var body: some View {
        RemoteImage(url: url, title: title)
            .aspectRatio(square ? 1 : 2.0 / 3.0, contentMode: .fit)
            .frame(width: width)
            .clipShape(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous).stroke(Color.white.opacity(0.06)))
    }
}

/// A small coloured label (status, kind, quality).
struct Pill: View {
    let text: String
    var tone: String? = nil
    var body: some View {
        let c = Theme.tone(tone)
        Text(verbatim: text)
            .font(.caption2.weight(.semibold))
            .foregroundStyle(c.fg)
            .padding(.horizontal, 7)
            .padding(.vertical, 3)
            .background(c.bg, in: Capsule())
            .lineLimit(1)
    }
}

/// A short message with a tone (the "why isn't it here?" answer, warnings).
struct Callout: View {
    let text: String
    var tone: String? = "info"
    var systemImage: String? = nil

    var body: some View {
        let c = Theme.tone(tone)
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: systemImage ?? icon).foregroundStyle(c.fg).font(.body.weight(.semibold))
            Text(verbatim: text).font(.callout).foregroundStyle(Theme.text).fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: 0)
        }
        .padding(12)
        .background(c.bg, in: RoundedRectangle(cornerRadius: Theme.radius, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).stroke(c.fg.opacity(0.25)))
        .accessibilityElement(children: .combine)
    }

    private var icon: String {
        switch tone {
        case "success": return "checkmark.circle.fill"
        case "warning": return "exclamationmark.triangle.fill"
        case "danger", "error": return "xmark.octagon.fill"
        default: return "info.circle.fill"
        }
    }
}

struct SectionTitle: View {
    let text: String
    var trailing: AnyView? = nil
    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            Text(verbatim: text).font(.title3.weight(.bold)).foregroundStyle(Theme.text)
                .accessibilityAddTraits(.isHeader)
            Spacer()
            if let trailing { trailing }
        }
    }
}

/// Loading, empty and error states.
struct LoadingState: View {
    var body: some View {
        ProgressView().tint(Theme.accent).frame(maxWidth: .infinity, minHeight: 160)
            .accessibilityLabel(Text(verbatim: L("common.loading")))
    }
}

struct EmptyState: View {
    let text: String
    var systemImage = "tray"
    var body: some View {
        VStack(spacing: 10) {
            Image(systemName: systemImage).font(.system(size: 34, weight: .light)).foregroundStyle(Theme.textFaint)
            Text(verbatim: text).font(.callout).foregroundStyle(Theme.textMuted).multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity, minHeight: 180)
        .padding()
        .accessibilityElement(children: .combine)
    }
}

struct ErrorState: View {
    let message: String
    var retry: (() async -> Void)? = nil
    var body: some View {
        VStack(spacing: 12) {
            Image(systemName: "wifi.exclamationmark").font(.system(size: 30, weight: .light)).foregroundStyle(Theme.danger)
            Text(verbatim: message).font(.callout).foregroundStyle(Theme.text).multilineTextAlignment(.center)
            if let retry {
                Button { Task { await retry() } } label: { Text(verbatim: L("app.retry")) }
                    .buttonStyle(SecondaryButtonStyle())
            }
        }
        .frame(maxWidth: .infinity, minHeight: 180)
        .padding()
    }
}

/// A label and value on one line (details lists).
struct InfoRow: View {
    let label: String
    let value: String
    var mono = false
    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            Text(verbatim: label).foregroundStyle(Theme.textMuted)
            Spacer(minLength: 12)
            Text(verbatim: value)
                .font(mono ? .callout.monospaced() : .callout)
                .foregroundStyle(Theme.text)
                .multilineTextAlignment(.trailing)
                .textSelection(.enabled)
        }
        .font(.callout)
        .accessibilityElement(children: .combine)
    }
}

/// A thin progress bar.
struct ProgressBar: View {
    let value: Double
    var tint: Color = Theme.accent
    var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                Capsule().fill(Theme.surface3)
                Capsule().fill(tint).frame(width: max(0, min(1, value)) * geo.size.width)
            }
        }
        .frame(height: 5)
        .accessibilityElement()
        .accessibilityValue(Text(verbatim: Fmt.percent(value)))
    }
}

/// A transient confirmation at the bottom of the screen.
struct Toast: ViewModifier {
    @Binding var text: String?
    func body(content: Content) -> some View {
        content.overlay(alignment: .bottom) {
            if let text {
                Text(verbatim: text)
                    .font(.callout.weight(.medium))
                    .foregroundStyle(Theme.text)
                    .padding(.horizontal, 16).padding(.vertical, 12)
                    .background(Theme.surface3, in: Capsule())
                    .overlay(Capsule().stroke(Theme.borderStrong))
                    .padding(.bottom, 24)
                    .padding(.horizontal)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                    .task(id: text) {
                        UIAccessibility.post(notification: .announcement, argument: text)
                        try? await Task.sleep(for: .seconds(3))
                        withAnimation { self.text = nil }
                    }
            }
        }
        .animation(.spring(response: 0.35, dampingFraction: 0.85), value: text)
    }
}

extension View {
    func toast(_ text: Binding<String?>) -> some View { modifier(Toast(text: text)) }
}
