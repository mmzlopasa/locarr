import SwiftUI

struct HealthView: View {
    @Environment(Session.self) private var session
    @Environment(\.openURL) private var openURL
    @State private var report = Loader<HealthReport>()
    @State private var running = false

    var body: some View {
        List {
            if let r = report.value {
                Section {
                    if let at = r.checkedAt { Text(verbatim: L("health.checked", ["when": Fmt.relative(at)])).font(.footnote).foregroundStyle(Theme.textMuted) }
                    if r.issues.isEmpty {
                        Label { Text(verbatim: L("health.allGood")) } icon: { Image(systemName: "checkmark.seal.fill").foregroundStyle(Theme.success) }
                    }
                }
                .listRowBackground(Theme.surface)
                ForEach(r.issues) { issue in
                    VStack(alignment: .leading, spacing: 8) {
                        HStack {
                            Pill(text: Loc.render("health.severity.\(issue.severity)", [:], fallback: issue.severity), tone: issue.severity)
                            Spacer()
                            if let since = issue.since { Text(verbatim: L("health.since", ["when": Fmt.relative(since)])).font(.caption2).foregroundStyle(Theme.textFaint) }
                        }
                        Text(verbatim: Describe.health(issue)).font(.callout).foregroundStyle(Theme.text).fixedSize(horizontal: false, vertical: true)
                        HStack(spacing: 14) {
                            if let fix = issue.fixRoute, !fix.isEmpty {
                                Button(L("health.fixHere")) { openURL(webURL(fix)) }.font(.footnote.weight(.semibold))
                            }
                            if let docs = issue.docs, !docs.isEmpty, let url = docsURL(docs) {
                                Button(L("health.howToFix")) { openURL(url) }.font(.footnote)
                            }
                        }
                        .buttonStyle(.borderless)
                    }
                    .padding(.vertical, 4)
                    .listRowBackground(Theme.background)
                }
            } else if let error = report.error {
                ErrorState(message: error) { await load() }.listRowBackground(Color.clear)
            } else {
                LoadingState().listRowBackground(Color.clear)
            }
        }
        .listStyle(.plain)
        .refreshable { await load() }
        .task { await load() }
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                if running { ProgressView() } else {
                    Button(L("health.runNow")) {
                        Task {
                            running = true
                            defer { running = false }
                            let client = session.client
                            await report.load { try await client.runHealth() }
                        }
                    }
                }
            }
        }
        .navigationTitle(Text(verbatim: L("health.title")))
        .screenStyle()
    }

    /// A page of the web client ("/settings/media" → server/settings/media).
    private func webURL(_ route: String) -> URL {
        let path = route.hasPrefix("/") ? String(route.dropFirst()) : route
        return session.server.url.appending(path: path)
    }

    /// The anchor in docs/user/troubleshooting.md on GitHub (or a full URL).
    private func docsURL(_ docs: String) -> URL? {
        if docs.hasPrefix("http") { return URL(string: docs) }
        return URL(string: "https://github.com/mmzlopasa/locarr/blob/main/docs/user/troubleshooting.md#" + docs)
    }

    private func load() async {
        let client = session.client
        await report.load { try await client.health() }
    }
}

/// Indexers: the configured ones (state, health, Test) and what can be added — built-in
/// sites Locarr searches itself and usenet presets. Adding and editing happens on the web.
struct IndexersView: View {
    @Environment(Session.self) private var session
    @Environment(\.openURL) private var openURL
    @State private var part = "configured"
    @State private var indexers = Loader<[Indexer]>()
    @State private var catalog = Loader<IndexerCatalog>()
    @State private var testing: Set<Int64> = []
    @State private var results: [Int64: IndexerTestResult] = [:]

    var body: some View {
        List {
            SegmentPicker(options: ["configured", "builtin", "usenet"], selection: $part, label: L("nav.indexers")) {
                $0 == "configured" ? L("system.indexersConfigured") : L("indexers.chooser.tabs.\($0)")
            }
            .segmentRow()
            switch part {
            case "builtin": builtinRows
            case "usenet": usenetRows
            default: configuredRows
            }
            Section {
                Button { openURL(session.server.url.appending(path: "indexers")) } label: {
                    Label { Text(verbatim: L("system.addOnWeb")) } icon: { Image(systemName: "safari") }
                }
            } footer: { Text(verbatim: L("system.editOnWeb")) }
        }
        .listStyle(.insetGrouped)
        .refreshable { await load() }
        .task { await load() }
        .navigationTitle(Text(verbatim: L("nav.indexers")))
        .screenStyle()
    }

    @ViewBuilder private var configuredRows: some View {
        if let list = indexers.value {
            if list.isEmpty { EmptyState(text: L("indexers.none")).listRowBackground(Color.clear) }
            ForEach(list) { i in
                VStack(alignment: .leading, spacing: 6) {
                    HStack {
                        Circle().fill(statusColor(i)).frame(width: 9, height: 9)
                        Text(verbatim: i.name).font(.callout.weight(.semibold)).foregroundStyle(i.enabled == false ? Theme.textMuted : Theme.text)
                        Spacer()
                        Pill(text: Loc.render("indexers.kinds.\(i.kind ?? "")", [:], fallback: i.kind ?? ""), tone: i.kind == "native" ? "info" : nil)
                    }
                    Text(verbatim: facts(i)).font(.caption).foregroundStyle(Theme.textMuted)
                    if let h = i.health, h.status == "failing" || h.status == "disabled" {
                        Text(verbatim: [h.lastErrorCode.flatMap { Loc.has("indexers.errorKinds.\($0)") ? L("indexers.errorKinds.\($0)") : nil } ?? h.lastError ?? "",
                                        h.retryAt.map { L("system.retryAt", ["when": Fmt.relative($0)]) } ?? ""]
                                .filter { !$0.isEmpty }.joined(separator: " · "))
                            .font(.caption).foregroundStyle(Theme.danger)
                    }
                    if let h = i.health, i.kind == "native" {
                        if let n = h.parseFailures, n > 0 {
                            Text(verbatim: L("indexers.builtin.parseFailures", ["n": String(n)])).font(.caption).foregroundStyle(Theme.warning)
                        }
                        if let at = h.selfTestAt {
                            Text(verbatim: L(h.selfTestOk == false ? "indexers.builtin.selfTestFailed" : "indexers.builtin.selfTestOk", ["when": Fmt.relative(at)]))
                                .font(.caption2).foregroundStyle(h.selfTestOk == false ? Theme.danger : Theme.textFaint)
                        }
                    }
                    if i.definition?.missing == true {
                        Text(verbatim: L("indexers.builtin.missing")).font(.caption).foregroundStyle(Theme.danger)
                    }
                    testLine(i)
                }
                .padding(.vertical, 4)
            }
        } else if let error = indexers.error {
            ErrorState(message: error) { await load() }.listRowBackground(Color.clear)
        } else {
            LoadingState().listRowBackground(Color.clear)
        }
    }

    @ViewBuilder private var builtinRows: some View {
        Section {
            Text(verbatim: L("indexers.chooser.builtinIntro")).font(.footnote).foregroundStyle(Theme.textMuted)
        }
        .listRowBackground(Color.clear)
        if let c = catalog.value {
            if c.definitions.isEmpty { EmptyState(text: L("indexers.builtin.none")).listRowBackground(Color.clear) }
            ForEach(c.definitions.sorted { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }) { d in
                VStack(alignment: .leading, spacing: 5) {
                    HStack {
                        Text(verbatim: d.name).font(.callout.weight(.semibold)).foregroundStyle(Theme.text)
                        if d.builtin == false { Pill(text: L(d.overrides == true ? "indexers.builtin.overrides" : "indexers.builtin.custom"), tone: "info") }
                        Spacer()
                        if !(d.indexerIds ?? []).isEmpty {
                            Label { Text(verbatim: L("system.inUse")) } icon: { Image(systemName: "checkmark.circle.fill") }
                                .font(.caption.weight(.semibold)).foregroundStyle(Theme.success)
                        }
                    }
                    if let desc = d.description, !desc.isEmpty {
                        Text(verbatim: desc).font(.caption).foregroundStyle(Theme.textMuted).lineLimit(3)
                    }
                    Text(verbatim: definitionFacts(d)).font(.caption2).foregroundStyle(Theme.textFaint)
                    if d.flaresolverr == "sometimes" || d.flaresolverr == "always" {
                        Text(verbatim: L("indexers.builtin.flaresolverr.\(d.flaresolverr ?? "")")).font(.caption2).foregroundStyle(Theme.warning)
                    }
                }
                .padding(.vertical, 3)
                .accessibilityElement(children: .combine)
            }
            ForEach(c.errors ?? [], id: \.file) { e in
                Callout(text: "\(L("indexers.definitions.errors")): \(e.file) — \(e.message)", tone: "danger").listRowBackground(Color.clear)
            }
        } else if let error = catalog.error {
            ErrorState(message: error) { await load() }.listRowBackground(Color.clear)
        } else {
            LoadingState().listRowBackground(Color.clear)
        }
    }

    @ViewBuilder private var usenetRows: some View {
        Section {
            Text(verbatim: L("indexers.chooser.usenetIntro")).font(.footnote).foregroundStyle(Theme.textMuted)
        }
        .listRowBackground(Color.clear)
        if let c = catalog.value {
            ForEach(c.presets) { p in
                HStack {
                    VStack(alignment: .leading, spacing: 3) {
                        Text(verbatim: p.name).font(.callout.weight(.semibold)).foregroundStyle(Theme.text)
                        if let site = p.siteUrl { Text(verbatim: site).font(.caption.monospaced()).foregroundStyle(Theme.textMuted) }
                    }
                    Spacer()
                    if usedPreset(p) {
                        Image(systemName: "checkmark.circle.fill").foregroundStyle(Theme.success)
                            .accessibilityLabel(Text(verbatim: L("system.inUse")))
                    } else if let site = p.siteUrl.flatMap({ URL(string: $0) }) {
                        Button { openURL(site) } label: { Image(systemName: "arrow.up.right.square") }
                            .buttonStyle(.borderless)
                            .accessibilityLabel(Text(verbatim: L("indexers.usenet.whereKey", ["site": p.name])))
                    }
                }
            }
        } else if catalog.error == nil {
            LoadingState().listRowBackground(Color.clear)
        }
    }

    private func usedPreset(_ p: UsenetPreset) -> Bool {
        guard let base = p.baseUrl?.lowercased(), !base.isEmpty else { return false }
        return (indexers.value ?? []).contains { ($0.baseUrl ?? "").lowercased().hasPrefix(base) }
    }

    private func facts(_ i: Indexer) -> String {
        var parts: [String] = []
        if let d = i.definition, i.kind == "native", d.name?.isEmpty == false, d.name != i.name { parts.append(d.name ?? "") }
        parts.append((i.protocol ?? "").capitalized)
        parts.append(L("indexers.priorityShort", ["n": String(i.priority ?? 0)]))
        if let src = i.source { parts.append(L("indexers.fromSource", ["name": src.name])) }
        if i.enabled == false { parts.append(L("system.disabled")) }
        return parts.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    private func definitionFacts(_ d: IndexerDefinition) -> String {
        var parts = [(d.protocol ?? "").capitalized]
        if let lang = d.language, !lang.isEmpty { parts.append(lang) }
        let modes = (d.searching ?? []).filter { $0.available != false }.map { Loc.render("indexers.builtin.modes.\($0.type)", [:], fallback: $0.type) }
        if !modes.isEmpty { parts.append(modes.joined(separator: ", ")) }
        parts.append(L(d.rss == true ? "indexers.builtin.rss" : "indexers.builtin.noRss"))
        return parts.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    @ViewBuilder private func testLine(_ i: Indexer) -> some View {
        HStack {
            if let r = results[i.id] {
                Label {
                    Text(verbatim: r.ok ? L("indexers.test.ok", ["n": String(r.probeResults ?? 0), "ms": String(r.durationMs ?? 0)])
                         : Loc.render("indexers.errorKinds.\(r.errorCode ?? "")", [:], fallback: r.error ?? L("errors.unknown")))
                } icon: { Image(systemName: r.ok ? "checkmark.circle.fill" : "xmark.octagon.fill") }
                .font(.caption).foregroundStyle(r.ok ? Theme.success : Theme.danger)
            }
            Spacer()
            if testing.contains(i.id) { ProgressView() } else {
                Button(L("indexers.test.button")) { Task { await test(i) } }.buttonStyle(.borderless).font(.footnote.weight(.semibold))
            }
        }
    }

    private func statusColor(_ i: Indexer) -> Color {
        if i.enabled == false { return Theme.textFaint }
        switch i.health?.status {
        case "healthy": return Theme.success
        case "failing": return Theme.danger
        case "disabled": return Theme.warning
        default: return Theme.textMuted
        }
    }

    private func test(_ i: Indexer) async {
        testing.insert(i.id)
        defer { testing.remove(i.id) }
        do { results[i.id] = try await session.client.testIndexer(i.id) } catch {
            results[i.id] = IndexerTestResult(ok: false, durationMs: nil, errorCode: (error as? APIError)?.code, error: error.userMessage)
        }
    }

    private func load() async {
        let client = session.client
        await indexers.load { try await client.indexers() }
        await catalog.load { try await client.indexerCatalog() }
    }
}

struct DownloadClientsView: View {
    @Environment(Session.self) private var session
    @State private var clients = Loader<[JSONValue]>()
    @State private var testing: Set<Int64> = []
    @State private var results: [Int64: DownloadClientTestResult] = [:]

    var body: some View {
        List {
            if let list = clients.value {
                if list.isEmpty { EmptyState(text: L("queue.noClients")).listRowBackground(Color.clear) }
                ForEach(Array(list.enumerated()), id: \.offset) { _, raw in
                    row(raw).listRowBackground(Theme.background)
                }
                Section { Text(verbatim: L("system.editOnWeb")).font(.footnote).foregroundStyle(Theme.textMuted) }.listRowBackground(Color.clear)
            } else if let error = clients.error {
                ErrorState(message: error) { await load() }.listRowBackground(Color.clear)
            } else {
                LoadingState().listRowBackground(Color.clear)
            }
        }
        .listStyle(.plain)
        .refreshable { await load() }
        .task { await load() }
        .navigationTitle(Text(verbatim: L("nav.clients")))
        .screenStyle()
    }

    private func row(_ raw: JSONValue) -> some View {
        let id = Int64(raw["id"].number ?? 0)
        let ok = raw["status"]["ok"].bool
        let enabled = raw["enabled"].bool != false
        return VStack(alignment: .leading, spacing: 6) {
            HStack {
                Circle().fill(!enabled ? Theme.textFaint : ok == true ? Theme.success : ok == false ? Theme.danger : Theme.textMuted).frame(width: 9, height: 9)
                Text(verbatim: raw["name"].text).font(.callout.weight(.semibold)).foregroundStyle(enabled ? Theme.text : Theme.textMuted)
                Spacer()
                Pill(text: raw["implementation"].text)
            }
            Text(verbatim: [raw["host"].text + (raw["port"].isNull ? "" : ":" + raw["port"].text), raw["status"]["version"].text,
                            raw["status"]["freeSpaceBytes"].number.map { L("system.freeSpace", ["size": Fmt.bytes(Int64($0))]) } ?? "",
                            enabled ? "" : L("system.disabled")].filter { !$0.isEmpty }.joined(separator: " · "))
                .font(.caption).foregroundStyle(Theme.textMuted)
            if ok == false, !raw["status"]["error"].text.isEmpty {
                Text(verbatim: raw["status"]["error"].text).font(.caption).foregroundStyle(Theme.danger)
            }
            HStack {
                if let r = results[id] {
                    Label {
                        Text(verbatim: clientResult(r))
                    } icon: { Image(systemName: r.ok ? "checkmark.circle.fill" : "xmark.octagon.fill") }
                    .font(.caption).foregroundStyle(r.ok && (r.missingCategories ?? []).isEmpty ? Theme.success : r.ok ? Theme.warning : Theme.danger)
                }
                Spacer()
                if testing.contains(id) { ProgressView() } else {
                    Button(L("system.test")) { Task { await test(raw, id: id) } }.buttonStyle(.borderless).font(.footnote.weight(.semibold))
                }
            }
        }
        .padding(.vertical, 4)
    }

    private func clientResult(_ r: DownloadClientTestResult) -> String {
        if r.ok, let missing = r.missingCategories, !missing.isEmpty { return L("system.missingCategories", ["list": missing.joined(separator: ", ")]) }
        if r.ok { return [L("system.connected"), r.version ?? ""].filter { !$0.isEmpty }.joined(separator: " · ") }
        return Loc.render("errors.\(r.errorCode ?? "")", [:], fallback: r.message ?? L("errors.unknown"))
    }

    private func test(_ raw: JSONValue, id: Int64) async {
        testing.insert(id)
        defer { testing.remove(id) }
        do { results[id] = try await session.client.testDownloadClient(raw) } catch {
            results[id] = DownloadClientTestResult(ok: false, errorCode: (error as? APIError)?.code, message: error.userMessage)
        }
    }

    private func load() async {
        let client = session.client
        await clients.load { try await client.downloadClients() }
    }
}

struct RootFoldersView: View {
    @Environment(Session.self) private var session
    @State private var folders = Loader<[RootFolder]>()

    var body: some View {
        List {
            if let list = folders.value {
                if list.isEmpty { EmptyState(text: L("folders.none"), systemImage: "folder").listRowBackground(Color.clear) }
                ForEach(list) { f in
                    VStack(alignment: .leading, spacing: 6) {
                        DiskRow(folder: f)
                        if f.writable == false {
                            Text(verbatim: [Loc.render("folders.reasons.\(f.notWritableReason ?? "other")", [:], fallback: f.notWritableReason ?? ""), f.notWritableDetail ?? ""]
                                    .filter { !$0.isEmpty }.joined(separator: " — "))
                                .font(.caption).foregroundStyle(Theme.warning)
                        }
                    }
                    .padding(.vertical, 4)
                    .listRowBackground(Theme.background)
                }
                Section { Text(verbatim: L("folders.editOnWeb")).font(.footnote).foregroundStyle(Theme.textMuted) }.listRowBackground(Color.clear)
            } else if let error = folders.error {
                ErrorState(message: error) { await load() }.listRowBackground(Color.clear)
            } else {
                LoadingState().listRowBackground(Color.clear)
            }
        }
        .listStyle(.plain)
        .refreshable { await load() }
        .task { await load() }
        .navigationTitle(Text(verbatim: L("overview.manage")))
        .screenStyle()
    }

    private func load() async {
        let client = session.client
        await folders.load { try await client.rootFolders() }
    }
}
