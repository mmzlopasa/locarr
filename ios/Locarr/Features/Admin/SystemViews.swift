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

struct IndexersView: View {
    @Environment(Session.self) private var session
    @State private var indexers = Loader<[Indexer]>()
    @State private var testing: Set<Int64> = []
    @State private var results: [Int64: IndexerTestResult] = [:]

    var body: some View {
        List {
            if let list = indexers.value {
                if list.isEmpty { EmptyState(text: L("indexers.none")).listRowBackground(Color.clear) }
                ForEach(list) { i in
                    VStack(alignment: .leading, spacing: 6) {
                        HStack {
                            Circle().fill(statusColor(i)).frame(width: 9, height: 9)
                            Text(verbatim: i.name).font(.callout.weight(.semibold)).foregroundStyle(i.enabled == false ? Theme.textMuted : Theme.text)
                            Spacer()
                            Pill(text: (i.protocol ?? "").capitalized)
                        }
                        Text(verbatim: [i.kind?.capitalized ?? "", L("system.priority", ["n": String(i.priority ?? 0)]),
                                        i.enabled == false ? L("system.disabled") : ""].filter { !$0.isEmpty }.joined(separator: " · "))
                            .font(.caption).foregroundStyle(Theme.textMuted)
                        if let h = i.health, h.status == "failing" || h.status == "disabled" {
                            Text(verbatim: [h.lastError ?? "", h.retryAt.map { L("system.retryAt", ["when": Fmt.relative($0)]) } ?? ""]
                                    .filter { !$0.isEmpty }.joined(separator: " · "))
                                .font(.caption).foregroundStyle(Theme.danger)
                        }
                        testLine(i)
                    }
                    .padding(.vertical, 4)
                    .listRowBackground(Theme.background)
                }
                Section { Text(verbatim: L("system.editOnWeb")).font(.footnote).foregroundStyle(Theme.textMuted) }.listRowBackground(Color.clear)
            } else if let error = indexers.error {
                ErrorState(message: error) { await load() }.listRowBackground(Color.clear)
            } else {
                LoadingState().listRowBackground(Color.clear)
            }
        }
        .listStyle(.plain)
        .refreshable { await load() }
        .task { await load() }
        .navigationTitle(Text(verbatim: L("nav.indexers")))
        .screenStyle()
    }

    @ViewBuilder private func testLine(_ i: Indexer) -> some View {
        HStack {
            if let r = results[i.id] {
                Label {
                    Text(verbatim: r.ok ? L("system.testOk", ["ms": String(r.durationMs ?? 0)]) : Loc.render("errors.\(r.errorCode ?? "")", [:], fallback: r.error ?? L("errors.unknown")))
                } icon: { Image(systemName: r.ok ? "checkmark.circle.fill" : "xmark.octagon.fill") }
                .font(.caption).foregroundStyle(r.ok ? Theme.success : Theme.danger)
            }
            Spacer()
            if testing.contains(i.id) { ProgressView() } else {
                Button(L("system.test")) { Task { await test(i) } }.buttonStyle(.borderless).font(.footnote.weight(.semibold))
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
