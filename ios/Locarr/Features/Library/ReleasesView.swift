import SwiftUI

/// Interactive search: every indexer is asked, each release shows the decision with its
/// reasons, and any release can be grabbed by hand.
struct ReleasesView: View {
    let target: ReleaseTarget
    let title: String
    @Environment(Session.self) private var session
    @State private var result = Loader<InteractiveSearch>()
    @State private var show = "all"
    @State private var grabbing: String?
    @State private var grabbed: Set<String> = []
    @State private var confirm: InteractiveDecision?
    @State private var toast: String?

    var body: some View {
        List {
            if let r = result.value {
                Section {
                    Text(verbatim: L("interactive.summary", ["accepted": String(r.accepted ?? 0), "pending": String(r.pending ?? 0),
                                                             "rejected": String(r.rejected ?? 0), "profile": r.profileName ?? "–"]))
                        .font(.footnote).foregroundStyle(Theme.textMuted)
                    Picker(selection: $show) {
                        ForEach(["all", "accepted", "rejected"], id: \.self) { Text(verbatim: L("interactive.show.\($0)")).tag($0) }
                    } label: { Text(verbatim: L("interactive.filter")) }
                    .pickerStyle(.segmented)
                    if let inds = r.indexers, !inds.isEmpty {
                        DisclosureGroup {
                            ForEach(inds, id: \.indexerName) { i in
                                HStack {
                                    Text(verbatim: i.indexerName).font(.footnote)
                                    Spacer()
                                    Text(verbatim: i.status == "ok" ? String(i.count ?? 0) : Loc.render("timeline.indexerStatus.\(i.status)", [:], fallback: i.status))
                                        .font(.footnote.monospacedDigit())
                                        .foregroundStyle(i.status == "ok" ? Theme.textMuted : Theme.warning)
                                }
                            }
                        } label: { Text(verbatim: L("releases.indexers", ["count": String(inds.count)])).font(.footnote) }
                    }
                }
                .listRowBackground(Theme.surface)
                let rows = r.decisions.filter { show == "all" || (show == "accepted" ? $0.decision.status != "rejected" : $0.decision.status == "rejected") }
                if rows.isEmpty {
                    EmptyState(text: L("interactive.none")).listRowBackground(Color.clear)
                }
                ForEach(Array(rows.enumerated()), id: \.offset) { _, d in
                    row(d).listRowBackground(Theme.background)
                }
            } else if let error = result.error {
                ErrorState(message: error) { await load() }.listRowBackground(Color.clear)
            } else {
                VStack(spacing: 12) {
                    ProgressView().tint(Theme.accent)
                    Text(verbatim: L("interactive.searching")).font(.footnote).foregroundStyle(Theme.textMuted)
                }
                .frame(maxWidth: .infinity, minHeight: 200)
                .listRowBackground(Color.clear)
            }
        }
        .listStyle(.plain)
        .refreshable { await load() }
        .task { if result.value == nil { await load() } }
        .navigationTitle(Text(verbatim: title))
        .navigationBarTitleDisplayMode(.inline)
        .screenStyle()
        .confirmationDialog(Text(verbatim: L("interactive.grabAnyway")), isPresented: Binding(get: { confirm != nil }, set: { if !$0 { confirm = nil } }),
                            presenting: confirm) { d in
            Button(L("interactive.grabAnyway")) { Task { await grab(d) } }
        } message: { d in
            Text(verbatim: (d.decision.rejections ?? []).map(Describe.rejection).joined(separator: ", "))
        }
        .toast($toast)
    }

    private func key(_ d: InteractiveDecision) -> String { d.decision.candidate.guid ?? d.decision.candidate.title }

    private func row(_ d: InteractiveDecision) -> some View {
        let dec = d.decision
        let c = dec.candidate
        let tone = dec.status == "accepted" ? "success" : dec.status == "pending" ? "warning" : "danger"
        return VStack(alignment: .leading, spacing: 6) {
            Text(verbatim: c.title).font(.footnote.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(3)
            HStack(spacing: 6) {
                Pill(text: Loc.render("interactive.status.\(dec.status)", [:], fallback: dec.status), tone: tone)
                if let q = dec.quality?.name { Pill(text: q) }
                if let s = dec.score, s != 0 { Pill(text: (s > 0 ? "+" : "") + String(s), tone: s > 0 ? "info" : "warning") }
            }
            Text(verbatim: facts(c)).font(.caption2.monospacedDigit()).foregroundStyle(Theme.textMuted)
            if let rej = dec.rejections, !rej.isEmpty {
                Text(verbatim: rej.map(Describe.rejection).joined(separator: " · ")).font(.caption).foregroundStyle(Theme.danger)
            }
            if let cf = dec.customFormats, !cf.isEmpty {
                Text(verbatim: cf.map(\.name).joined(separator: ", ")).font(.caption2).foregroundStyle(Theme.info)
            }
            HStack {
                Spacer()
                if grabbed.contains(key(d)) {
                    Label { Text(verbatim: L("interactive.sent")) } icon: { Image(systemName: "checkmark") }
                        .font(.footnote.weight(.semibold)).foregroundStyle(Theme.success)
                } else if grabbing == key(d) {
                    ProgressView()
                } else {
                    Button {
                        if dec.status == "rejected" { confirm = d } else { Task { await grab(d) } }
                    } label: {
                        Label { Text(verbatim: L(dec.status == "rejected" ? "interactive.grabAnyway" : "interactive.grab")) } icon: { Image(systemName: "arrow.down.circle") }
                    }
                    .buttonStyle(SecondaryButtonStyle(tint: dec.status == "rejected" ? Theme.warning : Theme.accent))
                }
            }
        }
        .padding(.vertical, 6)
        .accessibilityElement(children: .contain)
    }

    private func facts(_ c: Candidate) -> String {
        var parts: [String] = []
        if let i = c.indexerName { parts.append(i) }
        if let s = c.size, s > 0 { parts.append(Fmt.bytes(s)) }
        if c.protocol == "torrent" { parts.append(L("releases.seeders", ["n": String(c.seeders ?? 0)])) }
        if let p = c.publishDate { parts.append(Fmt.relative(p)) }
        return parts.joined(separator: " · ")
    }

    private func load() async {
        let client = session.client, target = target
        result.value = nil
        result.error = nil
        await result.load { try await client.releases(target) }
    }

    private func grab(_ d: InteractiveDecision) async {
        let k = key(d)
        grabbing = k
        defer { grabbing = nil }
        do {
            _ = try await session.client.grab(target, d)
            grabbed.insert(k)
            toast = L("releases.grabbed")
        } catch {
            toast = error.userMessage
        }
    }
}

/// The whole timeline of one item (movie, episode, album).
struct ItemTimelineView: View {
    let ref: String
    let title: String
    @Environment(Session.self) private var session
    @State private var timeline = Loader<Timeline>()

    var body: some View {
        List {
            Section { WhyCard(itemRef: ref, title: title).listRowInsets(EdgeInsets()) }
                .listRowBackground(Color.clear)
            if let t = timeline.value {
                if t.events.isEmpty {
                    EmptyState(text: L("timeline.empty")).listRowBackground(Color.clear)
                }
                ForEach(t.events, id: \.key) { e in
                    HStack(alignment: .top, spacing: 12) {
                        Circle().fill(Theme.tone(Describe.eventTone(e.type)).fg).frame(width: 9, height: 9).padding(.top, 5)
                        VStack(alignment: .leading, spacing: 3) {
                            Text(verbatim: Describe.event(e)).font(.callout).foregroundStyle(Theme.text).fixedSize(horizontal: false, vertical: true)
                            Text(verbatim: Fmt.dateTime(e.at)).font(.caption2).foregroundStyle(Theme.textFaint)
                        }
                    }
                    .listRowBackground(Theme.background)
                    .accessibilityElement(children: .combine)
                }
            } else if let error = timeline.error {
                ErrorState(message: error) { await load() }.listRowBackground(Color.clear)
            } else {
                LoadingState().listRowBackground(Color.clear)
            }
        }
        .listStyle(.plain)
        .refreshable { await load() }
        .task { await load() }
        .navigationTitle(Text(verbatim: title))
        .navigationBarTitleDisplayMode(.inline)
        .screenStyle()
    }

    private func load() async {
        let client = session.client, ref = ref
        await timeline.load { try await client.timeline(ref) }
    }
}
