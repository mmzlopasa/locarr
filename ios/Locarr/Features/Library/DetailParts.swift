import SwiftUI

/// "Why isn't it here?" for one library item, with Retry now and the full timeline.
struct WhyCard: View {
    let itemRef: String
    let title: String
    var reloadTick = 0
    @Environment(Session.self) private var session
    @State private var explanation: Explanation?
    @State private var retrying = false
    @State private var message: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Text(verbatim: explanation?.code == "available" ? L("explain.titleOk") : L("explain.title"))
                    .font(.headline).foregroundStyle(Theme.text)
                Spacer()
                NavigationLink(value: Route.timeline(ref: itemRef, title: title)) {
                    Text(verbatim: L("timeline.title")).font(.subheadline)
                }
            }
            if let explanation {
                Callout(text: Describe.explanation(explanation), tone: explanation.tone)
                if explanation.code != "available" && explanation.code != "downloading" && explanation.code != "importing" {
                    Button {
                        Task { await retry() }
                    } label: {
                        Label { Text(verbatim: retrying ? L("explain.retrying") : L("explain.retryNow")) } icon: { Image(systemName: "arrow.clockwise") }
                    }
                    .buttonStyle(SecondaryButtonStyle())
                    .disabled(retrying)
                    Text(verbatim: L("explain.retryHint")).font(.caption).foregroundStyle(Theme.textFaint)
                }
            } else {
                ProgressView().tint(Theme.accent)
            }
            if let message { Text(verbatim: message).font(.footnote).foregroundStyle(Theme.textMuted) }
        }
        .card()
        .task(id: "\(itemRef)|\(reloadTick)") { await load() }
    }

    private func load() async {
        if let t = try? await session.client.explain(itemRef) { explanation = t }
    }

    private func retry() async {
        retrying = true
        defer { retrying = false }
        do {
            _ = try await session.client.retry(itemRef)
            message = L("explain.retryQueued")
            try? await Task.sleep(for: .seconds(2))
            await load()
        } catch {
            message = error.userMessage
        }
    }
}

/// A file in the library: path, size, quality, formats, media info.
struct FileCard: View {
    let file: MediaFile
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(verbatim: L("library.file")).font(.headline).foregroundStyle(Theme.text)
            Text(verbatim: file.path).font(.caption.monospaced()).foregroundStyle(Theme.textMuted).textSelection(.enabled)
            InfoRow(label: L("library.quality"), value: file.quality?.name ?? "–")
            InfoRow(label: L("library.size"), value: Fmt.bytes(file.sizeBytes))
            if let g = file.releaseGroup, !g.isEmpty { InfoRow(label: L("library.group"), value: g) }
            if let cf = file.customFormats, !cf.isEmpty {
                InfoRow(label: L("library.formats"), value: cf.joined(separator: ", ") + (file.score.map { " (\($0))" } ?? ""))
            }
            if let mi = file.mediaInfo {
                let video = [mi.resolution.map { "\($0)p" } ?? "", mi.videoCodec ?? "", (mi.hdr ?? []).joined(separator: "/")].filter { !$0.isEmpty }.joined(separator: " ")
                let audio = [mi.audioCodec ?? "", mi.audioChannels ?? ""].filter { !$0.isEmpty }.joined(separator: " ")
                if !video.isEmpty { InfoRow(label: L("library.video"), value: video) }
                if !audio.isEmpty { InfoRow(label: L("library.audio"), value: audio) }
                if let langs = mi.languages, !langs.isEmpty { InfoRow(label: L("library.languages"), value: langs.joined(separator: ", ")) }
                if let subs = mi.subtitles, !subs.isEmpty { InfoRow(label: L("library.subtitles"), value: subs.joined(separator: ", ")) }
            }
            if let added = file.addedAt { InfoRow(label: L("library.added"), value: Fmt.date(added)) }
        }
        .card()
    }
}

/// Header of a library detail page: backdrop, poster, title, facts.
struct LibraryHeader: View {
    let backdrop: URL?
    let poster: URL?
    let title: String
    let facts: String
    var square = false
    var pills: [(String, String)] = []

    var body: some View {
        ZStack(alignment: .bottomLeading) {
            RemoteImage(url: backdrop, title: "", maxPixel: 1400)
                .frame(height: 240)
                .overlay(LinearGradient(colors: [Theme.background.opacity(0.25), Theme.background], startPoint: .top, endPoint: .bottom))
            HStack(alignment: .bottom, spacing: 14) {
                Poster(url: poster, title: title, square: square, width: 96).shadow(color: .black.opacity(0.5), radius: 12, y: 6)
                VStack(alignment: .leading, spacing: 6) {
                    Text(verbatim: title).font(.title3.weight(.bold)).foregroundStyle(Theme.text).lineLimit(3)
                    Text(verbatim: facts).font(.footnote).foregroundStyle(Theme.textMuted).lineLimit(2)
                    HStack(spacing: 6) { ForEach(Array(pills.enumerated()), id: \.offset) { _, p in Pill(text: p.0, tone: p.1) } }
                }
            }
            .padding(.horizontal)
        }
        .frame(height: 240)
    }
}

/// Feedback for one-shot actions ("Search started", errors).
@MainActor
func runAction(_ toast: Binding<String?>, success: String, _ op: () async throws -> Void) async {
    do {
        try await op()
        toast.wrappedValue = success
    } catch {
        let text = error.userMessage
        if !text.isEmpty { toast.wrappedValue = text }
    }
}
